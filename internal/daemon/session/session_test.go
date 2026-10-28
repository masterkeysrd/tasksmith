package session

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/harness"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type capturedTurn struct {
	model    string
	messages []message.Message
}

type fakeProvider struct {
	mu           sync.Mutex
	turns        []capturedTurn
	handler      func(ctx context.Context, req *llm.Request) ([]string, string, error)
	chunkHandler func(ctx context.Context, req *llm.Request) ([]message.AssistantChunk, error)
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
	chunkHandler := p.chunkHandler
	p.mu.Unlock()

	if chunkHandler != nil {
		customChunks, err := chunkHandler(ctx, req)
		if err != nil {
			return nil, err
		}
		return func(yield func(message.AssistantChunk, error) bool) {
			for _, ch := range customChunks {
				if err := ctx.Err(); err != nil {
					yield(message.AssistantChunk{}, err)
					return
				}
				if !yield(ch, nil) {
					return
				}
			}
		}, nil
	}

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

func (g *fakeGateway) ModelProfile(model string) (*profile.Profile, bool) {
	if g.provider != nil {
		if prof, found := g.provider.GetProfile(model); found {
			return &prof, true
		}
	}
	return nil, false
}

func (g *fakeGateway) Close() error {
	return nil
}

type pubRecord struct {
	topic string
	event Event
}

type testPublisher struct {
	mu      sync.Mutex
	events  []Event
	records []pubRecord
}

func (p *testPublisher) Publish(topic string, ev broker.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sev, ok := ev.(Event); ok {
		p.events = append(p.events, sev)
		p.records = append(p.records, pubRecord{topic: topic, event: sev})
	}
}

func (p *testPublisher) DeleteTopic(_ string) {}

func (p *testPublisher) recorded() []Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Event, len(p.events))
	copy(out, p.events)
	return out
}

func (p *testPublisher) recordedRecords() []pubRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]pubRecord, len(p.records))
	copy(out, p.records)
	return out
}

func (p *testPublisher) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = nil
	p.records = nil
}

func setupTestEnv(t *testing.T, prov *fakeProvider) (*sql.DB, workspace.Workspace, *fakeGateway, *testPublisher, Manager) {
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

	ws, err := workspace.Open(context.Background(), t.TempDir(), workspace.WithBuiltinFS(builtin.FS()))
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}

	logger := log.Default()
	gw := newFakeGateway(prov)
	pub := &testPublisher{}

	permMgr, err := permissions.New(permissions.Config{Workspace: ws})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}

	mgr, err := New(Config{
		DB:          db,
		Workspace:   ws,
		Gateway:     gw,
		Publisher:   pub,
		Logger:      logger,
		Permissions: permMgr,
	})
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}

	return db, ws, gw, pub, mgr
}

