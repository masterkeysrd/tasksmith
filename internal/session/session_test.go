package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/gateway"
	"github.com/masterkeysrd/tasksmith/internal/harness"
	"github.com/masterkeysrd/tasksmith/internal/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/std"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
)

type capturedTurn struct {
	model    string
	messages []message.Message
}

type fakeProvider struct {
	mu      sync.Mutex
	turns   []capturedTurn
	handler func(ctx context.Context, req *llm.Request) ([]string, string, error)
}

func (p *fakeProvider) Name() string {
	return "fake"
}

func (p *fakeProvider) ListProfiles() []profile.Profile {
	return nil
}

func (p *fakeProvider) GetProfile(string) (profile.Profile, bool) {
	return profile.Profile{}, false
}

func (p *fakeProvider) SearchProfiles(string) []profile.Profile {
	return nil
}

func (p *fakeProvider) OverrideProfile(string, profile.Profile) {}

func (p *fakeProvider) Stream(ctx context.Context, req *llm.Request) (llm.StreamResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.Lock()
	clonedMsgs := make([]message.Message, len(req.Messages))
	for i, m := range req.Messages {
		clonedMsgs[i] = message.CloneMessage(m)
	}
	p.turns = append(p.turns, capturedTurn{
		model:    req.Model,
		messages: clonedMsgs,
	})
	handler := p.handler
	p.mu.Unlock()

	var chunks []string
	var respID string
	if handler != nil {
		var err error
		chunks, respID, err = handler(ctx, req)
		if err != nil {
			return nil, err
		}
	} else {
		chunks = []string{"Hello", " from ", "assistant!"}
		respID = uuid.NewString()
	}

	return func(yield func(message.AssistantChunk, error) bool) {
		for i, text := range chunks {
			if err := ctx.Err(); err != nil {
				yield(message.AssistantChunk{}, err)
				return
			}
			chunk := message.AssistantChunk{
				BaseChunk: message.BaseChunk{ID: respID},
				Model:     req.Model,
				Content: []message.Block{
					&message.TextBlock{Text: text},
				},
				Done: i == len(chunks)-1,
			}
			if !yield(chunk, nil) {
				return
			}
		}
	}, nil
}

func (p *fakeProvider) recordedTurns() []capturedTurn {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]capturedTurn, len(p.turns))
	copy(out, p.turns)
	return out
}

type fakeGateway struct {
	provider *fakeProvider
}

func newFakeGateway(p *fakeProvider) *fakeGateway {
	if p == nil {
		p = &fakeProvider{}
	}
	return &fakeGateway{provider: p}
}

func (g *fakeGateway) ResolveLLM(ctx context.Context, explicit string, _ ...gateway.LLMOption) (*llm.Model, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	modelName := strings.TrimSpace(explicit)
	if modelName == "" {
		modelName = "fake-default-model"
	}
	return llm.NewModel(g.provider, modelName, nil)
}

func (g *fakeGateway) ResolveDecision(context.Context, string, ...gateway.DecisionOption) (*decision.Model, error) {
	return nil, gateway.ErrUnsupportedCapability
}

func (g *fakeGateway) ResolveEmbedding(context.Context, string, ...gateway.EmbeddingOption) (*embedding.Model, error) {
	return nil, gateway.ErrUnsupportedCapability
}

func (g *fakeGateway) ListProviders() []gateway.ProviderInfo {
	return nil
}

func (g *fakeGateway) Close() error {
	return nil
}

type testPublisher struct {
	mu     sync.Mutex
	events []Event
}

func (p *testPublisher) Publish(_ context.Context, ev Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, ev)
}

func (p *testPublisher) recorded() []Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Event, len(p.events))
	copy(out, p.events)
	return out
}

func (p *testPublisher) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = nil
}

func setupTestEnv(t *testing.T, prov *fakeProvider) (*sql.DB, *workspace.Workspace, *fakeGateway, *testPublisher, *Manager) {
	t.Helper()

	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-session-test")
	t.Cleanup(func() {
		xdg.ClearCache()
	})

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	ws, err := workspace.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}

	logger := log.Default()
	gw := newFakeGateway(prov)
	pub := &testPublisher{}

	mgr, err := New(Config{
		DB:        db,
		Workspace: ws,
		Gateway:   gw,
		Publisher: pub,
		Logger:    logger,
	})
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}

	return db, ws, gw, pub, mgr
}

func loadHarnessState(t *testing.T, db *sql.DB, gw *fakeGateway, sessionID string) harness.State {
	t.Helper()
	llmModel, err := gw.ResolveLLM(context.Background(), "fake-default-model")
	if err != nil {
		t.Fatalf("ResolveLLM: %v", err)
	}
	h, err := harness.New(harness.Config{
		DB:     db,
		Model:  llmModel,
		Logger: log.Default(),
	})
	if err != nil {
		t.Fatalf("harness.New: %v", err)
	}
	state, err := h.LoadState(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("h.LoadState: %v", err)
	}
	return state
}

