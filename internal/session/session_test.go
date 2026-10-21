package session

import (
	"context"
	"database/sql"
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