func loadHarnessState(t *testing.T, mgr Manager, db *sql.DB, gw *fakeGateway, sessionID string) harness.State {
	t.Helper()
	llmModel, err := gw.ResolveLLM(context.Background(), "fake-default-model")
	if err != nil {
		t.Fatalf("ResolveLLM: %v", err)
	}
	sess, err := mgr.Get(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("mgr.Get: %v", err)
	}
	h, err := harness.New(harness.Config{
		ID:          sessionID,
		DB:          db,
		Model:       llmModel,
		Tools:       sess.Tools,
		Bash:        sess.Std,
		Spiller:     sess.spiller,
		Permissions: sess.PermManager,
		Queue:       sess,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("harness.New: %v", err)
	}
	state, err := h.LoadState(context.Background())
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
	permMgr, err := permissions.New(permissions.Config{Workspace: ws})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}

	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "missing DB",
			cfg: Config{
				Workspace:   ws,
				Gateway:     gw,
				Publisher:   pub,
				Logger:      logger,
				Permissions: permMgr,
			},
		},
		{
			name: "missing Workspace",
			cfg: Config{
				DB:          db,
				Gateway:     gw,
				Publisher:   pub,
				Logger:      logger,
				Permissions: permMgr,
			},
		},
		{
			name: "missing Gateway",
			cfg: Config{
				DB:          db,
				Workspace:   ws,
				Publisher:   pub,
				Logger:      logger,
				Permissions: permMgr,
			},
		},
		{
			name: "missing Publisher",
			cfg: Config{
				DB:          db,
				Workspace:   ws,
				Gateway:     gw,
				Logger:      logger,
				Permissions: permMgr,
			},
		},
		{
			name: "missing Logger",
			cfg: Config{
				DB:          db,
				Workspace:   ws,
				Gateway:     gw,
				Publisher:   pub,
				Permissions: permMgr,
			},
		},
		{
			name: "missing Permissions",
			cfg: Config{
				DB:        db,
				Workspace: ws,
				Gateway:   gw,
				Publisher: pub,
				Logger:    logger,
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
	s1, _, err := mgr.Create(ctx, CreateParams{Title: "First Session"})
	if err != nil {
		t.Fatalf("Create s1 error: %v", err)
	}
	if s1.ID == "" {
		t.Fatal("expected non-empty s1.ID")
	}
	if s1.Title != "First Session" {
		t.Errorf("s1.Title = %q, want %q", s1.Title, "First Session")
	}
	if s1.Config.Agent == "" {
		t.Errorf("expected default s1.Agent to be resolved from workspace, got empty")
	}
	if s1.CreatedAt.IsZero() || s1.UpdatedAt.IsZero() {
		t.Errorf("expected non-zero timestamps on s1: %+v", s1)
	}

	// Ensure slightly later timestamp for s2
	time.Sleep(2 * time.Millisecond)

	// Create session 2
	s2, _, err := mgr.Create(ctx, CreateParams{Title: "Second Session"})
	if err != nil {
		t.Fatalf("Create s2 error: %v", err)
	}

	// Get s1
	got1, err := mgr.Get(ctx, s1.ID)
	if err != nil {
		t.Fatalf("Get s1 error: %v", err)
	}
	if got1.ID != s1.ID || got1.Title != s1.Title || got1.Config.Agent != s1.Config.Agent || got1.Config.Model != s1.Config.Model || !got1.CreatedAt.Equal(s1.CreatedAt) {
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
	if err := s1.appendMessage(ctx, message.NewUserText("Hello on s1")); err != nil {
		t.Fatalf("appendMessage s1 error: %v", err)
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

	// Delete s1 and verify both session and its timeline entries are gone
	if err := mgr.Delete(ctx, s1.ID); err != nil {
		t.Fatalf("Delete s1 error: %v", err)
	}
	if _, err := mgr.Get(ctx, s1.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Get deleted s1 error = %v, want %v", err, ErrSessionNotFound)
	}

	var msgCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM timeline WHERE session_id = ?`, s1.ID).Scan(&msgCount); err != nil {
		t.Fatalf("count timeline entries for deleted s1: %v", err)
	}
	if msgCount != 0 {
		t.Errorf("expected 0 timeline entries for deleted session s1, got %d", msgCount)
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

	ws, err := workspace.Open(ctx, wsDir, workspace.WithBuiltinFS(builtin.FS()))
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

	permMgr, err := permissions.New(permissions.Config{Workspace: ws})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}

	mgr, err := New(Config{
		DB:          db,
		Workspace:   ws,
		Gateway:     gw,
		Publisher:   pub,
		Logger:      log.Default(),
		Permissions: permMgr,
	})
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}

	// Session A: uses workspace defaults for Agent and Model
	sessDefault, _, err := mgr.Create(ctx, CreateParams{Title: "Default Session"})
	if err != nil {
		t.Fatalf("Create default session error: %v", err)
	}
	if sessDefault.Config.Agent != "custom-default-agent" {
		t.Errorf("sessDefault.Agent = %q, want %q", sessDefault.Config.Agent, "custom-default-agent")
	}
	if sessDefault.Config.Model != "openai/gpt-5.5-default" {
		t.Errorf("sessDefault.Model = %q, want %q", sessDefault.Config.Model, "openai/gpt-5.5-default")
	}

	// Session B: explicit Agent and Model overrides in the same workspace
	sessOverride, _, err := mgr.Create(ctx, CreateParams{
		Title: "Override Session",
		Config: SessionConfig{
			Agent: "override-agent",
			Model: "anthropic/claude-4-sonnet",
		},
	})
	if err != nil {
		t.Fatalf("Create override session error: %v", err)
	}
	if sessOverride.Config.Agent != "override-agent" {
		t.Errorf("sessOverride.Agent = %q, want %q", sessOverride.Config.Agent, "override-agent")
	}
	if sessOverride.Config.Model != "anthropic/claude-4-sonnet" {
		t.Errorf("sessOverride.Model = %q, want %q", sessOverride.Config.Model, "anthropic/claude-4-sonnet")
	}

	// Send on both concurrent sessions and verify each uses its own Agent prompt and Model
	if err := sessDefault.Send(ctx, SendInput{Message: message.NewUserText("Hello default")}); err != nil {
		t.Fatalf("Send sessDefault error: %v", err)
	}
	if err := sessDefault.Wait(ctx); err != nil {
		t.Fatalf("Wait sessDefault error: %v", err)
	}
	if err := sessOverride.Send(ctx, SendInput{Message: message.NewUserText("Hello override")}); err != nil {
		t.Fatalf("Send sessOverride error: %v", err)
	}
	if err := sessOverride.Wait(ctx); err != nil {
		t.Fatalf("Wait sessOverride error: %v", err)
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

	// Update Session A: update title, agent, and bare model (resolving with defaultProvider openai)
	updatedA, err := mgr.Update(ctx, sessDefault.ID, UpdateParams{
		Title: "Renamed Default Session",
		Config: SessionConfig{
			Agent: "override-agent",
			Model: "gpt-5.5-default",
		},
	})
	if err != nil {
		t.Fatalf("Update session error: %v", err)
	}
	if updatedA.Title != "Renamed Default Session" {
		t.Errorf("updatedA.Title = %q, want %q", updatedA.Title, "Renamed Default Session")
	}
	if updatedA.Config.Agent != "override-agent" {
		t.Errorf("updatedA.Agent = %q, want %q", updatedA.Config.Agent, "override-agent")
	}
	if updatedA.Config.Model != "openai/gpt-5.5-default" {
		t.Errorf("updatedA.Model = %q, want %q", updatedA.Config.Model, "openai/gpt-5.5-default")
	}

	// Update Session A again with fully qualified model
	updatedA2, err := mgr.Update(ctx, sessDefault.ID, UpdateParams{
		Config: SessionConfig{
			Model: "anthropic/claude-4-sonnet",
		},
	})
	if err != nil {
		t.Fatalf("Update session model: %v", err)
	}
	if updatedA2.Config.Model != "anthropic/claude-4-sonnet" {
		t.Errorf("updatedA2.Model = %q, want %q", updatedA2.Config.Model, "anthropic/claude-4-sonnet")
	}

	// Verify persistence via Get
	fetchedA, err := mgr.Get(ctx, sessDefault.ID)
	if err != nil {
		t.Fatalf("Get updated session: %v", err)
	}
	if fetchedA.Title != updatedA2.Title || fetchedA.Config.Agent != updatedA2.Config.Agent || fetchedA.Config.Model != updatedA2.Config.Model {
		t.Errorf("Get mismatch after update: got %+v, want %+v", fetchedA, updatedA2)
	}

	// Error cases
	if _, err := mgr.Update(ctx, "", UpdateParams{Title: "X"}); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("Update empty ID: got %v, want ErrEmptySessionID", err)
	}
	if _, err := mgr.Update(ctx, "non-existent-id", UpdateParams{Title: "X"}); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Update nonexistent ID: got %v, want ErrSessionNotFound", err)
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

	sess, _, err := mgr.Create(ctx, CreateParams{Title: "Chat Loop"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// Validation checks on Send and History
	if err := sess.Send(ctx, SendInput{Message: nil}); !errors.Is(err, ErrNilMessage) {
		t.Errorf("Send nil input error = %v, want %v", err, ErrNilMessage)
	}

	pub.reset()

	userIn := message.NewUserText("Greetings!")
	if err := sess.Send(ctx, SendInput{Message: userIn}); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait error: %v", err)
	}

	msgs, err := sess.History(ctx)
	if err != nil || len(msgs) < 2 {
		t.Fatalf("History error (%v) or too few messages (%d)", err, len(msgs))
	}
	reply, ok := msgs[1].(*message.Assistant)
	if !ok || reply == nil {
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
	// 0. StatusChangedEvent (StatusThinking)
	// 1. EntryAddedEvent (user message)
	// 2. StatusChangedEvent (StatusGenerating)
	// 3..5. ChunkEvent ("Hello", ", ", "TaskSmith!")
	// 6. EntryAddedEvent (assistant message)
	// 7. ContextUpdatedEvent (Context state)
	// 8. StatusChangedEvent (StatusIdle)
	// 9. CompletedEvent (assistant reply)
	events := pub.recorded()
	if len(events) != 10 {
		t.Fatalf("expected 10 published events, got %d: %+v", len(events), events)
	}
	for i, ev := range events {
		var sessionID string
		switch e := ev.(type) {
		case StatusChangedEvent:
			sessionID = e.SessionID
		case EntryAddedEvent:
			sessionID = e.SessionID
		case ChunkEvent:
			sessionID = e.SessionID
		case PermissionRequestedEvent:
			sessionID = e.SessionID
		case ContextUpdatedEvent:
			sessionID = e.SessionID
		case CompletedEvent:
			sessionID = e.SessionID
		}
		if sessionID != sess.ID {
			t.Errorf("events[%d].SessionID = %q, want %q", i, sessionID, sess.ID)
		}
	}

	sc0, ok := events[0].(StatusChangedEvent)
	if !ok || sc0.Status != StatusThinking {
		t.Errorf("events[0] = %+v, want StatusChangedEvent(StatusThinking)", events[0])
	}

	ev1, ok := events[1].(EntryAddedEvent)
	if !ok || ev1.Entry.Kind != timeline.KindMessage || ev1.Entry.Message == nil || ev1.Entry.Message.Role() != message.RoleUser || msgText(ev1.Entry.Message) != "Greetings!" {
		t.Errorf("events[1] = %+v, want user EntryAddedEvent", events[1])
	}
	if ev1.Entry.ID == "" || ev1.Entry.Message.GetID() == "" {
		t.Errorf("expected generated UUID on user message in events[1]")
	}

	sc2, ok := events[2].(StatusChangedEvent)
	if !ok || sc2.Status != StatusGenerating {
		t.Errorf("events[2] = %+v, want StatusChangedEvent(StatusGenerating)", events[2])
	}

	var chunkTexts []string
	for i := 3; i <= 5; i++ {
		chkEv, ok := events[i].(ChunkEvent)
		if !ok {
			t.Errorf("events[%d] = %+v, want ChunkEvent", i, events[i])
			continue
		}
		chunkTexts = append(chunkTexts, extractBlocksText(chkEv.Chunk.Content))
	}
	wantChunks := []string{"Hello", ", ", "TaskSmith!"}
	if !reflect.DeepEqual(chunkTexts, wantChunks) {
		t.Errorf("chunkTexts = %v, want %v", chunkTexts, wantChunks)
	}

	ev6, ok := events[6].(EntryAddedEvent)
	if !ok || ev6.Entry.Kind != timeline.KindMessage || ev6.Entry.Message == nil || ev6.Entry.Message.Role() != message.RoleAssistant || msgText(ev6.Entry.Message) != "Hello, TaskSmith!" {
		t.Errorf("events[6] = %+v, want assistant EntryAddedEvent", events[6])
	}
	ctxEv7, ok := events[7].(ContextUpdatedEvent)
	if !ok || ctxEv7.SessionID != sess.ID || ctxEv7.Context.UsedTokens == 0 {
		t.Errorf("events[7] = %+v, want ContextUpdatedEvent", events[7])
	}
	sc8, ok := events[8].(StatusChangedEvent)
	if !ok || sc8.Status != StatusIdle {
		t.Errorf("events[8] = %+v, want StatusChangedEvent(StatusIdle)", events[8])
	}
	ev9, ok := events[9].(CompletedEvent)
	if !ok || ev9.SessionID != sess.ID {
		t.Errorf("events[9] = %+v, want CompletedEvent for session %q", events[9], sess.ID)
	}

	// Verify session.History returns [User, Assistant] from the messages table
	history, err := sess.History(ctx)
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
	if history[0].GetID() >= history[1].GetID() {
		t.Errorf("expected UUIDv7 chronological order history[0].ID (%q) < history[1].ID (%q)", history[0].GetID(), history[1].GetID())
	}

	// Verify harness.LoadState returns [User, Assistant] from the checkpoints table
	state := loadHarnessState(t, mgr, db, gw, sess.ID)
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

	sess, _, err := mgr.Create(ctx, CreateParams{Title: "System Notice Thread"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// Validation checks on appendMessage
	if err := sess.appendMessage(ctx, nil); !errors.Is(err, ErrNilMessage) {
		t.Errorf("appendMessage nil message error = %v, want %v", err, ErrNilMessage)
	}

	// 1. Run a conversational turn via Send
	if err := sess.Send(ctx, SendInput{Message: message.NewUserText("Hello agent")}); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait error: %v", err)
	}

	pub.reset()

	// 2. Append a chat-only system message via appendMessage
	if err := sess.appendMessage(ctx, message.NewSystemText("Notice from system")); err != nil {
		t.Fatalf("appendMessage error: %v", err)
	}

	// Verify Publisher received EventEntryAdded for the appended system message
	appendedEvents := pub.recorded()
	if len(appendedEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(appendedEvents))
	}
	appEv, ok := appendedEvents[0].(EntryAddedEvent)
	if !ok || appEv.Entry.Kind != timeline.KindMessage || appEv.Entry.Message == nil || appEv.Entry.Message.Role() != message.RoleSystem {
		t.Fatalf("expected 1 EntryAddedEvent for system message, got %+v", appendedEvents)
	}

	// 3. Verify the RoleSystem message appears in session.History (messages table)
	history, err := sess.History(ctx)
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
	state := loadHarnessState(t, mgr, db, gw, sess.ID)
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

	sess, _, err := mgr.Create(ctx, CreateParams{Title: "File Tracker Session"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	jr := sess.Journal

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
		Tracker: jr,
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

	// 1. View file -> calls RecordRead in memory ONLY (must NOT create blobs on disk)
	viewRes, err := stdTools.View(ctx, std.ViewRequest{Path: "pkg/app.go"})
	if err != nil {
		t.Fatalf("stdTools.View: %v", err)
	}

	blobsDir := filepath.Join(sess.Root, "blobs")
	if _, err := os.Stat(blobsDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected View (RecordRead) NOT to create blobs dir %q on disk, stat err = %v", blobsDir, err)
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

	// 2. Edit file -> calls RecordChange (persists to SQLite + blobs)
	editRes, err := stdTools.Edit(ctx, std.EditRequest{
		Path:    "pkg/app.go",
		OldText: "line2",
		NewText: "line2_agent_edit",
	})
	if err != nil {
		t.Fatalf("stdTools.Edit: %v", err)
	}

	// 3. Inspect journal entries via ChangesForPath
	entries, err := jr.ChangesForPath(ctx, "pkg/app.go")
	if err != nil {
		t.Fatalf("ChangesForPath: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 journal entry, got %d", len(entries))
	}
	if entries[0].Path != "pkg/app.go" {
		t.Errorf("entry path = %v, want \"pkg/app.go\"", entries[0].Path)
	}
	if entries[0].BeforeHash != viewRes.Hash {
		t.Errorf("entry before_hash = %v, want %q", entries[0].BeforeHash, viewRes.Hash)
	}
	if entries[0].AfterHash != editRes.AfterHash {
		t.Errorf("entry after_hash = %v, want %q", entries[0].AfterHash, editRes.AfterHash)
	}

	// 4. Verify blobs exist and deduplicate identical content
	beforeBlob, err := os.ReadFile(filepath.Join(blobsDir, viewRes.Hash))
	if err != nil {
		t.Fatalf("ReadBlob baseline: %v", err)
	}
	if string(beforeBlob) != initialContent {
		t.Errorf("beforeBlob = %q, want %q", string(beforeBlob), initialContent)
	}

	afterBlob, err := os.ReadFile(filepath.Join(blobsDir, editRes.AfterHash))
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
	if err := jr.RecordChange(ctx, dupChange); err != nil {
		t.Fatalf("RecordChange duplicate: %v", err)
	}
	if dupChange.ID == "" {
		t.Errorf("expected RecordChange to populate dupChange.ID")
	}
	blobEntries, err := os.ReadDir(blobsDir)
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

	sess, _, err := mgr.Create(ctx, CreateParams{Title: "Recovery Session"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	jr := sess.Journal

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
		Tracker: jr,
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

	entries, err := jr.ChangesForPath(ctx, "notes.txt")
	if err != nil {
		t.Fatalf("ChangesForPath: %v", err)
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
	diffText, err := jr.DiffEntry(ctx, "notes.txt", entries[1].ID)
	if err != nil {
		t.Fatalf("DiffEntry: %v", err)
	}
	if !strings.Contains(diffText, "-v1") || !strings.Contains(diffText, "+v2") {
		t.Errorf("unexpected DiffEntry output:\n%s", diffText)
	}

	// RecoverAt entries[2] with before=true (state right before external bash edit -> "v2\n")
	if err := jr.RecoverAt(ctx, sb.FS(), "notes.txt", entries[2].ID, true); err != nil {
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
	if err := jr.RecoverAt(ctx, sb.FS(), "notes.txt", entries[0].ID, false); err != nil {
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
	if err := jr.RecoverAt(ctx, sb.FS(), "notes.txt", entries[0].ID, true); err != nil {
		t.Fatalf("RecoverAt before entries[0]: %v", err)
	}
	if _, err := sb.FS().Stat(ctx, "notes.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected notes.txt to be removed after recovering before creation, err = %v", err)
	}
}

func TestFileTrackerRevertToBaselineThreeWayMerge(t *testing.T) {
	ctx := context.Background()
	_, ws, _, _, mgr := setupTestEnv(t, nil)

	sess, _, err := mgr.Create(ctx, CreateParams{Title: "Merge3 Revert Session"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	jr := sess.Journal

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
		Tracker: jr,
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
	if err := jr.RevertToBaseline(ctx, sb.FS(), "service.go", false); err != nil {
		t.Fatalf("RevertToBaseline (non-overlapping) error: %v", err)
	}
	mergedDisk, err := sb.FS().ReadFile(ctx, "service.go")
	if err != nil {
		t.Fatalf("ReadFile after RevertToBaseline: %v", err)
	}
	wantMerged := "line1\nline2\nline3\nline4\n5_user\n"
	_ = wantMerged
	wantMerged = "line1\nline2\nline3\nline4\nline5_user\n"
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

	if err := jr.RevertToBaseline(ctx, sb.FS(), "service.go", false); !errors.Is(err, ErrRevertConflict) {
		t.Errorf("RevertToBaseline overlapping error = %v, want %v", err, ErrRevertConflict)
	}

	// With force=true, RevertToBaseline restores exact original baseline
	if err := jr.RevertToBaseline(ctx, sb.FS(), "service.go", true); err != nil {
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

	sess, _, err := mgr.Create(ctx, CreateParams{Title: "Persistent Tracker"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	jr1 := sess.Journal

	content := []byte("package main\n")
	hash := sha256Hex(content)
	if err := jr1.RecordChange(ctx, &std.FileChange{
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
	permMgr, err := permissions.New(permissions.Config{Workspace: ws})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}
	mgr2, err := New(Config{
		DB:          db,
		Workspace:   ws,
		Gateway:     gw,
		Publisher:   pub,
		Logger:      log.Default(),
		Permissions: permMgr,
	})
	if err != nil {
		t.Fatalf("New mgr2 error: %v", err)
	}

	sess2, err := mgr2.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get 2 error: %v", err)
	}
	jr2 := sess2.Journal

	gotHash, ok := jr2.LastHash("main.go")
	if !ok || gotHash != hash {
		t.Errorf("reloaded LastHash = (%q, %v), want (%q, true)", gotHash, ok, hash)
	}
	gotContent, ok := jr2.LastContent("main.go")
	if !ok || string(gotContent) != string(content) {
		t.Errorf("reloaded LastContent = (%q, %v), want (%q, true)", string(gotContent), ok, string(content))
	}
	if paths := jr2.TrackedPaths(); !reflect.DeepEqual(paths, []string{"main.go"}) {
		t.Errorf("reloaded TrackedPaths = %v, want [\"main.go\"]", paths)
	}

	// Delete session and verify <WorkspaceDir>/sessions/<sess.ID> is removed from disk
	sessDir := sess.Root
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

func TestMultiStepToolExecutionAndFileTracker(t *testing.T) {
	ctx := context.Background()

	var step int
	prov := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) ([]message.AssistantChunk, error) {
			step++
			if step == 1 {
				return []message.AssistantChunk{
					{
						BaseChunk: message.BaseChunk{ID: uuid.NewString()},
						Model:     req.Model,
						Content: []message.Block{
							&message.TextBlock{Text: "Let me view that file."},
							&message.ToolCall{
								ID:   "tc-view-1",
								Name: "view",
								Args: map[string]any{
									"path": "hello.txt",
								},
							},
						},
						Done: true,
					},
				}, nil
			}
			// Step 2: verify the tool result message is present in req.Messages
			var foundToolOutput bool
			for _, m := range req.Messages {
				if tm, ok := m.(*message.Tool); ok {
					if viewOut, ok := tm.StructuredContent.(api.ViewOutput); ok && strings.Contains(viewOut.Content, "Hello from disk!") {
						foundToolOutput = true
						break
					}
					if strings.Contains(msgText(m), "Hello from disk!") {
						foundToolOutput = true
						break
					}
				}
			}
			if !foundToolOutput {
				return nil, errors.New("expected tool output containing 'Hello from disk!' in second LLM turn")
			}
			return []message.AssistantChunk{
				{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content: []message.Block{
						&message.TextBlock{Text: "The file says: Hello from disk!"},
					},
					Done: true,
				},
			}, nil
		},
	}

	_, ws, _, pub, mgr := setupTestEnv(t, prov)

	fileBytes := []byte("Hello from disk!\n")
	if err := os.WriteFile(filepath.Join(ws.RootDir(), "hello.txt"), fileBytes, 0o644); err != nil {
		t.Fatalf("WriteFile hello.txt: %v", err)
	}

	sess, _, err := mgr.Create(ctx, CreateParams{Title: "Multi-step Tool Session"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	pub.reset()

	if err := sess.Send(ctx, SendInput{Message: message.NewUserText("Read hello.txt please")}); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait error: %v", err)
	}

	// Verify History in SQLite messages table contains:
	// [User, Assistant(ToolCall), Tool(Result), Assistant(Final)] in exact id ASC order!
	history, err := sess.History(ctx)
	if err != nil {
		t.Fatalf("History error: %v", err)
	}
	if len(history) != 4 {
		t.Fatalf("expected 4 messages in History, got %d: %+v", len(history), history)
	}

	reply, ok := history[len(history)-1].(*message.Assistant)
	if !ok || reply == nil {
		t.Fatal("expected non-nil final assistant reply")
	}
	if got := msgText(reply); got != "The file says: Hello from disk!" {
		t.Errorf("reply text = %q, want %q", got, "The file says: Hello from disk!")
	}
	if len(history) != 4 {
		t.Fatalf("expected 4 messages in History, got %d: %+v", len(history), history)
	}

	wantRoles := []message.Role{
		message.RoleUser,
		message.RoleAssistant,
		message.RoleTool,
		message.RoleAssistant,
	}
	for i, wantRole := range wantRoles {
		if history[i].Role() != wantRole {
			t.Errorf("history[%d].Role() = %q, want %q", i, history[i].Role(), wantRole)
		}
		if i > 0 && (history[i-1].GetID() >= history[i].GetID()) {
			t.Errorf("expected chronological UUIDv7 ordering history[%d].ID (%q) < history[%d].ID (%q)",
				i-1, history[i-1].GetID(), i, history[i].GetID())
		}
	}

	asstToolMsg, ok := history[1].(*message.Assistant)
	if !ok || len(asstToolMsg.ToolCalls()) != 1 || asstToolMsg.ToolCalls()[0].Name != "view" {
		t.Errorf("history[1] = %+v, want Assistant with 'view' tool call", history[1])
	}
	toolHistMsg, ok := history[2].(*message.Tool)
	if !ok {
		t.Fatalf("history[2] is %T, want *message.Tool", history[2])
	}
	var viewOut api.ViewOutput
	if vo, ok := toolHistMsg.StructuredContent.(api.ViewOutput); ok {
		viewOut = vo
	} else if raw, err := json.Marshal(toolHistMsg.StructuredContent); err == nil {
		_ = json.Unmarshal(raw, &viewOut)
	}
	if !strings.Contains(viewOut.Content, "Hello from disk!") && !strings.Contains(msgText(history[2]), "Hello from disk!") {
		t.Errorf("history[2] tool result = %q (structured: %+v), want to contain 'Hello from disk!'", msgText(history[2]), toolHistMsg.StructuredContent)
	}
	if msgText(history[3]) != "The file says: Hello from disk!" {
		t.Errorf("history[3] final text = %q, want 'The file says: Hello from disk!'", msgText(history[3]))
	}

	// Verify Journal(ctx, sess.ID) recorded the file read!
	jr := sess.Journal
	gotHash, ok := jr.LastHash("hello.txt")
	if !ok || gotHash != sha256Hex(fileBytes) {
		t.Errorf("Journal.LastHash(\"hello.txt\") = (%q, %v), want (%q, true)", gotHash, ok, sha256Hex(fileBytes))
	}
}

func TestMultiPermissionInterruptAndAllOrNothingRespondPermission(t *testing.T) {
	ctx := context.Background()

	var step int
	prov := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) ([]message.AssistantChunk, error) {
			step++
			if step == 1 {
				return []message.AssistantChunk{
					{
						BaseChunk: message.BaseChunk{ID: uuid.NewString()},
						Model:     req.Model,
						Content: []message.Block{
							&message.TextBlock{Text: "I will write both files."},
							&message.ToolCall{
								ID:   "tc-write-1",
								Name: "write",
								Args: map[string]any{
									"path":    "file1.txt",
									"content": "content one\n",
								},
							},
							&message.ToolCall{
								ID:   "tc-write-2",
								Name: "write",
								Args: map[string]any{
									"path":    "file2.txt",
									"content": "content two\n",
								},
							},
						},
						Done: true,
					},
				}, nil
			}
			return []message.AssistantChunk{
				{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content: []message.Block{
						&message.TextBlock{Text: "Wrote file1.txt and skipped file2.txt as requested."},
					},
					Done: true,
				},
			}, nil
		},
	}

	_, ws, _, pub, mgr := setupTestEnv(t, prov)

	sess, _, err := mgr.Create(ctx, CreateParams{Title: "All-Or-Nothing Permissions"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	pub.reset()

	// 1. Send triggers 2 write tool calls -> interrupts with 2 permissions
	err = sess.Send(ctx, SendInput{Message: message.NewUserText("Write file1.txt and file2.txt")})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait error: %v", err)
	}
	if sess.Status != StatusWaitingPermission {
		t.Fatalf("expected StatusWaitingPermission, got %v", sess.Status)
	}

	// Verify PermissionRequestedEvent was published with 2 permissions
	events := pub.recorded()
	var permEvent *PermissionRequestedEvent
	for i := range events {
		if pe, ok := events[i].(PermissionRequestedEvent); ok {
			permEvent = &pe
		}
	}
	if permEvent == nil || len(permEvent.Permissions) != 2 {
		t.Fatalf("expected PermissionRequestedEvent with 2 permissions, got %+v", permEvent)
	}

	// Verify PendingPermissions returns 2 items
	liveSess, err := mgr.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	pending := liveSess.PendingPermissions
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending permissions, got %d", len(pending))
	}

	// Validation error checks on RespondPermission
	if err := sess.Send(ctx, SendInput{
		Decision: &tools.PermissionDecision{
			RequestID: pending[0].ID,
			Kind:      "invalid_kind",
		},
	}); !errors.Is(err, ErrInvalidDecision) {
		t.Errorf("RespondPermission invalid kind error = %v, want %v", err, ErrInvalidDecision)
	}
	if err := sess.Send(ctx, SendInput{
		Decision: &tools.PermissionDecision{
			RequestID: "non-existent-req-id",
			Kind:      tools.DecisionAllowOnce,
		},
	}); !errors.Is(err, ErrPermissionNotFound) {
		t.Errorf("RespondPermission unknown request ID error = %v, want %v", err, ErrPermissionNotFound)
	}

	pub.reset()

	// 2. Respond to permission #1 ONLY -> must buffer decision and return WITHOUT calling the graph!
	err = sess.Send(ctx, SendInput{
		Decision: &tools.PermissionDecision{
			RequestID: pending[0].ID,
			Kind:      tools.DecisionAllowOnce,
		},
	})
	if err != nil {
		t.Fatalf("RespondPermission #1 error: %v", err)
	}
	if len(prov.recordedTurns()) != 1 {
		t.Fatalf("expected graph NOT to be called until all permissions resolved, got %d LLM turns", len(prov.recordedTurns()))
	}
	recs := pub.recorded()
	if len(recs) != 1 {
		t.Fatalf("expected 1 event, got %+v", recs)
	}
	decEv, ok := recs[0].(EntryAddedEvent)
	if !ok || decEv.Entry.Kind != timeline.KindPermission {
		t.Fatalf("expected 1 EntryAddedEvent (permission) published when recording decision, got %+v", recs)
	}
	pub.reset()
	if _, err := os.Stat(filepath.Join(ws.RootDir(), "file1.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected file1.txt NOT to be written before all permissions are resolved, stat err = %v", err)
	}

	// Verify PendingPermissions now returns ONLY permission #2!
	liveSess, err = mgr.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	remaining := liveSess.PendingPermissions
	if len(remaining) != 1 || remaining[0].ID != pending[1].ID {
		t.Fatalf("PendingPermissions after #1 = %+v, want only [%s]", remaining, pending[1].ID)
	}

	// 3. Respond to permission #2 (rejecting it with feedback) -> triggers h.Resume!
	err = sess.Send(ctx, SendInput{
		Decision: &tools.PermissionDecision{
			RequestID: pending[1].ID,
			Kind:      tools.DecisionReject,
			Feedback:  "Do not create file2.txt",
		},
	})
	if err != nil {
		t.Fatalf("RespondPermission #2 error: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait error: %v", err)
	}

	history, err := sess.History(ctx)
	if err != nil {
		t.Fatalf("History error: %v", err)
	}
	finalReply, ok := history[len(history)-1].(*message.Assistant)
	if !ok || finalReply == nil {
		t.Fatal("expected non-nil final assistant reply after resolving all permissions")
	}
	if got := msgText(finalReply); got != "Wrote file1.txt and skipped file2.txt as requested." {
		t.Errorf("finalReply = %q, want %q", got, "Wrote file1.txt and skipped file2.txt as requested.")
	}

	// Verify file1.txt was written and tracked in session's FileTracker journal!
	disk1, err := os.ReadFile(filepath.Join(ws.RootDir(), "file1.txt"))
	if err != nil {
		t.Fatalf("ReadFile file1.txt: %v", err)
	}
	if string(disk1) != "content one\n" {
		t.Errorf("file1.txt content = %q, want \"content one\\n\"", string(disk1))
	}
	if _, err := os.Stat(filepath.Join(ws.RootDir(), "file2.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected rejected file2.txt not to exist on disk, stat err = %v", err)
	}

	jr := sess.Journal
	entries, err := jr.ChangesForPath(ctx, "file1.txt")
	if err != nil {
		t.Fatalf("ChangesForPath(\"file1.txt\") error: %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != std.KindCreated || entries[0].Tool != "write" {
		t.Errorf("unexpected journal entries for file1.txt: %+v", entries)
	}

	// Verify PendingPermissions is now empty and RespondPermission returns ErrPermissionNotFound
	liveSess, err = mgr.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	afterPerms := liveSess.PendingPermissions
	if len(afterPerms) != 0 {
		t.Errorf("expected 0 pending permissions after completion, got %d", len(afterPerms))
	}
	if err := sess.Send(ctx, SendInput{
		Decision: &tools.PermissionDecision{
			RequestID: pending[0].ID,
			Kind:      tools.DecisionAllowOnce,
		},
	}); !errors.Is(err, ErrPermissionNotFound) {
		t.Errorf("RespondPermission with no pending permissions error = %v, want %v", err, ErrPermissionNotFound)
	}

	// Verify History contains [User, Assistant(ToolCalls), Tool1, Tool2, Assistant(Final)] in exact id ASC order
	if len(history) != 5 {
		t.Fatalf("expected 5 messages in History, got %d: %+v", len(history), history)
	}
	wantRoles := []message.Role{
		message.RoleUser,
		message.RoleAssistant,
		message.RoleTool,
		message.RoleTool,
		message.RoleAssistant,
	}
	for i, wantRole := range wantRoles {
		if history[i].Role() != wantRole {
			t.Errorf("history[%d].Role() = %q, want %q", i, history[i].Role(), wantRole)
		}
		if i > 0 && (history[i-1].GetID() >= history[i].GetID()) {
			t.Errorf("expected chronological UUIDv7 ordering history[%d].ID (%q) < history[%d].ID (%q)",
				i-1, history[i-1].GetID(), i, history[i].GetID())
		}
	}
	if !strings.Contains(msgText(history[3]), "Do not create file2.txt") {
		t.Errorf("history[3] rejected tool message = %q, want to contain rejection feedback", msgText(history[3]))
	}
}

func TestCancelPendingPermissionsOnNewUserSend(t *testing.T) {
	ctx := context.Background()

	var step int
	prov := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) ([]message.AssistantChunk, error) {
			step++
			if step == 1 {
				return []message.AssistantChunk{
					{
						BaseChunk: message.BaseChunk{ID: uuid.NewString()},
						Model:     req.Model,
						Content: []message.Block{
							&message.TextBlock{Text: "Preparing writes."},
							&message.ToolCall{
								ID:   "tc-cancel-1",
								Name: "write",
								Args: map[string]any{
									"path":    "cancel1.txt",
									"content": "nope 1\n",
								},
							},
							&message.ToolCall{
								ID:   "tc-cancel-2",
								Name: "write",
								Args: map[string]any{
									"path":    "cancel2.txt",
									"content": "nope 2\n",
								},
							},
						},
						Done: true,
					},
				}, nil
			}
			return []message.AssistantChunk{
				{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content: []message.Block{
						&message.TextBlock{Text: "Understood, canceled those writes and moving on."},
					},
					Done: true,
				},
			}, nil
		},
	}

	_, ws, _, _, mgr := setupTestEnv(t, prov)

	sess, _, err := mgr.Create(ctx, CreateParams{Title: "Cancel Pending Permissions"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// 1. First Send pauses awaiting 2 permissions
	err = sess.Send(ctx, SendInput{Message: message.NewUserText("Write cancel1.txt and cancel2.txt")})
	if err != nil {
		t.Fatalf("Send 1 error: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait 1 error: %v", err)
	}
	if sess.Status != StatusWaitingPermission {
		t.Fatalf("expected StatusWaitingPermission, got %v", sess.Status)
	}

	liveSess, err := mgr.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	pending := liveSess.PendingPermissions
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending permissions, got %d", len(pending))
	}

	// Buffer a partial decision for permission #1
	if err := sess.Send(ctx, SendInput{
		Decision: &tools.PermissionDecision{
			RequestID: pending[0].ID,
			Kind:      tools.DecisionAllowOnce,
		},
	}); err != nil {
		t.Fatalf("RespondPermission partial error: %v", err)
	}

	// 2. Call Send with a new user message while permissions are pending!
	err = sess.Send(ctx, SendInput{Message: message.NewUserText("Actually, cancel that and do something else")})
	if err != nil {
		t.Fatalf("Send 2 error: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait 2 error: %v", err)
	}

	// Verify neither file was written
	for _, name := range []string{"cancel1.txt", "cancel2.txt"} {
		if _, err := os.Stat(filepath.Join(ws.RootDir(), name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected %s not to be written after cancellation, stat err = %v", name, err)
		}
	}

	// Verify History (ORDER BY id ASC) returns:
	// [User1, Assistant1(ToolCalls), Tool1(Canceled), Tool2(Canceled), User2, Assistant2]
	// in exact chronological order!
	history, err := sess.History(ctx)
	if err != nil {
		t.Fatalf("History error: %v", err)
	}
	if len(history) != 6 {
		t.Fatalf("expected 6 messages in History, got %d: %+v", len(history), history)
	}
	reply2, ok := history[len(history)-1].(*message.Assistant)
	if !ok || reply2 == nil || msgText(reply2) != "Understood, canceled those writes and moving on." {
		t.Fatalf("unexpected reply2: %+v", reply2)
	}

	wantRoles := []message.Role{
		message.RoleUser,
		message.RoleAssistant,
		message.RoleTool,
		message.RoleTool,
		message.RoleUser,
		message.RoleAssistant,
	}
	for i, wantRole := range wantRoles {
		if history[i].Role() != wantRole {
			t.Errorf("history[%d].Role() = %q, want %q", i, history[i].Role(), wantRole)
		}
		if i > 0 && (history[i-1].GetID() >= history[i].GetID()) {
			t.Errorf("expected chronological UUIDv7 ordering history[%d].ID (%q) < history[%d].ID (%q)",
				i-1, history[i-1].GetID(), i, history[i].GetID())
		}
	}

	if msgText(history[0]) != "Write cancel1.txt and cancel2.txt" {
		t.Errorf("history[0] = %q", msgText(history[0]))
	}
	if !strings.Contains(strings.ToLower(msgText(history[2])), "cancel") {
		t.Errorf("history[2] = %q, want canceled tool result", msgText(history[2]))
	}
	if !strings.Contains(strings.ToLower(msgText(history[3])), "cancel") {
		t.Errorf("history[3] = %q, want canceled tool result", msgText(history[3]))
	}
	if msgText(history[4]) != "Actually, cancel that and do something else" {
		t.Errorf("history[4] = %q", msgText(history[4]))
	}
	if msgText(history[5]) != "Understood, canceled those writes and moving on." {
		t.Errorf("history[5] = %q", msgText(history[5]))
	}
}

func TestPermissionModeGetAndSet(t *testing.T) {
	ctx := context.Background()
	_, _, _, _, mgr := setupTestEnv(t, nil)

	sess, _, err := mgr.Create(ctx, CreateParams{Title: "Permission Modes"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	if sess.Config.Mode != tools.ModeDefault {
		t.Errorf("initial PermissionMode = %q, want %q", sess.Config.Mode, tools.ModeDefault)
	}

	for _, nextMode := range []tools.PermissionMode{
		tools.ModeAcceptEdits,
		tools.ModePlan,
		tools.ModeAsk,
		tools.ModeAuto,
		tools.ModeDefault,
	} {
		updated, err := mgr.Update(ctx, sess.ID, UpdateParams{Config: SessionConfig{Mode: nextMode}})
		if err != nil {
			t.Fatalf("Update(%q) error: %v", nextMode, err)
		}
		if updated.Config.Mode != nextMode {
			t.Errorf("PermissionMode = %q, want %q", updated.Config.Mode, nextMode)
		}
		got, err := mgr.Get(ctx, sess.ID)
		if err != nil {
			t.Fatalf("Get after Update(%q) error: %v", nextMode, err)
		}
		if got.Config.Mode != nextMode {
			t.Errorf("Get PermissionMode = %q, want %q", got.Config.Mode, nextMode)
		}
	}

	if _, err := mgr.Update(ctx, sess.ID, UpdateParams{Config: SessionConfig{Mode: "invalid_mode"}}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("Update invalid mode error = %v, want %v", err, ErrInvalidConfig)
	}
}

func TestTimelineRecordEvents(t *testing.T) {
	ctx := context.Background()

	var turnCount int
	prov := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) ([]message.AssistantChunk, error) {
			turnCount++
			if turnCount == 1 {
				return []message.AssistantChunk{
					{
						BaseChunk: message.BaseChunk{ID: uuid.NewString()},
						Model:     req.Model,
						Content: []message.Block{
							&message.TextBlock{Text: "I will write the test file."},
							&message.ToolCall{
								ID:   "tc-timeline-write",
								Name: "write",
								Args: map[string]any{
									"path":    "timeline_test.txt",
									"content": "sample content\n",
								},
							},
						},
						Done: true,
					},
				}, nil
			}
			return []message.AssistantChunk{
				{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content: []message.Block{
						&message.TextBlock{Text: "Write completed."},
					},
					Done: true,
				},
			}, nil
		},
	}

	db, _, _, pub, mgr := setupTestEnv(t, prov)

	// 1. Create session and verify empty initial timeline
	sess, _, err := mgr.Create(ctx, CreateParams{
		Title: "Timeline Lifecycle",
		Config: SessionConfig{
			Agent: "code-reviewer",
			Model: "sonnet",
		},
	})
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	initialTimeline, err := sess.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("Timeline initial: %v", err)
	}
	if len(initialTimeline) != 0 {
		t.Fatalf("expected empty initial timeline, got %d entries", len(initialTimeline))
	}

	// 2. Append a user message and verify KindMessage entry
	userMsg := message.NewUserText("Please review this code")
	if err := sess.appendMessage(ctx, userMsg); err != nil {
		t.Fatalf("AppendMessage error: %v", err)
	}

	tl1, err := sess.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("Timeline after message: %v", err)
	}
	if len(tl1) != 1 {
		t.Fatalf("expected 1 timeline entry, got %d", len(tl1))
	}
	if tl1[0].Kind != timeline.KindMessage {
		t.Errorf("tl1[0].Kind = %q, want %q", tl1[0].Kind, timeline.KindMessage)
	}
	if tl1[0].Message == nil || msgText(tl1[0].Message) != "Please review this code" {
		t.Errorf("tl1[0].Message text = %v, want %q", tl1[0].Message, "Please review this code")
	}

	// 3. Update Model and verify KindModel entry
	oldModel := sess.Config.Model
	newModelRaw := "claude-3-5-sonnet"
	updatedSess, err := mgr.Update(ctx, sess.ID, UpdateParams{Config: SessionConfig{Model: newModelRaw}})
	if err != nil {
		t.Fatalf("Update model error: %v", err)
	}
	if updatedSess.Config.Model == oldModel {
		t.Fatalf("expected model to change from %q", oldModel)
	}

	tl2, err := sess.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("Timeline after model update: %v", err)
	}
	if len(tl2) != 2 {
		t.Fatalf("expected 2 timeline entries, got %d", len(tl2))
	}
	if tl2[1].Kind != timeline.KindModel {
		t.Errorf("tl2[1].Kind = %q, want %q", tl2[1].Kind, timeline.KindModel)
	}
	if tl2[1].Model == nil || tl2[1].Model.From != oldModel || tl2[1].Model.To != updatedSess.Config.Model {
		t.Errorf("tl2[1].Model = %+v, want From=%q To=%q", tl2[1].Model, oldModel, updatedSess.Config.Model)
	}

	// 4. Update Agent and verify KindAgent entry
	oldAgent := updatedSess.Config.Agent
	newAgent := "general-assistant"
	_, err = mgr.Update(ctx, sess.ID, UpdateParams{Config: SessionConfig{Agent: newAgent}})
	if err != nil {
		t.Fatalf("Update agent error: %v", err)
	}

	tl3, err := sess.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("Timeline after agent update: %v", err)
	}
	if len(tl3) != 3 {
		t.Fatalf("expected 3 timeline entries, got %d", len(tl3))
	}
	if tl3[2].Kind != timeline.KindAgent {
		t.Errorf("tl3[2].Kind = %q, want %q", tl3[2].Kind, timeline.KindAgent)
	}
	if tl3[2].Agent == nil || tl3[2].Agent.From != oldAgent || tl3[2].Agent.To != newAgent {
		t.Errorf("tl3[2].Agent = %+v, want From=%q To=%q", tl3[2].Agent, oldAgent, newAgent)
	}

	// 5. Change permission mode and verify KindMode entry
	if _, err := mgr.Update(ctx, sess.ID, UpdateParams{Config: SessionConfig{Mode: tools.ModePlan}}); err != nil {
		t.Fatalf("Update(ModePlan) error: %v", err)
	}

	tl4, err := sess.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("Timeline after mode change: %v", err)
	}
	if len(tl4) != 4 {
		t.Fatalf("expected 4 timeline entries, got %d", len(tl4))
	}
	if tl4[3].Kind != timeline.KindMode {
		t.Errorf("tl4[3].Kind = %q, want %q", tl4[3].Kind, timeline.KindMode)
	}
	if tl4[3].Mode == nil || tl4[3].Mode.From != tools.ModeDefault || tl4[3].Mode.To != tools.ModePlan {
		t.Errorf("tl4[3].Mode = %+v, want From=%q To=%q", tl4[3].Mode, tools.ModeDefault, tools.ModePlan)
	}

	// Setting the same mode again should NOT append an entry
	if _, err := mgr.Update(ctx, sess.ID, UpdateParams{Config: SessionConfig{Mode: tools.ModePlan}}); err != nil {
		t.Fatalf("Update duplicate error: %v", err)
	}
	tl4Dup, err := sess.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("Timeline after duplicate mode: %v", err)
	}
	if len(tl4Dup) != 4 {
		t.Fatalf("expected no new entry for duplicate mode, got %d", len(tl4Dup))
	}

	// Reset mode to default so the tool call requires approval
	if _, err := mgr.Update(ctx, sess.ID, UpdateParams{Config: SessionConfig{Mode: tools.ModeDefault}}); err != nil {
		t.Fatalf("Update(ModeDefault) error: %v", err)
	}
	tl5, err := sess.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("Timeline after mode reset: %v", err)
	}
	if len(tl5) != 5 {
		t.Fatalf("expected 5 timeline entries, got %d", len(tl5))
	}
	if tl5[4].Kind != timeline.KindMode || tl5[4].Mode.From != tools.ModePlan || tl5[4].Mode.To != tools.ModeDefault {
		t.Errorf("tl5[4].Mode = %+v, want From=%q To=%q", tl5[4].Mode, tools.ModePlan, tools.ModeDefault)
	}

	// 6. Send tool-calling turn to trigger permission interrupt
	if err := sess.Send(ctx, SendInput{Message: message.NewUserText("Write a file")}); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait error: %v", err)
	}
	if sess.Status != StatusWaitingPermission {
		t.Fatalf("expected StatusWaitingPermission, got %v", sess.Status)
	}

	liveSess, err := mgr.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	pending := liveSess.PendingPermissions
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending permission, got %d", len(pending))
	}

	// Record decision and verify KindPermission entry
	dec := tools.PermissionDecision{
		RequestID: pending[0].ID,
		Kind:      tools.DecisionAllowOnce,
	}
	ready, err := sess.recordDecision(ctx, dec)
	if err != nil {
		t.Fatalf("RecordDecision error: %v", err)
	}
	if !ready {
		t.Errorf("expected ready = true after resolving only pending permission")
	}

	tl6, err := sess.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("Timeline after permission decision: %v", err)
	}

	// Entries in tl6 should be:
	// [0] KindMessage (user append)
	// [1] KindModel
	// [2] KindAgent
	// [3] KindMode (default -> plan)
	// [4] KindMode (plan -> default)
	// [5] KindMessage (Send user message)
	// [6] KindMessage (assistant tool-call turn)
	// [7] KindPermission (recorded decision)
	if len(tl6) != 8 {
		t.Fatalf("expected 8 timeline entries, got %d: %+v", len(tl6), tl6)
	}

	permEntry := tl6[7]
	if permEntry.Kind != timeline.KindPermission {
		t.Fatalf("tl6[7].Kind = %q, want %q", permEntry.Kind, timeline.KindPermission)
	}
	if permEntry.Permission == nil {
		t.Fatalf("expected non-nil Permission payload in entry")
	}
	if permEntry.Permission.Request.ID != pending[0].ID {
		t.Errorf("Permission.Request.ID = %q, want %q", permEntry.Permission.Request.ID, pending[0].ID)
	}
	if permEntry.Permission.Decision.Kind != tools.DecisionAllowOnce {
		t.Errorf("Permission.Decision.Kind = %q, want %q", permEntry.Permission.Decision.Kind, tools.DecisionAllowOnce)
	}

	// 8. Verify strict UUIDv7 ascending chronological ordering
	for i := 0; i < len(tl6)-1; i++ {
		if tl6[i].ID >= tl6[i+1].ID {
			t.Errorf("expected strictly ascending entry IDs: tl6[%d].ID=%q >= tl6[%d].ID=%q",
				i, tl6[i].ID, i+1, tl6[i+1].ID)
		}
	}

	// 9. Verify History returns only KindMessage entries
	history, err := sess.History(ctx)
	if err != nil {
		t.Fatalf("History error: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("expected 3 messages in History (user append, send user, send assistant), got %d: %+v", len(history), history)
	}

	// Verify EntryAddedEvent published events for all appended entries
	var publishedKinds []timeline.Kind
	for _, ev := range pub.recorded() {
		if eae, ok := ev.(EntryAddedEvent); ok {
			publishedKinds = append(publishedKinds, eae.Entry.Kind)
		}
	}
	for _, expectedKind := range []timeline.Kind{timeline.KindMessage, timeline.KindModel, timeline.KindAgent, timeline.KindMode, timeline.KindPermission} {
		if !slices.Contains(publishedKinds, expectedKind) {
			t.Errorf("expected EventEntryAdded with kind %q in published events: %v", expectedKind, publishedKinds)
		}
	}

	// 10. Delete session and verify timeline entries are removed
	if err := mgr.Delete(ctx, sess.ID); err != nil {
		t.Fatalf("Delete session: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM timeline WHERE session_id = ?`, sess.ID).Scan(&count); err != nil {
		t.Fatalf("count timeline entries after delete: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 timeline entries for deleted session, got %d", count)
	}
}

func TestTargetedSessionFixes(t *testing.T) {
	ctx := context.Background()

	prov := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) ([]message.AssistantChunk, error) {
			return []message.AssistantChunk{
				{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content: []message.Block{
						&message.ToolCall{
							ID:   "call-write-1",
							Name: "write",
							Args: map[string]any{
								"path":    "demo.txt",
								"content": "hello",
							},
						},
					},
					Done: true,
				},
			}, nil
		},
	}

	db, ws, gw, pub, mgr := setupTestEnv(t, prov)

	// 1. Verify sessionDir uses xdg.WorkspaceDir and does NOT create a sessions/ dir in ws.RootDir()
	sess, _, err := mgr.Create(ctx, CreateParams{
		Title: "Test Fixes",
		Config: SessionConfig{
			Effort: "low",
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	wsDataDir, err := xdg.WorkspaceDir(ws.RootDir())
	if err != nil {
		t.Fatalf("xdg.WorkspaceDir: %v", err)
	}
	expectedRoot := filepath.Join(wsDataDir, "sessions", sess.ID)
	if sess.Root != expectedRoot {
		t.Errorf("sess.Root = %q, want %q", sess.Root, expectedRoot)
	}
	workspaceSessionsDir := filepath.Join(ws.RootDir(), "sessions")
	if _, err := os.Stat(workspaceSessionsDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected no sessions/ dir in workspace root, got stat err: %v", err)
	}

	// 2. Verify Update Effort appends and publishes timeline.NewEffortEntry
	pub.reset()
	updated, err := mgr.Update(ctx, sess.ID, UpdateParams{
		Config: SessionConfig{
			Effort: "high",
		},
	})
	if err != nil {
		t.Fatalf("Update effort: %v", err)
	}
	if updated.Config.Effort != "high" {
		t.Errorf("updated.Config.Effort = %q, want 'high'", updated.Config.Effort)
	}

	tl, err := sess.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("Timeline.List: %v", err)
	}
	var foundEffort bool
	for _, entry := range tl {
		if entry.Kind == timeline.KindEffort && entry.Effort != nil {
			if entry.Effort.From == "low" && entry.Effort.To == "high" {
				foundEffort = true
				break
			}
		}
	}
	if !foundEffort {
		t.Errorf("expected timeline EffortEntry with From='low' To='high'")
	}

	var foundEffortEvent bool
	for _, ev := range pub.recorded() {
		if ea, ok := ev.(EntryAddedEvent); ok && ea.Entry.Kind == timeline.KindEffort {
			if ea.Entry.Effort != nil && ea.Entry.Effort.From == "low" && ea.Entry.Effort.To == "high" {
				foundEffortEvent = true
				break
			}
		}
	}
	if !foundEffortEvent {
		t.Errorf("expected EntryAddedEvent with KindEffort published")
	}

	// 3. Trigger permission interrupt and verify loadOrCacheSession restores StatusWaitingPermission
	if err := sess.Send(ctx, SendInput{Message: message.NewUserText("write demo.txt")}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if sess.Status != StatusWaitingPermission {
		t.Fatalf("expected StatusWaitingPermission, got %v", sess.Status)
	}

	// Load session via a new manager (simulating fresh loadOrCacheSession)
	permMgr, err := permissions.New(permissions.Config{Workspace: ws})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}
	mgr2, err := New(Config{
		DB:          db,
		Workspace:   ws,
		Gateway:     gw,
		Publisher:   pub,
		Logger:      log.Default(),
		Permissions: permMgr,
	})
	if err != nil {
		t.Fatalf("New mgr2: %v", err)
	}

	sess2, err := mgr2.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("mgr2.Get: %v", err)
	}
	if sess2.Status != StatusWaitingPermission {
		t.Errorf("sess2.Status = %v, want StatusWaitingPermission", sess2.Status)
	}
	if !sess2.WaitingPermission {
		t.Errorf("sess2.WaitingPermission = false, want true")
	}
	if len(sess2.PendingPermissions) == 0 {
		t.Errorf("sess2.PendingPermissions is empty, want > 0")
	}

	// 4. Verify Sandbox initialized with ID
	if sess.Sandbox == nil {
		t.Fatalf("sess.Sandbox is nil")
	}
	if sess.Sandbox.ID() != sess.ID {
		t.Errorf("Sandbox.ID() = %q, want %q", sess.Sandbox.ID(), sess.ID)
	}
	if sess.Sandbox.Platform().WorkDir != ws.RootDir() {
		t.Errorf("Sandbox.WorkDir = %q, want %q", sess.Sandbox.Platform().WorkDir, ws.RootDir())
	}
}

func TestSessionPermissionsPersistence(t *testing.T) {
	ctx := context.Background()
	db, ws, gw, pub, mgr := setupTestEnv(t, &fakeProvider{})

	sess, _, err := mgr.Create(ctx, CreateParams{
		Title: "Perm Test Session",
	})
	if err != nil {
		t.Fatalf("mgr.Create: %v", err)
	}

	if len(sess.Permissions.Allow) != 0 || len(sess.Permissions.Deny) != 0 {
		t.Fatalf("expected empty permissions initially, got %+v", sess.Permissions)
	}

	if sess.PermManager == nil {
		t.Fatalf("expected non-nil PermManager")
	}

	if err := sess.PermManager.AddAllow("write(internal/**)"); err != nil {
		t.Fatalf("PermManager.AddAllow: %v", err)
	}

	sess.Lock()
	hasAllow := slices.Contains(sess.Permissions.Allow, "write(internal/**)")
	sess.Unlock()
	if !hasAllow {
		t.Fatalf("expected sess.Permissions to reflect added allow rule")
	}

	// Verify directly in SQLite store
	st, err := newStore(ctx, db)
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	dbSess, err := st.getSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("st.getSession: %v", err)
	}
	if !slices.Contains(dbSess.Permissions.Allow, "write(internal/**)") {
		t.Fatalf("expected persisted permissions in store to contain allow rule, got %+v", dbSess.Permissions)
	}

	// Load via a fresh manager instance
	permMgr, err := permissions.New(permissions.Config{Workspace: ws})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}
	mgr2, err := New(Config{
		DB:          db,
		Workspace:   ws,
		Gateway:     gw,
		Publisher:   pub,
		Logger:      log.Default(),
		Permissions: permMgr,
	})
	if err != nil {
		t.Fatalf("New mgr2: %v", err)
	}

	sess2, err := mgr2.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("mgr2.Get: %v", err)
	}
	if !slices.Contains(sess2.Permissions.Allow, "write(internal/**)") {
		t.Fatalf("expected sess2 to restore permissions, got %+v", sess2.Permissions)
	}

	// Update permissions directly via store helper
	newPerms := permissions.Permissions{
		Allow: []string{"write(internal/**)"},
		Deny:  []string{"bash(*)"},
	}
	if err := st.updateSessionPermissions(ctx, sess.ID, newPerms); err != nil {
		t.Fatalf("updateSessionPermissions: %v", err)
	}
	dbSess2, err := st.getSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("st.getSession after update: %v", err)
	}
	if !slices.Contains(dbSess2.Permissions.Deny, "bash(*)") {
		t.Fatalf("expected updated deny rule in store, got %+v", dbSess2.Permissions)
	}
}