func extractBlocksText(blocks []message.Block) string {
	var sb strings.Builder
	for _, b := range blocks {
		if tb, ok := b.(*message.TextBlock); ok && tb != nil {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}

func msgText(m message.Message) string {
	if m == nil {
		return ""
	}
	return extractBlocksText(m.GetContent())
}

func TestConfigValidate(t *testing.T) {
	db, ws, gw, pub, _ := setupTestEnv(t, nil)
	logger := log.Default()

	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "missing DB",
			cfg: Config{
				Workspace: ws,
				Gateway:   gw,
				Publisher: pub,
				Logger:    logger,
			},
		},
		{
			name: "missing Workspace",
			cfg: Config{
				DB:        db,
				Gateway:   gw,
				Publisher: pub,
				Logger:    logger,
			},
		},
		{
			name: "missing Gateway",
			cfg: Config{
				DB:        db,
				Workspace: ws,
				Publisher: pub,
				Logger:    logger,
			},
		},
		{
			name: "missing Publisher",
			cfg: Config{
				DB:        db,
				Workspace: ws,
				Gateway:   gw,
				Logger:    logger,
			},
		},
		{
			name: "missing Logger",
			cfg: Config{
				DB:        db,
				Workspace: ws,
				Gateway:   gw,
				Publisher: pub,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("Validate() error = %v, want %v", err, ErrInvalidConfig)
			}
			if _, err := New(tc.cfg); !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("New() error = %v, want %v", err, ErrInvalidConfig)
			}
		})
	}
}

func TestSessionCRUD(t *testing.T) {
	ctx := context.Background()
	db, _, _, _, mgr := setupTestEnv(t, nil)

	// Initially empty list
	initial, err := mgr.List(ctx)
	if err != nil {
		t.Fatalf("List initial error: %v", err)
	}
	if len(initial) != 0 {
		t.Fatalf("expected 0 initial sessions, got %d", len(initial))
	}

	// Create session 1
	s1, err := mgr.Create(ctx, CreateParams{Title: "First Session"})
	if err != nil {
		t.Fatalf("Create s1 error: %v", err)
	}
	if s1.ID == "" {
		t.Fatal("expected non-empty s1.ID")
	}
	if s1.Title != "First Session" {
		t.Errorf("s1.Title = %q, want %q", s1.Title, "First Session")
	}
	if s1.Agent == "" {
		t.Errorf("expected default s1.Agent to be resolved from workspace, got empty")
	}
	if s1.CreatedAt.IsZero() || s1.UpdatedAt.IsZero() {
		t.Errorf("expected non-zero timestamps on s1: %+v", s1)
	}

	// Ensure slightly later timestamp for s2
	time.Sleep(2 * time.Millisecond)

	// Create session 2
	s2, err := mgr.Create(ctx, CreateParams{Title: "Second Session"})
	if err != nil {
		t.Fatalf("Create s2 error: %v", err)
	}

	// Get s1
	got1, err := mgr.Get(ctx, s1.ID)
	if err != nil {
		t.Fatalf("Get s1 error: %v", err)
	}
	if got1.ID != s1.ID || got1.Title != s1.Title || got1.Agent != s1.Agent || got1.Model != s1.Model || !got1.CreatedAt.Equal(s1.CreatedAt) {
		t.Errorf("Get(s1) = %+v, want %+v", got1, s1)
	}

	// Get validation errors
	if _, err := mgr.Get(ctx, ""); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("Get empty ID error = %v, want %v", err, ErrEmptySessionID)
	}
	if _, err := mgr.Get(ctx, "   "); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("Get whitespace ID error = %v, want %v", err, ErrEmptySessionID)
	}
	if _, err := mgr.Get(ctx, "non-existent-id"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Get non-existent ID error = %v, want %v", err, ErrSessionNotFound)
	}

	// List should return [s2, s1] ordered by updated_at DESC
	list, err := mgr.List(ctx)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(list))
	}
	if list[0].ID != s2.ID || list[1].ID != s1.ID {
		t.Errorf("List order = [%s, %s], want [%s, %s]", list[0].ID, list[1].ID, s2.ID, s1.ID)
	}

	// Append a message to s1 so its updated_at becomes newer than s2
	time.Sleep(2 * time.Millisecond)
	if err := mgr.AppendMessage(ctx, s1.ID, message.NewUserText("Hello on s1")); err != nil {
		t.Fatalf("AppendMessage s1 error: %v", err)
	}

	listAfterUpdate, err := mgr.List(ctx)
	if err != nil {
		t.Fatalf("List after update error: %v", err)
	}
	if listAfterUpdate[0].ID != s1.ID || listAfterUpdate[1].ID != s2.ID {
		t.Errorf("List order after s1 update = [%s, %s], want [%s, %s]",
			listAfterUpdate[0].ID, listAfterUpdate[1].ID, s1.ID, s2.ID)
	}

	// Delete validation errors
	if err := mgr.Delete(ctx, ""); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("Delete empty ID error = %v, want %v", err, ErrEmptySessionID)
	}
	if err := mgr.Delete(ctx, "unknown-session"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Delete unknown ID error = %v, want %v", err, ErrSessionNotFound)
	}

	// Delete s1 and verify both session and its messages are gone
	if err := mgr.Delete(ctx, s1.ID); err != nil {
		t.Fatalf("Delete s1 error: %v", err)
	}
	if _, err := mgr.Get(ctx, s1.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Get deleted s1 error = %v, want %v", err, ErrSessionNotFound)
	}

	var msgCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE session_id = ?`, s1.ID).Scan(&msgCount); err != nil {
		t.Fatalf("count messages for deleted s1: %v", err)
	}
	if msgCount != 0 {
		t.Errorf("expected 0 messages for deleted session s1, got %d", msgCount)
	}
}

func TestCreateWorkspaceDefaultsAndConcurrentSessionOverrides(t *testing.T) {
	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-session-ws-test")
	t.Cleanup(func() {
		xdg.ClearCache()
	})

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "ws-test.db")
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	wsDir := t.TempDir()
	wsManifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: test-ws
spec:
  defaultAgent: custom-default-agent
  defaultProvider: openai
---
WORKSPACE_SHARED_PROMPT_MARKER
`
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	agentsDir := filepath.Join(wsDir, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	defaultAgentManifest := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: custom-default-agent
  description: Default agent
spec:
  models:
    - gpt-5.5-default
---
DEFAULT_AGENT_PROMPT_MARKER
`
	if err := os.WriteFile(filepath.Join(agentsDir, "custom-default-agent.agent.md"), []byte(defaultAgentManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	shellToolManifest := `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: shell
  description: Shell tool
spec:
  command:
    - sh
---
Run shell commands.
`
	if err := os.WriteFile(filepath.Join(agentsDir, "shell.tool.md"), []byte(shellToolManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	overrideAgentManifest := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: override-agent
  description: Override agent
spec:
  tools:
    - shell
---
OVERRIDE_AGENT_PROMPT_MARKER {{ if call .HasTool "shell" }}HAS_SHELL_TOOL{{ end }}
`
	if err := os.WriteFile(filepath.Join(agentsDir, "override-agent.agent.md"), []byte(overrideAgentManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.Open(ctx, wsDir)
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}

	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"OK"}, uuid.NewString(), nil
		},
	}
	gw := newFakeGateway(prov)
	pub := &testPublisher{}

	mgr, err := New(Config{
		DB:        db,
		Workspace: ws,
		Gateway:   gw,
		Publisher: pub,
		Logger:    log.Default(),
	})
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}

	// Session A: uses workspace defaults for Agent and Model
	sessDefault, err := mgr.Create(ctx, CreateParams{Title: "Default Session"})
	if err != nil {
		t.Fatalf("Create default session error: %v", err)
	}
	if sessDefault.Agent != "custom-default-agent" {
		t.Errorf("sessDefault.Agent = %q, want %q", sessDefault.Agent, "custom-default-agent")
	}
	if sessDefault.Model != "openai/gpt-5.5-default" {
		t.Errorf("sessDefault.Model = %q, want %q", sessDefault.Model, "openai/gpt-5.5-default")
	}

	// Session B: explicit Agent and Model overrides in the same workspace
	sessOverride, err := mgr.Create(ctx, CreateParams{
		Title: "Override Session",
		Agent: "override-agent",
		Model: "anthropic/claude-4-sonnet",
	})
	if err != nil {
		t.Fatalf("Create override session error: %v", err)
	}
	if sessOverride.Agent != "override-agent" {
		t.Errorf("sessOverride.Agent = %q, want %q", sessOverride.Agent, "override-agent")
	}
	if sessOverride.Model != "anthropic/claude-4-sonnet" {
		t.Errorf("sessOverride.Model = %q, want %q", sessOverride.Model, "anthropic/claude-4-sonnet")
	}

	// Send on both concurrent sessions and verify each uses its own Agent prompt and Model
	if _, err := mgr.Send(ctx, sessDefault.ID, message.NewUserText("Hello default")); err != nil {
		t.Fatalf("Send sessDefault error: %v", err)
	}
	if _, err := mgr.Send(ctx, sessOverride.ID, message.NewUserText("Hello override")); err != nil {
		t.Fatalf("Send sessOverride error: %v", err)
	}

	turns := prov.recordedTurns()
	if len(turns) != 2 {
		t.Fatalf("expected 2 LLM turns, got %d", len(turns))
	}

	// Verify Turn 1 (sessDefault)
	if turns[0].model != "openai/gpt-5.5-default" {
		t.Errorf("turns[0].model = %q, want %q", turns[0].model, "openai/gpt-5.5-default")
	}
	sys0 := msgText(turns[0].messages[0])
	if !strings.Contains(sys0, "WORKSPACE_SHARED_PROMPT_MARKER") || !strings.Contains(sys0, "DEFAULT_AGENT_PROMPT_MARKER") {
		t.Errorf("turns[0] system prompt = %q, missing expected default markers", sys0)
	}

	// Verify Turn 2 (sessOverride)
	if turns[1].model != "anthropic/claude-4-sonnet" {
		t.Errorf("turns[1].model = %q, want %q", turns[1].model, "anthropic/claude-4-sonnet")
	}
	sys1 := msgText(turns[1].messages[0])
	if !strings.Contains(sys1, "WORKSPACE_SHARED_PROMPT_MARKER") ||
		!strings.Contains(sys1, "OVERRIDE_AGENT_PROMPT_MARKER") ||
		!strings.Contains(sys1, "HAS_SHELL_TOOL") {
		t.Errorf("turns[1] system prompt = %q, missing expected override markers", sys1)
	}
}