func TestSessionAutoGenerateTitle(t *testing.T) {
	ctx := t.Context()

	var mu sync.Mutex
	prov := &fakeProvider{
		chunkHandler: func(ctx context.Context, req *llm.Request) ([]message.AssistantChunk, error) {
			mu.Lock()
			defer mu.Unlock()
			isTitleGen := false
			for _, m := range req.Messages {
				if m.Role() == message.RoleSystem {
					for _, b := range m.GetContent() {
						if tb, ok := b.(*message.TextBlock); ok && strings.Contains(tb.Text, "short title") {
							isTitleGen = true
						}
					}
				}
			}
			if isTitleGen {
				return []message.AssistantChunk{
					{
						BaseChunk: message.BaseChunk{ID: uuid.NewString()},
						Content: []message.Block{
							&message.TextBlock{Text: "\"Database Refactoring\"\nExtra explanation to ignore"},
						},
					},
				}, nil
			}
			return []message.AssistantChunk{
				{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Content: []message.Block{
						&message.TextBlock{Text: "I will help refactor the database."},
					},
				},
			}, nil
		},
	}
	db, _, _, pub, mgr := setupTestEnv(t, prov)

	// 1. Create a session with Title: ""
	sess, _, err := mgr.Create(ctx, CreateParams{})
	if err != nil {
		t.Fatalf("mgr.Create: %v", err)
	}
	if sess.Title != "" {
		t.Fatalf("expected initial title to be empty, got %q", sess.Title)
	}

	// 2. Send first user message
	err = sess.Send(ctx, SendInput{
		Message: message.NewUserText("Help me refactor the database layer"),
	})
	if err != nil {
		t.Fatalf("sess.Send: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("sess.Wait: %v", err)
	}

	// Wait for background title generator to complete
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sess.Lock()
		curTitle := sess.Title
		sess.Unlock()
		if curTitle != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 3. Verify sess.Title updated in memory and sanitized
	sess.Lock()
	titleInMemory := sess.Title
	sess.Unlock()
	if titleInMemory != "Database Refactoring" {
		t.Fatalf("expected in-memory title %q, got %q", "Database Refactoring", titleInMemory)
	}

	// Verify title in SQLite DB
	var dbTitle string
	err = db.QueryRowContext(ctx, "SELECT title FROM sessions WHERE id = ?", sess.ID).Scan(&dbTitle)
	if err != nil {
		t.Fatalf("failed to query session title from DB: %v", err)
	}
	if dbTitle != "Database Refactoring" {
		t.Fatalf("expected DB title %q, got %q", "Database Refactoring", dbTitle)
	}

	// Verify UpdatedEvent was published on api.TopicSessions
	var foundUpdatedEvent bool
	for _, r := range pub.recordedRecords() {
		if r.topic == api.TopicSessions {
			if uev, ok := r.event.(UpdatedEvent); ok && uev.Session.ID == sess.ID && uev.Session.Title == "Database Refactoring" {
				foundUpdatedEvent = true
				break
			}
		}
	}
	if !foundUpdatedEvent {
		t.Fatal("expected UpdatedEvent with generated title on TopicSessions")
	}

	// 4. Verify subsequent Send does not overwrite title
	pub.reset()
	err = sess.Send(ctx, SendInput{
		Message: message.NewUserText("Another follow-up prompt"),
	})
	if err != nil {
		t.Fatalf("sess.Send follow-up: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("sess.Wait follow-up: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	sess.Lock()
	titleAfterFollowUp := sess.Title
	sess.Unlock()
	if titleAfterFollowUp != "Database Refactoring" {
		t.Fatalf("expected title to remain %q, got %q", "Database Refactoring", titleAfterFollowUp)
	}

	// Verify manual update or explicit initial title is not overwritten
	sessManual, _, err := mgr.Create(ctx, CreateParams{Title: "Manual Title"})
	if err != nil {
		t.Fatalf("mgr.Create sessManual: %v", err)
	}
	err = sessManual.Send(ctx, SendInput{
		Message: message.NewUserText("First prompt in manual session"),
	})
	if err != nil {
		t.Fatalf("sessManual.Send: %v", err)
	}
	if err := sessManual.Wait(ctx); err != nil {
		t.Fatalf("sessManual.Wait: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	sessManual.Lock()
	manualTitle := sessManual.Title
	sessManual.Unlock()
	if manualTitle != "Manual Title" {
		t.Fatalf("expected manual title to remain %q, got %q", "Manual Title", manualTitle)
	}
}

func TestSessionDirectShellEscape(t *testing.T) {
	ctx := context.Background()
	provider := &fakeProvider{}
	db, ws, gw, pub, mgr := setupTestEnv(t, provider)
	_ = db
	_ = ws
	_ = gw
	_ = pub

	sess, _, err := mgr.Create(ctx, CreateParams{
		Title: "Direct Shell Escape Test",
	})
	if err != nil {
		t.Fatalf("mgr.Create: %v", err)
	}

	// 1. Send direct shell command "echo hello"
	err = sess.Send(ctx, SendInput{
		Command: "echo hello",
	})
	if err != nil {
		t.Fatalf("sess.Send command: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("sess.Wait: %v", err)
	}

	// 2. Verify NO LLM turn was triggered
	provider.mu.Lock()
	turnCount := len(provider.turns)
	provider.mu.Unlock()
	if turnCount != 0 {
		t.Fatalf("expected 0 LLM turns after direct command, got %d", turnCount)
	}

	// 3. Verify timeline contains KindCommand entry (not a KindMessage)
	entries, err := sess.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("sess.Timeline.List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 timeline entry, got %d", len(entries))
	}
	if entries[0].Kind != timeline.KindCommand {
		t.Fatalf("expected timeline entry kind %q, got %q", timeline.KindCommand, entries[0].Kind)
	}
	if entries[0].Command == nil {
		t.Fatal("expected non-nil entries[0].Command")
	}
	if entries[0].Command.Command != "echo hello" {
		t.Fatalf("expected Command 'echo hello', got %q", entries[0].Command.Command)
	}
	if entries[0].Command.ExitCode != 0 {
		t.Fatalf("expected ExitCode 0, got %d", entries[0].Command.ExitCode)
	}
	if strings.TrimSpace(entries[0].Command.Output) != "hello" {
		t.Fatalf("expected Output 'hello', got %q", entries[0].Command.Output)
	}

	// sess.History only returns KindMessage entries; verify it returns none
	msgs, err := sess.History(ctx)
	if err != nil {
		t.Fatalf("sess.History: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 timeline messages in History, got %d", len(msgs))
	}

	// 4. Verify command was checkpointed in harness state and queue is drained
	vis := sess.VisibleQueue()
	if len(vis) != 0 {
		t.Fatalf("expected visible queue to be empty, got %d", len(vis))
	}
	h, err := sess.harness(ctx)
	if err != nil {
		t.Fatalf("sess.harness: %v", err)
	}
	state, err := h.LoadState(ctx)
	if err != nil {
		t.Fatalf("h.LoadState: %v", err)
	}
	if len(state.Messages) != 1 {
		t.Fatalf("expected 1 message in harness state, got %d", len(state.Messages))
	}
	if harness.SystemKindOf(state.Messages[0]) != harness.SystemKindCommand {
		t.Fatalf("expected message to be marked SystemKindCommand, got %q", harness.SystemKindOf(state.Messages[0]))
	}
	inboxText := state.Messages[0].GetContent().Text()
	if !strings.Contains(inboxText, `<bash_command command="echo hello" exit_code="0">`) {
		t.Fatalf("expected text to contain bash_command tag, got %s", inboxText)
	}

	// 5. Send a normal user prompt and verify harness receives both previous command and new user prompt
	err = sess.Send(ctx, SendInput{
		Message: message.NewUserText("What did I just run?"),
	})
	if err != nil {
		t.Fatalf("sess.Send user prompt: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("sess.Wait user prompt: %v", err)
	}

	provider.mu.Lock()
	turns := provider.turns
	provider.mu.Unlock()
	if len(turns) != 1 {
		t.Fatalf("expected 1 LLM turn after user prompt, got %d", len(turns))
	}

	// The harness should have received messages including the bash command output and the new user prompt
	reqMsgs := turns[0].messages
	var foundBashMsg, foundPromptMsg bool
	for _, m := range reqMsgs {
		txt := m.GetContent().Text()
		if strings.Contains(txt, `<bash_command command="echo hello"`) {
			foundBashMsg = true
		}
		if strings.Contains(txt, "What did I just run?") {
			foundPromptMsg = true
		}
	}
	if !foundBashMsg {
		t.Fatalf("expected harness request to contain bash_command message, got: %+v", reqMsgs)
	}
	if !foundPromptMsg {
		t.Fatalf("expected harness request to contain prompt message, got: %+v", reqMsgs)
	}

	// 6. Verify queue is now empty
	if len(sess.VisibleQueue()) != 0 {
		t.Fatalf("expected queue to be empty, got %d remaining", len(sess.VisibleQueue()))
	}

	// 7. Verify timeline messages do NOT have duplicate <bash_command> message
	timelineMsgs, err := sess.History(ctx)
	if err != nil {
		t.Fatalf("sess.History after turn: %v", err)
	}
	for _, m := range timelineMsgs {
		if strings.Contains(m.GetContent().Text(), "<bash_command") {
			t.Fatalf("unexpected <bash_command> in History: %s", m.GetContent().Text())
		}
	}
}

func TestSessionEnqueueCommandWhileRunning(t *testing.T) {
	ctx := context.Background()
	provider := &fakeProvider{}
	db, ws, gw, pub, mgr := setupTestEnv(t, provider)
	_ = db
	_ = ws
	_ = gw
	_ = pub

	sess, _, err := mgr.Create(ctx, CreateParams{
		Title: "Enqueue Command Test",
	})
	if err != nil {
		t.Fatalf("mgr.Create: %v", err)
	}

	// 1. Send first command that sleeps briefly
	if err := sess.Send(ctx, SendInput{Command: "sleep 0.1 && echo first"}); err != nil {
		t.Fatalf("sess.Send first command: %v", err)
	}

	// Wait until command begins executing (and has been popped from the queue)
	for range 100 {
		sess.Lock()
		running := sess.Running
		sess.Unlock()
		if running && len(sess.VisibleQueue()) == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// 2. While running, enqueue second and third commands
	if err := sess.Send(ctx, SendInput{Command: "echo second"}); err != nil {
		t.Fatalf("sess.Send second command: %v", err)
	}
	if err := sess.Send(ctx, SendInput{Command: "echo third"}); err != nil {
		t.Fatalf("sess.Send third command: %v", err)
	}

	// Verify visible queue has 2 commands
	pending := sess.VisibleQueue()
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending commands in visible queue, got %d", len(pending))
	}

	// 3. Wait for all to complete
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("sess.Wait: %v", err)
	}

	// Verify session is now idle and no pending commands remain
	sess.Lock()
	running := sess.Running
	status := sess.Status
	sess.Unlock()
	pending = sess.VisibleQueue()
	if running || status != StatusIdle || len(pending) != 0 {
		t.Fatalf("expected idle not running with 0 pending commands, got running=%v status=%s pending=%d", running, status, len(pending))
	}

	// 4. Verify timeline has all 3 commands executed in FIFO order
	entries, err := sess.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("sess.Timeline.List: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 timeline entries, got %d", len(entries))
	}
	for i, expectedCmd := range []string{"sleep 0.1 && echo first", "echo second", "echo third"} {
		if entries[i].Kind != timeline.KindCommand {
			t.Fatalf("entry %d: expected KindCommand, got %q", i, entries[i].Kind)
		}
		if entries[i].Command == nil || entries[i].Command.Command != expectedCmd {
			t.Fatalf("entry %d: expected command %q, got %+v", i, expectedCmd, entries[i].Command)
		}
		if entries[i].Command.ExitCode != 0 {
			t.Fatalf("entry %d: expected exit code 0, got %d", i, entries[i].Command.ExitCode)
		}
	}

	// Verify all 3 commands were also checkpointed in harness state
	h, err := sess.harness(ctx)
	if err != nil {
		t.Fatalf("sess.harness: %v", err)
	}
	state, err := h.LoadState(ctx)
	if err != nil {
		t.Fatalf("h.LoadState: %v", err)
	}
	if len(state.Messages) != 3 {
		t.Fatalf("expected 3 harness messages, got %d", len(state.Messages))
	}
}

func TestSessionEnqueueCommandDuringTurn(t *testing.T) {
	ctx := context.Background()
	turnStarted := make(chan struct{})
	allowTurnToFinish := make(chan struct{})

	provider := &fakeProvider{
		handler: func(ctx context.Context, req *llm.Request) ([]string, string, error) {
			select {
			case <-turnStarted:
			default:
				close(turnStarted)
			}
			select {
			case <-allowTurnToFinish:
				return []string{"Turn response"}, "", nil
			case <-ctx.Done():
				return nil, "", ctx.Err()
			}
		},
	}
	db, ws, gw, pub, mgr := setupTestEnv(t, provider)
	_ = db
	_ = ws
	_ = gw
	_ = pub

	sess, _, err := mgr.Create(ctx, CreateParams{
		Title: "Enqueue Command During Turn",
	})
	if err != nil {
		t.Fatalf("mgr.Create: %v", err)
	}

	// 1. Start a turn
	if err := sess.Send(ctx, SendInput{Message: message.NewUserText("Hello")}); err != nil {
		t.Fatalf("sess.Send message: %v", err)
	}

	// Wait until turn starts
	<-turnStarted

	// 2. While turn is running, send a direct command
	if err := sess.Send(ctx, SendInput{Command: "echo while_turn"}); err != nil {
		t.Fatalf("sess.Send command during turn: %v", err)
	}

	pending := sess.VisibleQueue()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending command, got %d", len(pending))
	}

	// 3. Allow turn to finish
	close(allowTurnToFinish)
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("sess.Wait: %v", err)
	}

	// 4. Verify the pending command was drained and executed
	entries, err := sess.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("sess.Timeline.List: %v", err)
	}

	var foundCmd bool
	for _, e := range entries {
		if e.Kind == timeline.KindCommand && e.Command != nil && e.Command.Command == "echo while_turn" {
			foundCmd = true
			if e.Command.ExitCode != 0 {
				t.Fatalf("expected exit code 0, got %d", e.Command.ExitCode)
			}
			if strings.TrimSpace(e.Command.Output) != "while_turn" {
				t.Fatalf("expected output 'while_turn', got %q", e.Command.Output)
			}
		}
	}
	if !foundCmd {
		t.Fatalf("expected KindCommand 'echo while_turn' in timeline, got entries: %+v", entries)
	}
}

func TestQueueKindTaskFilteredAndNoEvent(t *testing.T) {
	ctx := context.Background()
	turnStarted := make(chan struct{})
	allowTurnToFinish := make(chan struct{})

	provider := &fakeProvider{
		handler: func(ctx context.Context, req *llm.Request) ([]string, string, error) {
			select {
			case <-turnStarted:
			default:
				close(turnStarted)
			}
			select {
			case <-allowTurnToFinish:
				return []string{"Turn done"}, "", nil
			case <-ctx.Done():
				return nil, "", ctx.Err()
			}
		},
	}
	_, _, _, pub, mgr := setupTestEnv(t, provider)

	sess, _, err := mgr.Create(ctx, CreateParams{
		Title: "Task Queue Filter Test",
	})
	if err != nil {
		t.Fatalf("mgr.Create: %v", err)
	}

	// 1. Start a turn
	if err := sess.Send(ctx, SendInput{Message: message.NewUserText("Hello")}); err != nil {
		t.Fatalf("sess.Send: %v", err)
	}
	<-turnStarted

	// 2. Enqueue a user message while running -> should emit QueueUpdatedEvent
	if err := sess.Send(ctx, SendInput{Message: message.NewUserText("Queued user message")}); err != nil {
		t.Fatalf("sess.Send queued: %v", err)
	}

	// Verify QueueUpdatedEvent was published for the user message
	var queueUpdatedCountBefore int
	for _, ev := range pub.recorded() {
		if qe, ok := ev.(QueueUpdatedEvent); ok {
			queueUpdatedCountBefore++
			if len(qe.Queue) != 1 || qe.Queue[0].Kind != QueueKindMessage {
				t.Fatalf("expected 1 item in queue event, got %+v", qe.Queue)
			}
		}
	}
	if queueUpdatedCountBefore == 0 {
		t.Fatal("expected QueueUpdatedEvent for queued user message")
	}

	// 3. Enqueue a task message -> should NOT emit QueueUpdatedEvent and should NOT be in VisibleQueue
	taskMsg := message.NewUserTextMeta("task complete background", map[string]any{harness.MetaSystemKind: harness.SystemKindTask})
	sess.Enqueue(taskMsg)

	// Verify task item is NOT in VisibleQueue
	vis := sess.VisibleQueue()
	if len(vis) != 1 {
		t.Fatalf("expected VisibleQueue to have only 1 item, got %d: %+v", len(vis), vis)
	}
	if vis[0].Kind != QueueKindMessage {
		t.Fatalf("expected VisibleQueue[0] to be QueueKindMessage, got %s", vis[0].Kind)
	}

	// Verify internal queue has 2 items: 1 message + 1 task
	sess.Lock()
	qLen := len(sess.queue)
	var hasTaskKind bool
	for _, it := range sess.queue {
		if it.Kind == QueueKindTask {
			hasTaskKind = true
		}
	}
	sess.Unlock()
	if qLen != 2 || !hasTaskKind {
		t.Fatalf("expected 2 items in internal queue with QueueKindTask, got qLen=%d hasTaskKind=%v", qLen, hasTaskKind)
	}

	// Verify NO additional QueueUpdatedEvent was published for the task enqueue
	var queueUpdatedCountAfter int
	for _, ev := range pub.recorded() {
		if _, ok := ev.(QueueUpdatedEvent); ok {
			queueUpdatedCountAfter++
		}
	}
	if queueUpdatedCountAfter != queueUpdatedCountBefore {
		t.Fatalf("unexpected QueueUpdatedEvent received for QueueKindTask (before=%d, after=%d)", queueUpdatedCountBefore, queueUpdatedCountAfter)
	}

	// 4. Finish the turn and let the queued items process
	close(allowTurnToFinish)
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("sess.Wait: %v", err)
	}
}