func TestSendAndHistory(t *testing.T) {
	ctx := context.Background()
	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"Hello", ", ", "TaskSmith!"}, "asst-msg-1", nil
		},
	}
	db, _, gw, pub, mgr := setupTestEnv(t, prov)

	sess, err := mgr.Create(ctx, CreateParams{Title: "Chat Loop"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// Validation checks on Send and History
	if _, err := mgr.Send(ctx, "", message.NewUserText("hi")); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("Send empty sessionID error = %v, want %v", err, ErrEmptySessionID)
	}
	if _, err := mgr.Send(ctx, "missing", message.NewUserText("hi")); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Send missing sessionID error = %v, want %v", err, ErrSessionNotFound)
	}
	if _, err := mgr.Send(ctx, sess.ID, nil); !errors.Is(err, ErrNilMessage) {
		t.Errorf("Send nil input error = %v, want %v", err, ErrNilMessage)
	}
	if _, err := mgr.History(ctx, "missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("History missing sessionID error = %v, want %v", err, ErrSessionNotFound)
	}

	pub.reset()

	userIn := message.NewUserText("Greetings!")
	reply, err := mgr.Send(ctx, sess.ID, userIn)
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}

	if reply == nil {
		t.Fatal("expected non-nil assistant reply")
	}
	if got := msgText(reply); got != "Hello, TaskSmith!" {
		t.Errorf("reply text = %q, want %q", got, "Hello, TaskSmith!")
	}
	parsedReplyID, err := uuid.Parse(reply.GetID())
	if err != nil || parsedReplyID.Version() != 7 {
		t.Errorf("reply.GetID() = %q, want valid UUIDv7 (err=%v)", reply.GetID(), err)
	}

	// Verify published event sequence via Publisher:
	// 1. EventMessageAdded (user)
	// 2..4. EventChunk ("Hello", ", ", "TaskSmith!")
	// 5. EventMessageAdded (assistant)
	// 6. EventCompleted (assistant)
	events := pub.recorded()
	if len(events) != 6 {
		t.Fatalf("expected 6 published events, got %d: %+v", len(events), events)
	}
	for i, ev := range events {
		if ev.SessionID != sess.ID {
			t.Errorf("events[%d].SessionID = %q, want %q", i, ev.SessionID, sess.ID)
		}
	}

	if events[0].Type != EventMessageAdded || events[0].Message == nil || events[0].Message.Role() != message.RoleUser || msgText(events[0].Message) != "Greetings!" {
		t.Errorf("events[0] = %+v, want user EventMessageAdded", events[0])
	}
	if events[0].Message.GetID() == "" {
		t.Errorf("expected generated UUID on user message in events[0]")
	}

	var chunkTexts []string
	for i := 1; i <= 3; i++ {
		if events[i].Type != EventChunk || events[i].Chunk == nil {
			t.Errorf("events[%d] = %+v, want EventChunk", i, events[i])
			continue
		}
		chunkTexts = append(chunkTexts, extractBlocksText(events[i].Chunk.Content))
	}
	wantChunks := []string{"Hello", ", ", "TaskSmith!"}
	if !reflect.DeepEqual(chunkTexts, wantChunks) {
		t.Errorf("chunkTexts = %v, want %v", chunkTexts, wantChunks)
	}

	if events[4].Type != EventMessageAdded || events[4].Message == nil || events[4].Message.Role() != message.RoleAssistant || msgText(events[4].Message) != "Hello, TaskSmith!" {
		t.Errorf("events[4] = %+v, want assistant EventMessageAdded", events[4])
	}
	if events[5].Type != EventCompleted || events[5].Message == nil || events[5].Message.Role() != message.RoleAssistant || msgText(events[5].Message) != "Hello, TaskSmith!" {
		t.Errorf("events[5] = %+v, want assistant EventCompleted", events[5])
	}

	// Verify session.History returns [User, Assistant] from the messages table
	history, err := mgr.History(ctx, sess.ID)
	if err != nil {
		t.Fatalf("History error: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 messages in History, got %d", len(history))
	}
	if history[0].Role() != message.RoleUser || msgText(history[0]) != "Greetings!" {
		t.Errorf("history[0] = (%s, %q), want (user, \"Greetings!\")", history[0].Role(), msgText(history[0]))
	}
	if history[1].Role() != message.RoleAssistant || msgText(history[1]) != "Hello, TaskSmith!" || history[1].GetID() != reply.GetID() {
		t.Errorf("history[1] = (%s, %q, %q), want (assistant, \"Hello, TaskSmith!\", %q)",
			history[1].Role(), msgText(history[1]), history[1].GetID(), reply.GetID())
	}
	if !(history[0].GetID() < history[1].GetID()) {
		t.Errorf("expected UUIDv7 chronological order history[0].ID (%q) < history[1].ID (%q)", history[0].GetID(), history[1].GetID())
	}

	// Verify harness.LoadState returns [User, Assistant] from the checkpoints table
	state := loadHarnessState(t, db, gw, sess.ID)
	if len(state.Messages) != 2 {
		t.Fatalf("expected 2 messages in harness checkpoint state, got %d", len(state.Messages))
	}
	if state.Messages[0].Role() != message.RoleUser || msgText(state.Messages[0]) != "Greetings!" {
		t.Errorf("state.Messages[0] = (%s, %q), want (user, \"Greetings!\")", state.Messages[0].Role(), msgText(state.Messages[0]))
	}
	if state.Messages[1].Role() != message.RoleAssistant || msgText(state.Messages[1]) != "Hello, TaskSmith!" {
		t.Errorf("state.Messages[1] = (%s, %q), want (assistant, \"Hello, TaskSmith!\")", state.Messages[1].Role(), msgText(state.Messages[1]))
	}
}

func TestAppendMessageIsolationFromHarnessCheckpoints(t *testing.T) {
	ctx := context.Background()
	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"Assistant reply"}, "asst-isolation-1", nil
		},
	}
	db, _, gw, pub, mgr := setupTestEnv(t, prov)

	sess, err := mgr.Create(ctx, CreateParams{Title: "System Notice Thread"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// Validation checks on AppendMessage
	if err := mgr.AppendMessage(ctx, "", message.NewSystemText("notice")); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("AppendMessage empty sessionID error = %v, want %v", err, ErrEmptySessionID)
	}
	if err := mgr.AppendMessage(ctx, "does-not-exist", message.NewSystemText("notice")); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("AppendMessage unknown sessionID error = %v, want %v", err, ErrSessionNotFound)
	}
	if err := mgr.AppendMessage(ctx, sess.ID, nil); !errors.Is(err, ErrNilMessage) {
		t.Errorf("AppendMessage nil message error = %v, want %v", err, ErrNilMessage)
	}

	// 1. Run a conversational turn via Send
	if _, err := mgr.Send(ctx, sess.ID, message.NewUserText("Hello agent")); err != nil {
		t.Fatalf("Send error: %v", err)
	}

	pub.reset()

	// 2. Append a chat-only system message via AppendMessage
	if err := mgr.AppendMessage(ctx, sess.ID, message.NewSystemText("Notice from system")); err != nil {
		t.Fatalf("AppendMessage error: %v", err)
	}

	// Verify Publisher received EventMessageAdded for the appended system message
	appendedEvents := pub.recorded()
	if len(appendedEvents) != 1 || appendedEvents[0].Type != EventMessageAdded || appendedEvents[0].Message == nil || appendedEvents[0].Message.Role() != message.RoleSystem {
		t.Fatalf("expected 1 EventMessageAdded for system message, got %+v", appendedEvents)
	}

	// 3. Verify the RoleSystem message appears in session.History (messages table)
	history, err := mgr.History(ctx, sess.ID)
	if err != nil {
		t.Fatalf("History error: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("expected 3 messages in session.History, got %d", len(history))
	}
	if history[0].Role() != message.RoleUser || msgText(history[0]) != "Hello agent" {
		t.Errorf("history[0] = (%s, %q), want (user, \"Hello agent\")", history[0].Role(), msgText(history[0]))
	}
	if history[1].Role() != message.RoleAssistant || msgText(history[1]) != "Assistant reply" {
		t.Errorf("history[1] = (%s, %q), want (assistant, \"Assistant reply\")", history[1].Role(), msgText(history[1]))
	}
	if history[2].Role() != message.RoleSystem || msgText(history[2]) != "Notice from system" {
		t.Errorf("history[2] = (%s, %q), want (system, \"Notice from system\")", history[2].Role(), msgText(history[2]))
	}
	if history[2].GetID() == "" {
		t.Errorf("expected generated UUID on appended system message")
	}

	// 4. Verify harness.LoadState (checkpoints table) is NOT polluted with the chat-only system message
	state := loadHarnessState(t, db, gw, sess.ID)
	if len(state.Messages) != 2 {
		t.Fatalf("expected 2 messages in harness checkpoint state, got %d: %+v", len(state.Messages), state.Messages)
	}
	for i, m := range state.Messages {
		if m.Role() == message.RoleSystem || strings.Contains(msgText(m), "Notice from system") {
			t.Errorf("harness checkpoint state.Messages[%d] was polluted with chat-only system message: %+v", i, m)
		}
	}
}