func TestSessionTodosRestoreOnLoad(t *testing.T) {
	ctx := context.Background()

	var turn int
	prov := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) ([]message.AssistantChunk, error) {
			turn++
			if turn == 1 {
				return []message.AssistantChunk{
					{
						BaseChunk: message.BaseChunk{ID: uuid.NewString()},
						Model:     req.Model,
						Content: []message.Block{
							&message.ToolCall{
								ID:   "call-todos-1",
								Name: "todos",
								Args: map[string]any{
									"todos": []any{
										map[string]any{
											"content":     "Task A",
											"active_text": "Working on Task A",
											"status":      "in_progress",
										},
										map[string]any{
											"content":     "Task B",
											"active_text": "Task B queued",
											"status":      "pending",
										},
									},
								},
							},
						},
						Done: true,
					},
				}, nil
			}
			return []message.AssistantChunk{
				{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content: []message.Block{
						&message.TextBlock{Text: "Checklist updated."},
					},
					Done: true,
				},
			}, nil
		},
	}

	db, ws, gw, pub, mgr := setupTestEnv(t, prov)

	sess, _, err := mgr.Create(ctx, CreateParams{
		Title: "Todos Test",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := sess.Send(ctx, SendInput{Message: message.NewUserText("Make checklist")}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	sess.Lock()
	if len(sess.Todos) != 2 {
		t.Fatalf("expected 2 todos in sess.Todos, got %d", len(sess.Todos))
	}
	if sess.Todos[0].Content != "Task A" || sess.Todos[0].Status != tools.TodoStatusInProgress {
		t.Fatalf("unexpected sess.Todos[0]: %+v", sess.Todos[0])
	}
	sess.Unlock()

	// Verify TodosUpdatedEvent was published
	var foundTodosEvent bool
	for _, ev := range pub.recorded() {
		if te, ok := ev.(TodosUpdatedEvent); ok {
			foundTodosEvent = true
			if len(te.Todos) != 2 {
				t.Fatalf("expected 2 todos in published event, got %d", len(te.Todos))
			}
		}
	}
	if !foundTodosEvent {
		t.Fatal("expected TodosUpdatedEvent to be published")
	}

	// Create a new manager to test fresh loadOrCacheSession
	permMgr, err := permissions.New(permissions.Config{Workspace: ws})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}
	mgr2, err := New(Config{
		DB:          db,
		Workspace:   ws,
		Gateway:     gw,
		Publisher:   pub,
		Logger:      log.Default(),
		Permissions: permMgr,
	})
	if err != nil {
		t.Fatalf("New mgr2: %v", err)
	}

	sess2, err := mgr2.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("mgr2.Get: %v", err)
	}
	sess2.Lock()
	defer sess2.Unlock()
	if len(sess2.Todos) != 2 {
		t.Fatalf("expected 2 todos restored in sess2.Todos, got %d", len(sess2.Todos))
	}
	if sess2.Todos[0].ActiveText != "Working on Task A" || sess2.Todos[1].Status != tools.TodoStatusPending {
		t.Fatalf("unexpected restored todos in sess2: %+v", sess2.Todos)
	}
}