func TestFileTrackerCompactJournalAndBlobs(t *testing.T) {
	ctx := context.Background()
	_, ws, _, _, mgr := setupTestEnv(t, nil)

	sess, err := mgr.Create(ctx, CreateParams{Title: "File Tracker Session"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	if _, err := mgr.Tracker(ctx, "non-existent"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Tracker non-existent session error = %v, want %v", err, ErrSessionNotFound)
	}

	tr, err := mgr.Tracker(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Tracker error: %v", err)
	}

	sb, err := sandbox.New(sandbox.Config{
		WorkDir: ws.RootDir(),
		Mode:    sandbox.ModeLocal,
	})
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	t.Cleanup(func() {
		_ = sb.Close()
	})

	stdTools, err := std.New(std.Config{
		Sandbox: sb,
		Tracker: tr,
		Logger:  log.Default(),
	})
	if err != nil {
		t.Fatalf("std.New: %v", err)
	}

	// Create initial baseline file on disk
	initialContent := "line1\nline2\nline3\n"
	if err := sb.FS().MkdirAll(ctx, "pkg", 0o755); err != nil {
		t.Fatalf("MkdirAll pkg: %v", err)
	}
	if err := sb.FS().WriteFile(ctx, "pkg/app.go", []byte(initialContent), 0o644); err != nil {
		t.Fatalf("WriteFile initial: %v", err)
	}

	// 1. View file -> calls RecordRead in memory ONLY (must NOT create .jsonl or blobs on disk)
	viewRes, err := stdTools.View(ctx, std.ViewRequest{Path: "pkg/app.go"})
	if err != nil {
		t.Fatalf("stdTools.View: %v", err)
	}

	jPath := tr.journalPath("pkg/app.go")
	if _, err := os.Stat(jPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected View (RecordRead) NOT to create journal %q on disk, stat err = %v", jPath, err)
	}
	if _, err := os.Stat(tr.blobsDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected View (RecordRead) NOT to create blobs dir %q on disk, stat err = %v", tr.blobsDir, err)
	}

	// Verify CAS conflict detection when file is modified externally after View
	if err := sb.FS().WriteFile(ctx, "pkg/app.go", []byte("external_tamper\n"), 0o644); err != nil {
		t.Fatalf("WriteFile external tamper: %v", err)
	}
	if _, err := stdTools.Edit(ctx, std.EditRequest{
		Path:    "pkg/app.go",
		OldText: "line2",
		NewText: "line2_agent_edit",
	}); !errors.Is(err, std.ErrFileModified) {
		t.Fatalf("expected std.ErrFileModified after external tamper, got %v", err)
	}

	// Restore initial content and re-read via View
	if err := sb.FS().WriteFile(ctx, "pkg/app.go", []byte(initialContent), 0o644); err != nil {
		t.Fatalf("WriteFile restore initial: %v", err)
	}
	if _, err := stdTools.View(ctx, std.ViewRequest{Path: "pkg/app.go"}); err != nil {
		t.Fatalf("stdTools.View restore: %v", err)
	}

	// 2. Edit file -> calls RecordChange (writes Line 1 JournalHeader + Line 2 JournalEntry + blobs)
	editRes, err := stdTools.Edit(ctx, std.EditRequest{
		Path:    "pkg/app.go",
		OldText: "line2",
		NewText: "line2_agent_edit",
	})
	if err != nil {
		t.Fatalf("stdTools.Edit: %v", err)
	}

	// 3. Inspect raw <path_hash>.jsonl on disk
	rawBytes, err := os.ReadFile(jPath)
	if err != nil {
		t.Fatalf("ReadFile journal %q: %v", jPath, err)
	}
	lines := strings.Split(strings.TrimSpace(string(rawBytes)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines in journal (1 header + 1 entry), got %d:\n%s", len(lines), string(rawBytes))
	}

	// Verify Line 1 has "path" and "baseline"
	var line1Map map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &line1Map); err != nil {
		t.Fatalf("unmarshal line 1: %v", err)
	}
	if line1Map["path"] != "pkg/app.go" {
		t.Errorf("line 1 path = %v, want \"pkg/app.go\"", line1Map["path"])
	}
	if line1Map["baseline"] != viewRes.Hash {
		t.Errorf("line 1 baseline = %v, want %q", line1Map["baseline"], viewRes.Hash)
	}

	// Verify Line 2 does NOT contain "path" or "diff"
	var line2Map map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &line2Map); err != nil {
		t.Fatalf("unmarshal line 2: %v", err)
	}
	if _, hasPath := line2Map["path"]; hasPath {
		t.Errorf("line 2 unexpectedly contains redundant \"path\" field: %s", lines[1])
	}
	if _, hasDiff := line2Map["diff"]; hasDiff {
		t.Errorf("line 2 unexpectedly contains inline \"diff\" field: %s", lines[1])
	}

	// 4. Verify blobs exist and deduplicate identical content
	beforeBlob, err := tr.ReadBlob(viewRes.Hash)
	if err != nil {
		t.Fatalf("ReadBlob baseline: %v", err)
	}
	if string(beforeBlob) != initialContent {
		t.Errorf("beforeBlob = %q, want %q", string(beforeBlob), initialContent)
	}

	afterBlob, err := tr.ReadBlob(editRes.AfterHash)
	if err != nil {
		t.Fatalf("ReadBlob after: %v", err)
	}
	if string(afterBlob) != "line1\nline2_agent_edit\nline3\n" {
		t.Errorf("afterBlob = %q", string(afterBlob))
	}

	// RecordChange again with identical after content and verify only 2 blobs exist in blobs/
	dupChange := &std.FileChange{
		Path:         "pkg/app.go",
		Source:       std.SourceAgent,
		Tool:         "write",
		Kind:         std.KindModified,
		BeforeHash:   editRes.AfterHash,
		AfterHash:    editRes.AfterHash,
		AfterContent: afterBlob,
	}
	if err := tr.RecordChange(ctx, dupChange); err != nil {
		t.Fatalf("RecordChange duplicate: %v", err)
	}
	if dupChange.ID == "" {
		t.Errorf("expected RecordChange to populate dupChange.ID")
	}
	blobEntries, err := os.ReadDir(tr.blobsDir)
	if err != nil {
		t.Fatalf("ReadDir blobs: %v", err)
	}
	if len(blobEntries) != 2 {
		t.Errorf("expected exactly 2 deduplicated blobs, got %d", len(blobEntries))
	}
}

func TestFileTrackerDiffAndRecoverAt(t *testing.T) {
	ctx := context.Background()
	_, ws, _, _, mgr := setupTestEnv(t, nil)

	sess, err := mgr.Create(ctx, CreateParams{Title: "Recovery Session"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	tr, err := mgr.Tracker(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Tracker error: %v", err)
	}

	sb, err := sandbox.New(sandbox.Config{
		WorkDir: ws.RootDir(),
		Mode:    sandbox.ModeLocal,
	})
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	t.Cleanup(func() {
		_ = sb.Close()
	})

	stdTools, err := std.New(std.Config{
		Sandbox: sb,
		Tracker: tr,
		Logger:  log.Default(),
	})
	if err != nil {
		t.Fatalf("std.New: %v", err)
	}

	// Step 1: Agent creates file via Write ("v1\n")
	if _, err := stdTools.Write(ctx, std.WriteRequest{
		Path:    "notes.txt",
		Content: "v1\n",
	}); err != nil {
		t.Fatalf("Write v1: %v", err)
	}

	// Step 2: Agent edits file to "v2\n"
	if _, err := stdTools.Edit(ctx, std.EditRequest{
		Path:    "notes.txt",
		OldText: "v1",
		NewText: "v2",
	}); err != nil {
		t.Fatalf("Edit v2: %v", err)
	}

	// Step 3: External bash command modifies file to "v3_external\n"
	if _, err := stdTools.Bash(ctx, std.BashRequest{
		Command: "printf 'v3_external\\n' > notes.txt",
	}); err != nil {
		t.Fatalf("Bash v3_external: %v", err)
	}

	header, entries, err := tr.ReadJournal("notes.txt")
	if err != nil {
		t.Fatalf("ReadJournal: %v", err)
	}
	if header.Baseline != "" {
		t.Errorf("expected empty baseline for newly created file, got %q", header.Baseline)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 journal entries, got %d", len(entries))
	}
	if entries[0].Source != std.SourceAgent || entries[0].Kind != std.KindCreated {
		t.Errorf("entries[0] = %+v", entries[0])
	}
	if entries[1].Source != std.SourceAgent || entries[1].Kind != std.KindModified {
		t.Errorf("entries[1] = %+v", entries[1])
	}
	if entries[2].Source != std.SourceExternal || entries[2].Kind != std.KindModified {
		t.Errorf("entries[2] = %+v", entries[2])
	}

	// On-demand DiffEntry for entries[1] ("v1\n" -> "v2\n")
	diffText, err := tr.DiffEntry("notes.txt", entries[1].ID)
	if err != nil {
		t.Fatalf("DiffEntry: %v", err)
	}
	if !strings.Contains(diffText, "-v1") || !strings.Contains(diffText, "+v2") {
		t.Errorf("unexpected DiffEntry output:\n%s", diffText)
	}

	// RecoverAt entries[2] with before=true (state right before external bash edit -> "v2\n")
	if err := tr.RecoverAt(ctx, sb.FS(), "notes.txt", entries[2].ID, true); err != nil {
		t.Fatalf("RecoverAt before entries[2]: %v", err)
	}
	diskBytes, err := sb.FS().ReadFile(ctx, "notes.txt")
	if err != nil {
		t.Fatalf("ReadFile after RecoverAt: %v", err)
	}
	if string(diskBytes) != "v2\n" {
		t.Errorf("disk content after RecoverAt(entries[2], before=true) = %q, want \"v2\\n\"", string(diskBytes))
	}

	// RecoverAt entries[0] with before=false (state right after creation -> "v1\n")
	if err := tr.RecoverAt(ctx, sb.FS(), "notes.txt", entries[0].ID, false); err != nil {
		t.Fatalf("RecoverAt after entries[0]: %v", err)
	}
	diskBytes, err = sb.FS().ReadFile(ctx, "notes.txt")
	if err != nil {
		t.Fatalf("ReadFile after RecoverAt: %v", err)
	}
	if string(diskBytes) != "v1\n" {
		t.Errorf("disk content after RecoverAt(entries[0], before=false) = %q, want \"v1\\n\"", string(diskBytes))
	}

	// RecoverAt entries[0] with before=true (state before creation -> file removed)
	if err := tr.RecoverAt(ctx, sb.FS(), "notes.txt", entries[0].ID, true); err != nil {
		t.Fatalf("RecoverAt before entries[0]: %v", err)
	}
	if _, err := sb.FS().Stat(ctx, "notes.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected notes.txt to be removed after recovering before creation, err = %v", err)
	}
}

func TestFileTrackerRevertToBaselineThreeWayMerge(t *testing.T) {
	ctx := context.Background()
	_, ws, _, _, mgr := setupTestEnv(t, nil)

	sess, err := mgr.Create(ctx, CreateParams{Title: "Merge3 Revert Session"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	tr, err := mgr.Tracker(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Tracker error: %v", err)
	}

	sb, err := sandbox.New(sandbox.Config{
		WorkDir: ws.RootDir(),
		Mode:    sandbox.ModeLocal,
	})
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	t.Cleanup(func() {
		_ = sb.Close()
	})

	stdTools, err := std.New(std.Config{
		Sandbox: sb,
		Tracker: tr,
		Logger:  log.Default(),
	})
	if err != nil {
		t.Fatalf("std.New: %v", err)
	}

	// Initial baseline on disk: 5 lines
	baseline := "line1\nline2\nline3\nline4\nline5\n"
	if err := sb.FS().WriteFile(ctx, "service.go", []byte(baseline), 0o644); err != nil {
		t.Fatalf("WriteFile baseline: %v", err)
	}

	// Agent reads and edits line1 -> line1_agent
	if _, err := stdTools.View(ctx, std.ViewRequest{Path: "service.go"}); err != nil {
		t.Fatalf("View: %v", err)
	}
	if _, err := stdTools.Edit(ctx, std.EditRequest{
		Path:    "service.go",
		OldText: "line1",
		NewText: "line1_agent",
	}); err != nil {
		t.Fatalf("Edit line1: %v", err)
	}

	// User externally edits line5 -> line5_user (non-overlapping with line1)
	externalNonOverlapping := "line1_agent\nline2\nline3\nline4\nline5_user\n"
	if err := sb.FS().WriteFile(ctx, "service.go", []byte(externalNonOverlapping), 0o644); err != nil {
		t.Fatalf("WriteFile external non-overlapping: %v", err)
	}

	// RevertToBaseline with force=false should revert line1_agent -> line1 while preserving line5_user!
	if err := tr.RevertToBaseline(ctx, sb.FS(), "service.go", false); err != nil {
		t.Fatalf("RevertToBaseline (non-overlapping) error: %v", err)
	}
	mergedDisk, err := sb.FS().ReadFile(ctx, "service.go")
	if err != nil {
		t.Fatalf("ReadFile after RevertToBaseline: %v", err)
	}
	wantMerged := "line1\nline2\nline3\nline4\nline5_user\n"
	if string(mergedDisk) != wantMerged {
		t.Errorf("merged file =\n%s\nwant:\n%s", string(mergedDisk), wantMerged)
	}

	// Now test overlapping conflict: Agent edits line2 -> line2_agent, then user externally edits line2_agent -> line2_user_conflict
	if _, err := stdTools.View(ctx, std.ViewRequest{Path: "service.go"}); err != nil {
		t.Fatalf("View 2: %v", err)
	}
	if _, err := stdTools.Edit(ctx, std.EditRequest{
		Path:    "service.go",
		OldText: "line2",
		NewText: "line2_agent",
	}); err != nil {
		t.Fatalf("Edit line2: %v", err)
	}

	overlappingExternal := "line1\nline2_user_conflict\nline3\nline4\nline5_user\n"
	if err := sb.FS().WriteFile(ctx, "service.go", []byte(overlappingExternal), 0o644); err != nil {
		t.Fatalf("WriteFile overlapping external: %v", err)
	}

	if err := tr.RevertToBaseline(ctx, sb.FS(), "service.go", false); !errors.Is(err, ErrRevertConflict) {
		t.Errorf("RevertToBaseline overlapping error = %v, want %v", err, ErrRevertConflict)
	}

	// With force=true, RevertToBaseline restores exact original baseline
	if err := tr.RevertToBaseline(ctx, sb.FS(), "service.go", true); err != nil {
		t.Fatalf("RevertToBaseline(force=true) error: %v", err)
	}
	forcedDisk, err := sb.FS().ReadFile(ctx, "service.go")
	if err != nil {
		t.Fatalf("ReadFile after forced revert: %v", err)
	}
	if string(forcedDisk) != baseline {
		t.Errorf("forced revert content = %q, want %q", string(forcedDisk), baseline)
	}
}

func TestFileTrackerReloadAndDeleteSessionCleanup(t *testing.T) {
	ctx := context.Background()
	db, ws, gw, pub, mgr := setupTestEnv(t, nil)

	sess, err := mgr.Create(ctx, CreateParams{Title: "Persistent Tracker"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	tr1, err := mgr.Tracker(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Tracker 1 error: %v", err)
	}

	content := []byte("package main\n")
	hash := sha256Hex(content)
	if err := tr1.RecordChange(ctx, &std.FileChange{
		Path:         "main.go",
		Source:       std.SourceAgent,
		Tool:         "write",
		Kind:         std.KindCreated,
		AfterHash:    hash,
		AfterContent: content,
		Additions:    1,
	}); err != nil {
		t.Fatalf("RecordChange: %v", err)
	}

	// Construct a second Manager over the same DB and Workspace (simulating process restart)
	mgr2, err := New(Config{
		DB:        db,
		Workspace: ws,
		Gateway:   gw,
		Publisher: pub,
		Logger:    log.Default(),
	})
	if err != nil {
		t.Fatalf("New mgr2 error: %v", err)
	}

	tr2, err := mgr2.Tracker(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Tracker 2 error: %v", err)
	}

	gotHash, ok := tr2.LastHash("main.go")
	if !ok || gotHash != hash {
		t.Errorf("reloaded LastHash = (%q, %v), want (%q, true)", gotHash, ok, hash)
	}
	gotContent, ok := tr2.LastContent("main.go")
	if !ok || string(gotContent) != string(content) {
		t.Errorf("reloaded LastContent = (%q, %v), want (%q, true)", string(gotContent), ok, string(content))
	}
	if paths := tr2.TrackedPaths(); !reflect.DeepEqual(paths, []string{"main.go"}) {
		t.Errorf("reloaded TrackedPaths = %v, want [\"main.go\"]", paths)
	}

	// Delete session and verify <WorkspaceDir>/sessions/<sess.ID> is removed from disk
	sessDir, err := mgr2.sessionDir(sess.ID)
	if err != nil {
		t.Fatalf("sessionDir: %v", err)
	}
	if _, err := os.Stat(sessDir); err != nil {
		t.Fatalf("expected sessionDir %q to exist before Delete: %v", sessDir, err)
	}

	if err := mgr2.Delete(ctx, sess.ID); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if _, err := os.Stat(sessDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected sessionDir %q to be removed on Delete, stat err = %v", sessDir, err)
	}
}