func TestSessionSendCompactDirect(t *testing.T) {
	ctx := context.Background()
	provider := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"Compacted summary of work"}, "reply-1", nil
		},
	}
	db, ws, gw, pub, mgr := setupTestEnv(t, provider)
	_ = db
	_ = ws
	_ = gw
	_ = pub

	sess, _, err := mgr.Create(ctx, CreateParams{
		Title: "Direct Compaction Test",
	})
	if err != nil {
		t.Fatalf("mgr.Create: %v", err)
	}

	// Invalid input: Compact combined with Message
	err = sess.Send(ctx, SendInput{
		Message: message.NewUserText("hello"),
		Compact: &api.CompactRequest{Focus: "focus"},
	})
	if err == nil {
		t.Fatal("expected error combining compact with message, got nil")
	}

	// 1. Initial turn with user message
	err = sess.Send(ctx, SendInput{
		Message: message.NewUserText("First prompt"),
	})
	if err != nil {
		t.Fatalf("sess.Send message: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("sess.Wait: %v", err)
	}

	// 2. Direct compact while idle
	err = sess.Send(ctx, SendInput{
		Compact: &api.CompactRequest{Focus: "preserve test instructions"},
	})
	if err != nil {
		t.Fatalf("sess.Send compact: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("sess.Wait: %v", err)
	}

	// 3. Verify timeline contains compacted entry
	entries, err := sess.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("sess.Timeline.List: %v", err)
	}
	var hasCompacted bool
	for _, e := range entries {
		if e.Kind == timeline.KindCompacted {
			hasCompacted = true
			if e.Compacted == nil || e.Compacted.Focus != "preserve test instructions" {
				t.Errorf("unexpected compacted entry: %+v", e.Compacted)
			}
		}
	}
	if !hasCompacted {
		t.Errorf("expected KindCompacted entry in timeline")
	}

	// 4. Compaction rejected if session is currently running
	sess.Lock()
	sess.Running = true
	sess.Unlock()
	err = sess.Send(ctx, SendInput{
		Compact: &api.CompactRequest{Focus: "should fail"},
	})
	sess.Lock()
	sess.Running = false
	sess.Unlock()
	if !errors.Is(err, ErrSessionRunning) {
		t.Fatalf("expected ErrSessionRunning when session is running, got %v", err)
	}
}
