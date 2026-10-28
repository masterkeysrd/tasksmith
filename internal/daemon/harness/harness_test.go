package harness

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

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	_ "modernc.org/sqlite"
)

type capturedTurn struct {
	model    string
	messages []message.Message
}

type fakeProvider struct {
	mu          sync.Mutex
	turns       []capturedTurn
	handler     func(ctx context.Context, req *llm.Request) ([]string, string, error)
	stepHandler func(ctx context.Context, req *llm.Request, turn int) ([]message.AssistantChunk, error)
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
	turnIdx := len(p.turns)
	stepHandler := p.stepHandler
	handler := p.handler
	p.mu.Unlock()

	if stepHandler != nil {
		rawChunks, err := stepHandler(ctx, req, turnIdx)
		if err != nil {
			return nil, err
		}
		return func(yield func(message.AssistantChunk, error) bool) {
			for _, ch := range rawChunks {
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
		chunks = []string{"Hello", ", ", "world!"}
		respID = "resp-default"
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

func newTestModel(t *testing.T, p *fakeProvider) *llm.Model {
	t.Helper()
	if p == nil {
		p = &fakeProvider{}
	}
	m, err := llm.NewModel(p, "test-model", nil)
	if err != nil {
		t.Fatalf("llm.NewModel: %v", err)
	}
	return m
}

func openTestSQLite(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func newTestToolsRegistry(t *testing.T) (string, tools.Container, permissions.SessionManager) {
	t.Helper()
	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-harness-test")
	t.Cleanup(func() {
		xdg.ClearCache()
	})

	dir := t.TempDir()
	if realDir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realDir
	}

	ws, err := workspace.Open(context.Background(), dir, workspace.WithBuiltinFS(builtin.FS()))
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}

	sb, err := sandbox.New(sandbox.Config{
		Mode:    sandbox.ModeLocal,
		WorkDir: dir,
	})
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	t.Cleanup(func() {
		_ = sb.Close()
	})

	stdLib, err := std.New(std.Config{
		Sandbox: sb,
		Tracker: std.NewMemoryTracker(),
		Logger:  log.Default(),
	})
	if err != nil {
		t.Fatalf("std.New: %v", err)
	}

	guard := shellguard.New(shellguard.Config{
		WorkspaceDir: dir,
	})

	builtinTools, err := builtin.New(builtin.Config{
		Std:       stdLib,
		Sandbox:   sb,
		Guard:     guard,
		Workspace: ws,
	})
	if err != nil {
		t.Fatalf("builtin.New: %v", err)
	}

	container, err := tools.NewContainer(tools.Config{
		Tools:     builtinTools,
		Sandbox:   sb,
		Workspace: ws,
		Logger:    log.Default(),
	})
	if err != nil {
		t.Fatalf("tools.NewContainer: %v", err)
	}

	permMgr, err := permissions.New(permissions.Config{
		Workspace:  ws,
		GlobalPath: filepath.Join(t.TempDir(), "permissions.json"),
	})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}
	sessPerm := permMgr.Attach(tools.ModeDefault, permissions.Permissions{}, nil)

	return dir, container, sessPerm
}

func newTestSpiller(t *testing.T) tools.Spiller {
	t.Helper()
	sp, err := tools.NewSpiller(tools.SpillerConfig{BaseDir: t.TempDir()})
	if err != nil {
		t.Fatalf("tools.NewSpiller: %v", err)
	}
	return sp
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

func drainStream(ctx context.Context, h Harness, input message.Message) ([]string, *message.Assistant, error) {
	var deltas []string
	var completed *message.Assistant
	for ev, err := range h.Stream(ctx, Input{Message: input}) {
		if err != nil {
			return deltas, completed, err
		}
		switch ev.Type {
		case EventChunk:
			if ev.Chunk != nil {
				if text := extractBlocksText(ev.Chunk.Content); text != "" {
					deltas = append(deltas, text)
				}
			}
		case EventCompleted:
			if asst, ok := ev.Message.(*message.Assistant); ok {
				completed = asst
			}
		}
	}
	return deltas, completed, nil
}

func TestSingleTurnStreaming(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	_, reg, perm := newTestToolsRegistry(t)
	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"Task", "Smith", " v2", " ready."}, "msg-assistant-1", nil
		},
	}

	h, err := New(Config{
		ID:          "thread-1",
		DB:          db,
		Model:       newTestModel(t, prov),
		Tools:       reg,
		Bash:        &fakeBashRunner{},
		Spiller:     newTestSpiller(t),
		Permissions: perm,
		Queue:       &fakeQueue{},
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	deltas, reply, err := drainStream(ctx, h, message.NewUserText("Hi there"))
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}

	wantDeltas := []string{"Task", "Smith", " v2", " ready."}
	if !reflect.DeepEqual(deltas, wantDeltas) {
		t.Errorf("deltas = %v, want %v", deltas, wantDeltas)
	}

	if reply == nil {
		t.Fatalf("expected non-nil completed Assistant message")
	}

	wantContent := "TaskSmith v2 ready."
	if got := msgText(reply); got != wantContent {
		t.Errorf("reply text = %q, want %q", got, wantContent)
	}
	if reply.Role() != message.RoleAssistant {
		t.Errorf("reply.Role() = %q, want %q", reply.Role(), message.RoleAssistant)
	}
	parsedID, err := uuid.Parse(reply.GetID())
	if err != nil || parsedID.Version() != 7 {
		t.Errorf("reply.GetID() = %q, want valid UUIDv7 (err=%v)", reply.GetID(), err)
	}
}

func TestMultiTurnSQLitePersistence(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	_, reg, perm := newTestToolsRegistry(t)

	turnCount := 0
	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			turnCount++
			if turnCount == 1 {
				return []string{"Nice to meet you, ", "Alice!"}, "resp-turn-1", nil
			}
			return []string{"Your name is ", "Alice."}, "resp-turn-2", nil
		},
	}
	model := newTestModel(t, prov)

	h, err := New(Config{
		ID:          "thread-alice",
		DB:          db,
		Model:       model,
		Tools:       reg,
		Bash:        &fakeBashRunner{},
		Spiller:     newTestSpiller(t),
		Permissions: perm,
		Queue:       &fakeQueue{},
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	// Verify empty state before any turn on thread
	initialState, err := h.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState initial error: %v", err)
	}
	if len(initialState.Messages) != 0 {
		t.Fatalf("expected 0 messages initially, got %d", len(initialState.Messages))
	}

	// Turn 1
	_, reply1, err := drainStream(ctx, h, message.NewUserText("My name is Alice"))
	if err != nil {
		t.Fatalf("Turn 1 Stream error: %v", err)
	}
	if got := msgText(reply1); got != "Nice to meet you, Alice!" {
		t.Errorf("Turn 1 reply = %q, want %q", got, "Nice to meet you, Alice!")
	}

	// Turn 2 on the same threadID across a freshly constructed Harness sharing the same SQLite DB
	h2, err := New(Config{
		ID:          "thread-alice",
		DB:          db,
		Model:       model,
		Tools:       reg,
		Bash:        &fakeBashRunner{},
		Spiller:     newTestSpiller(t),
		Permissions: perm,
		Queue:       &fakeQueue{},
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New h2 error: %v", err)
	}

	_, reply2, err := drainStream(ctx, h2, message.NewUserText("What is my name?"))
	if err != nil {
		t.Fatalf("Turn 2 Stream error: %v", err)
	}
	if got := msgText(reply2); got != "Your name is Alice." {
		t.Errorf("Turn 2 reply = %q, want %q", got, "Your name is Alice.")
	}

	// Inspect what the LLM received on Turn 2
	turns := prov.recordedTurns()
	if len(turns) != 2 {
		t.Fatalf("expected 2 LLM invocations, got %d", len(turns))
	}

	turn2Msgs := turns[1].messages
	if len(turn2Msgs) != 3 {
		t.Fatalf("expected 3 messages sent to LLM on Turn 2, got %d", len(turn2Msgs))
	}
	if turn2Msgs[0].Role() != message.RoleUser || msgText(turn2Msgs[0]) != "My name is Alice" {
		t.Errorf("turn2Msgs[0] = (%s, %q), want (user, \"My name is Alice\")", turn2Msgs[0].Role(), msgText(turn2Msgs[0]))
	}
	if turn2Msgs[1].Role() != message.RoleAssistant || msgText(turn2Msgs[1]) != "Nice to meet you, Alice!" {
		t.Errorf("turn2Msgs[1] = (%s, %q), want (assistant, \"Nice to meet you, Alice!\")", turn2Msgs[1].Role(), msgText(turn2Msgs[1]))
	}
	if turn2Msgs[2].Role() != message.RoleUser || msgText(turn2Msgs[2]) != "What is my name?" {
		t.Errorf("turn2Msgs[2] = (%s, %q), want (user, \"What is my name?\")", turn2Msgs[2].Role(), msgText(turn2Msgs[2]))
	}

	// Verify persisted state has all 4 conversation messages
	state, err := h2.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}
	if len(state.Messages) != 4 {
		t.Fatalf("expected 4 persisted messages, got %d", len(state.Messages))
	}
	if state.Messages[0].Role() != message.RoleUser || msgText(state.Messages[0]) != "My name is Alice" {
		t.Errorf("state.Messages[0] = (%s, %q)", state.Messages[0].Role(), msgText(state.Messages[0]))
	}
	if state.Messages[1].Role() != message.RoleAssistant || msgText(state.Messages[1]) != "Nice to meet you, Alice!" {
		t.Errorf("state.Messages[1] = (%s, %q)", state.Messages[1].Role(), msgText(state.Messages[1]))
	}
	if state.Messages[2].Role() != message.RoleUser || msgText(state.Messages[2]) != "What is my name?" {
		t.Errorf("state.Messages[2] = (%s, %q)", state.Messages[2].Role(), msgText(state.Messages[2]))
	}
	if state.Messages[3].Role() != message.RoleAssistant || msgText(state.Messages[3]) != "Your name is Alice." {
		t.Errorf("state.Messages[3] = (%s, %q)", state.Messages[3].Role(), msgText(state.Messages[3]))
	}
	for i, m := range state.Messages {
		u, err := uuid.Parse(m.GetID())
		if err != nil || u.Version() != 7 {
			t.Errorf("state.Messages[%d].GetID() = %q, want valid UUIDv7 (err=%v)", i, m.GetID(), err)
		}
	}
}

func TestSystemPromptIsolation(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	_, reg, perm := newTestToolsRegistry(t)

	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"Understood."}, "resp-sys-1", nil
		},
	}

	sysPrompt := "You are TaskSmith v2.\n\nFollow strict Go conventions."
	h, err := New(Config{
		ID:           "thread-sys",
		DB:           db,
		Model:        newTestModel(t, prov),
		Tools:        reg,
		Bash:         &fakeBashRunner{},
		Spiller:      newTestSpiller(t),
		Permissions:  perm,
		Queue:        &fakeQueue{},
		SystemPrompt: sysPrompt,
		Logger:       log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	_, _, err = drainStream(ctx, h, message.NewUserText("Ping"))
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}

	// 1. Verify LLM received the system prompt as the leading message.System
	turns := prov.recordedTurns()
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}

	llmMsgs := turns[0].messages
	if len(llmMsgs) != 2 {
		t.Fatalf("expected 2 LLM messages (system + user), got %d", len(llmMsgs))
	}
	if llmMsgs[0].Role() != message.RoleSystem {
		t.Errorf("llmMsgs[0].Role() = %q, want %q", llmMsgs[0].Role(), message.RoleSystem)
	}
	if got := msgText(llmMsgs[0]); got != sysPrompt {
		t.Errorf("system prompt = %q, want %q", got, sysPrompt)
	}
	if llmMsgs[1].Role() != message.RoleUser || msgText(llmMsgs[1]) != "Ping" {
		t.Errorf("llmMsgs[1] = (%s, %q), want (user, \"Ping\")", llmMsgs[1].Role(), msgText(llmMsgs[1]))
	}

	// 2. Verify LoadState NEVER contains the system prompt
	state, err := h.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}
	if len(state.Messages) != 2 {
		t.Fatalf("expected 2 messages in checkpoint state (user + assistant), got %d", len(state.Messages))
	}
	for i, m := range state.Messages {
		if m.Role() == message.RoleSystem {
			t.Errorf("checkpoint state.Messages[%d] unexpectedly has RoleSystem: %+v", i, m)
		}
		if strings.Contains(msgText(m), "You are TaskSmith v2") {
			t.Errorf("checkpoint state.Messages[%d] leaked system prompt content: %q", i, msgText(m))
		}
	}
}

func TestThreadIsolation(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	_, reg, perm := newTestToolsRegistry(t)

	prov := &fakeProvider{
		handler: func(_ context.Context, req *llm.Request) ([]string, string, error) {
			lastMsg := req.Messages[len(req.Messages)-1]
			return []string{"Echo: " + msgText(lastMsg)}, "", nil
		},
	}

	hA, err := New(Config{
		ID:          "thread-A",
		DB:          db,
		Model:       newTestModel(t, prov),
		Tools:       reg,
		Bash:        &fakeBashRunner{},
		Spiller:     newTestSpiller(t),
		Permissions: perm,
		Queue:       &fakeQueue{},
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New hA error: %v", err)
	}

	hB, err := New(Config{
		ID:          "thread-B",
		DB:          db,
		Model:       newTestModel(t, prov),
		Tools:       reg,
		Bash:        &fakeBashRunner{},
		Spiller:     newTestSpiller(t),
		Permissions: perm,
		Queue:       &fakeQueue{},
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New hB error: %v", err)
	}

	if _, _, err := drainStream(ctx, hA, message.NewUserText("Message for A")); err != nil {
		t.Fatalf("Stream thread-A error: %v", err)
	}
	if _, _, err := drainStream(ctx, hB, message.NewUserText("Message for B")); err != nil {
		t.Fatalf("Stream thread-B error: %v", err)
	}

	stateA, err := hA.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState thread-A error: %v", err)
	}
	stateB, err := hB.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState thread-B error: %v", err)
	}

	if len(stateA.Messages) != 2 || msgText(stateA.Messages[0]) != "Message for A" || msgText(stateA.Messages[1]) != "Echo: Message for A" {
		t.Errorf("unexpected stateA: %+v", stateA.Messages)
	}
	if len(stateB.Messages) != 2 || msgText(stateB.Messages[0]) != "Message for B" || msgText(stateB.Messages[1]) != "Echo: Message for B" {
		t.Errorf("unexpected stateB: %+v", stateB.Messages)
	}

	// Verify Turn 1 on thread-B did NOT receive thread-A's messages
	turns := prov.recordedTurns()
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}
	if len(turns[1].messages) != 1 || msgText(turns[1].messages[0]) != "Message for B" {
		t.Errorf("thread-B LLM call received unexpected messages: %v", turns[1].messages)
	}
}

func TestMultiStepToolExecution(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	workDir, reg, perm := newTestToolsRegistry(t)

	targetFile := filepath.Join(workDir, "hello.txt")
	if err := os.WriteFile(targetFile, []byte("secret-file-contents\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	prov := &fakeProvider{
		stepHandler: func(_ context.Context, _ *llm.Request, turn int) ([]message.AssistantChunk, error) {
			if turn == 1 {
				return []message.AssistantChunk{
					{
						BaseChunk: message.BaseChunk{ID: "asst-tool-call"},
						Content: []message.Block{
							&message.TextBlock{Text: "Let me read hello.txt."},
							&message.ToolCall{
								ID:   "call-view-1",
								Name: "view",
								Args: map[string]any{"path": "hello.txt"},
							},
						},
						Done: true,
					},
				}, nil
			}
			return []message.AssistantChunk{
				{
					BaseChunk: message.BaseChunk{ID: "asst-final"},
					Content: []message.Block{
						&message.TextBlock{Text: "The file contains secret-file-contents."},
					},
					Done: true,
				},
			}, nil
		},
	}

	h, err := New(Config{
		ID:          "thread-tools",
		DB:          db,
		Model:       newTestModel(t, prov),
		Tools:       reg,
		Bash:        &fakeBashRunner{},
		Spiller:     newTestSpiller(t),
		Permissions: perm,
		Queue:       &fakeQueue{},
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	var emittedMessages []message.Message
	var completedMsg message.Message

	for ev, err := range h.Stream(ctx, Input{Message: message.NewUserText("Read hello.txt")}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		switch ev.Type {
		case EventMessage:
			emittedMessages = append(emittedMessages, ev.Message)
		case EventCompleted:
			completedMsg = ev.Message
		case EventInterrupted:
			t.Fatalf("unexpected EventInterrupted for read-only view tool: %+v", ev.Permissions)
		}
	}

	// Expect 4 EventMessage notifications: User, Assistant(ToolCall), Tool(Result), Assistant(Final)
	if len(emittedMessages) != 4 {
		t.Fatalf("expected 4 EventMessage events, got %d", len(emittedMessages))
	}
	if emittedMessages[0].Role() != message.RoleUser {
		t.Errorf("emittedMessages[0].Role() = %q, want user", emittedMessages[0].Role())
	}
	if emittedMessages[1].Role() != message.RoleAssistant {
		t.Errorf("emittedMessages[1].Role() = %q, want assistant", emittedMessages[1].Role())
	}
	toolMsg, ok := emittedMessages[2].(*message.Tool)
	if !ok {
		t.Fatalf("emittedMessages[2] is %T, want *message.Tool", emittedMessages[2])
	}
	if toolMsg.IsError {
		t.Errorf("unexpected tool error: %s", msgText(toolMsg))
	}
	viewOut, ok := toolMsg.StructuredContent.(api.ViewOutput)
	if !ok || !strings.Contains(viewOut.Content, "secret-file-contents") {
		t.Errorf("unexpected tool structured output: ok=%v, content=%+v", ok, toolMsg.StructuredContent)
	}
	if emittedMessages[3].Role() != message.RoleAssistant || msgText(emittedMessages[3]) != "The file contains secret-file-contents." {
		t.Errorf("unexpected final assistant message: %q", msgText(emittedMessages[3]))
	}
	if completedMsg == nil || msgText(completedMsg) != "The file contains secret-file-contents." {
		t.Errorf("unexpected EventCompleted message: %v", completedMsg)
	}

	// Verify all 4 messages are persisted in checkpoint state
	state, err := h.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}
	if len(state.Messages) != 4 {
		t.Fatalf("expected 4 messages in checkpoint state, got %d", len(state.Messages))
	}
}

func TestPermissionInterruptAndResume(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	workDir, reg, perm := newTestToolsRegistry(t)

	prov := &fakeProvider{
		stepHandler: func(_ context.Context, _ *llm.Request, turn int) ([]message.AssistantChunk, error) {
			if turn == 1 {
				return []message.AssistantChunk{
					{
						BaseChunk: message.BaseChunk{ID: "asst-write"},
						Content: []message.Block{
							&message.ToolCall{
								ID:   "call-ls-readonly",
								Name: "ls",
								Args: map[string]any{},
							},
							&message.ToolCall{
								ID:   "call-write-allow",
								Name: "write",
								Args: map[string]any{
									"path":    "allowed.txt",
									"content": "approved content",
								},
							},
							&message.ToolCall{
								ID:   "call-write-reject",
								Name: "write",
								Args: map[string]any{
									"path":    "rejected.txt",
									"content": "rejected content",
								},
							},
						},
						Done: true,
					},
				}, nil
			}
			return []message.AssistantChunk{
				{
					BaseChunk: message.BaseChunk{ID: "asst-after-resume"},
					Content: []message.Block{
						&message.TextBlock{Text: "Processed permission decisions."},
					},
					Done: true,
				},
			}, nil
		},
	}

	h, err := New(Config{
		ID:          "thread-perm",
		DB:          db,
		Model:       newTestModel(t, prov),
		Tools:       reg,
		Bash:        &fakeBashRunner{},
		Spiller:     newTestSpiller(t),
		Permissions: perm,
		Queue:       &fakeQueue{},
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	// 1. Calling Resume on a thread with no pending permissions -> ErrNoPendingPermissions
	for _, err := range h.Stream(ctx, Input{Decisions: map[string]tools.PermissionDecision{}}) {
		if !errors.Is(err, ErrNoPendingPermissions) {
			t.Fatalf("Resume without pending permissions error = %v, want %v", err, ErrNoPendingPermissions)
		}
	}

	// 2. Run Stream -> ls executes immediately, while the 2 write calls pause at ToolsNode with EventInterrupted
	var streamToolMsgs []*message.Tool
	var interruptedPerms []tools.PermissionRequest
	for ev, err := range h.Stream(ctx, Input{Message: message.NewUserText("Write two files")}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		if ev.Type == EventMessage {
			if tm, ok := ev.Message.(*message.Tool); ok {
				streamToolMsgs = append(streamToolMsgs, tm)
			}
		}
		if ev.Type == EventInterrupted {
			interruptedPerms = ev.Permissions
		}
		if ev.Type == EventCompleted {
			t.Fatalf("expected stream to interrupt for write permissions, but got EventCompleted")
		}
	}

	if len(streamToolMsgs) != 1 || streamToolMsgs[0].IsError {
		t.Fatalf("expected ls read-only tool to execute before interrupt, got %+v", streamToolMsgs)
	}
	if len(interruptedPerms) != 2 {
		t.Fatalf("expected 2 pending permission requests, got %d", len(interruptedPerms))
	}

	statePaused, err := h.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState paused error: %v", err)
	}
	if len(statePaused.PendingPermissions) != 2 {
		t.Fatalf("expected 2 PendingPermissions in checkpoint, got %d", len(statePaused.PendingPermissions))
	}

	// 3. Calling Resume with partial/missing decisions MUST fail with ErrPendingPermissionsUnresolved without calling the graph
	turnsBeforePartial := len(prov.recordedTurns())
	partialDecisions := map[string]tools.PermissionDecision{
		interruptedPerms[0].ID: {
			Kind: tools.DecisionAllowOnce,
		},
	}
	var partialErr error
	for _, err := range h.Stream(ctx, Input{Decisions: partialDecisions}) {
		if err != nil {
			partialErr = err
		}
	}
	if !errors.Is(partialErr, ErrPendingPermissionsUnresolved) {
		t.Fatalf("Resume with partial decisions error = %v, want %v", partialErr, ErrPendingPermissionsUnresolved)
	}
	if gotTurns := len(prov.recordedTurns()); gotTurns != turnsBeforePartial {
		t.Fatalf("expected 0 graph/LLM invocations on unresolved permissions, got %d -> %d", turnsBeforePartial, gotTurns)
	}

	// 4. Calling Resume with all decisions resolved (1 allow_once, 1 reject with feedback)
	fullDecisions := map[string]tools.PermissionDecision{
		interruptedPerms[0].ID: {
			Kind: tools.DecisionAllowOnce,
		},
		interruptedPerms[1].ToolCallID: {
			Kind:     tools.DecisionReject,
			Feedback: "do not touch rejected.txt",
		},
	}

	var resumeToolMsgs []*message.Tool
	var resumeCompleted message.Message
	for ev, err := range h.Stream(ctx, Input{Decisions: fullDecisions}) {
		if err != nil {
			t.Fatalf("Resume error: %v", err)
		}
		switch ev.Type {
		case EventMessage:
			if tm, ok := ev.Message.(*message.Tool); ok {
				resumeToolMsgs = append(resumeToolMsgs, tm)
			}
		case EventCompleted:
			resumeCompleted = ev.Message
		}
	}

	if len(resumeToolMsgs) != 2 {
		t.Fatalf("expected 2 Tool messages during Resume (2 writes), got %d", len(resumeToolMsgs))
	}
	if resumeToolMsgs[0].IsError {
		t.Errorf("expected allowed.txt write to succeed, got error: %s", msgText(resumeToolMsgs[0]))
	}
	if !resumeToolMsgs[1].IsError || !strings.Contains(msgText(resumeToolMsgs[1]), "do not touch rejected.txt") {
		t.Errorf("expected rejected.txt write to fail with user feedback, got: %s", msgText(resumeToolMsgs[1]))
	}
	if resumeCompleted == nil || msgText(resumeCompleted) != "Processed permission decisions." {
		t.Errorf("unexpected completed message after Resume: %v", resumeCompleted)
	}

	// Verify allowed.txt was written on disk and rejected.txt was not
	if data, err := os.ReadFile(filepath.Join(workDir, "allowed.txt")); err != nil || string(data) != "approved content" {
		t.Errorf("allowed.txt content = %q, err = %v", string(data), err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "rejected.txt")); !os.IsNotExist(err) {
		t.Errorf("expected rejected.txt not to exist, got err=%v", err)
	}

	// Verify PendingPermissions is cleared in final checkpoint state
	finalState, err := h.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState final error: %v", err)
	}
	if len(finalState.PendingPermissions) != 0 {
		t.Errorf("expected 0 PendingPermissions after Resume, got %d", len(finalState.PendingPermissions))
	}
}

func TestCancelPendingPermissionsOnNewUserMessage(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	workDir, reg, perm := newTestToolsRegistry(t)

	prov := &fakeProvider{
		stepHandler: func(_ context.Context, _ *llm.Request, turn int) ([]message.AssistantChunk, error) {
			if turn == 1 {
				return []message.AssistantChunk{
					{
						BaseChunk: message.BaseChunk{ID: "asst-write-pending"},
						Content: []message.Block{
							&message.ToolCall{
								ID:   "call-write-cancel",
								Name: "write",
								Args: map[string]any{
									"path":    "never-written.txt",
									"content": "should be canceled",
								},
							},
						},
						Done: true,
					},
				}, nil
			}
			return []message.AssistantChunk{
				{
					BaseChunk: message.BaseChunk{ID: "asst-new-turn"},
					Content: []message.Block{
						&message.TextBlock{Text: "Switched to your new request."},
					},
					Done: true,
				},
			}, nil
		},
	}

	h, err := New(Config{
		ID:          "thread-cancel-perm",
		DB:          db,
		Model:       newTestModel(t, prov),
		Tools:       reg,
		Bash:        &fakeBashRunner{},
		Spiller:     newTestSpiller(t),
		Permissions: perm,
		Queue:       &fakeQueue{},
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	// Turn 1: Trigger permission interrupt
	var interrupted bool
	for ev, err := range h.Stream(ctx, Input{Message: message.NewUserText("Write never-written.txt")}) {
		if err != nil {
			t.Fatalf("Turn 1 Stream error: %v", err)
		}
		if ev.Type == EventInterrupted {
			interrupted = true
		}
	}
	if !interrupted {
		t.Fatalf("expected Turn 1 to interrupt for permission")
	}

	// Turn 2: Send a brand new user message via Stream without resolving permissions
	var turn2Events []message.Message
	var canceledToolMsg *message.Tool
	var finalMsg message.Message
	for ev, err := range h.Stream(ctx, Input{Message: message.NewUserText("Actually, cancel that and do something else")}) {
		if err != nil {
			t.Fatalf("Turn 2 Stream error: %v", err)
		}
		switch ev.Type {
		case EventMessage:
			turn2Events = append(turn2Events, ev.Message)
			if tm, ok := ev.Message.(*message.Tool); ok {
				canceledToolMsg = tm
			}
		case EventCompleted:
			finalMsg = ev.Message
		}
	}

	if len(turn2Events) != 3 || turn2Events[0].Role() != message.RoleTool ||
		turn2Events[1].Role() != message.RoleUser || turn2Events[2].Role() != message.RoleAssistant {
		t.Fatalf("expected Turn 2 EventMessage order [Tool(Canceled), User, Assistant], got %d events", len(turn2Events))
	}

	if canceledToolMsg == nil || !canceledToolMsg.IsError || !strings.Contains(msgText(canceledToolMsg), "canceled") {
		t.Fatalf("expected canceled *message.Tool event, got %+v", canceledToolMsg)
	}
	if finalMsg == nil || msgText(finalMsg) != "Switched to your new request." {
		t.Fatalf("unexpected final message: %v", finalMsg)
	}
	if _, err := os.Stat(filepath.Join(workDir, "never-written.txt")); !os.IsNotExist(err) {
		t.Errorf("expected never-written.txt not to be created, got err=%v", err)
	}

	// Verify LLM on Turn 2 received messages in strict order:
	// [User1, Assistant1(ToolCall), Tool(Canceled), User2]
	turns := prov.recordedTurns()
	if len(turns) != 2 {
		t.Fatalf("expected 2 LLM turns, got %d", len(turns))
	}
	turn2Msgs := turns[1].messages
	if len(turn2Msgs) != 4 {
		t.Fatalf("expected 4 messages in Turn 2 LLM request, got %d", len(turn2Msgs))
	}
	if turn2Msgs[0].Role() != message.RoleUser || turn2Msgs[1].Role() != message.RoleAssistant ||
		turn2Msgs[2].Role() != message.RoleTool || turn2Msgs[3].Role() != message.RoleUser {
		t.Errorf("unexpected Turn 2 message role sequence: [%s, %s, %s, %s]",
			turn2Msgs[0].Role(), turn2Msgs[1].Role(), turn2Msgs[2].Role(), turn2Msgs[3].Role())
	}

	state, err := h.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}
	if len(state.PendingPermissions) != 0 {
		t.Errorf("expected PendingPermissions to be cleared, got %d", len(state.PendingPermissions))
	}
	if len(state.Messages) != 5 {
		t.Fatalf("expected 5 persisted messages in state, got %d", len(state.Messages))
	}
	for i := 1; i < len(state.Messages); i++ {
		if state.Messages[i-1].GetID() >= state.Messages[i].GetID() {
			t.Errorf("expected strictly increasing UUIDv7 IDs: state.Messages[%d].ID (%s) >= state.Messages[%d].ID (%s)",
				i-1, state.Messages[i-1].GetID(), i, state.Messages[i].GetID())
		}
	}
}

func TestMixedToolCallsAndPlanModeDeny(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	workDir, reg, perm := newTestToolsRegistry(t)

	if err := os.WriteFile(filepath.Join(workDir, "readme.txt"), []byte("hello plan\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Put permissions manager in ModePlan: view is allowed, write is denied (AccessDeny without interrupt) and unknown tool fails Prepare
	perm.SetMode(tools.ModePlan)

	prov := &fakeProvider{
		stepHandler: func(_ context.Context, _ *llm.Request, turn int) ([]message.AssistantChunk, error) {
			if turn == 1 {
				return []message.AssistantChunk{
					{
						BaseChunk: message.BaseChunk{ID: "asst-plan-calls"},
						Content: []message.Block{
							&message.ToolCall{
								ID:   "call-view",
								Name: "view",
								Args: map[string]any{"path": "readme.txt"},
							},
							&message.ToolCall{
								ID:   "call-write-denied",
								Name: "write",
								Args: map[string]any{"path": "denied.txt", "content": "nope"},
							},
							&message.ToolCall{
								ID:   "call-nonexistent",
								Name: "nonexistent_tool",
								Args: map[string]any{},
							},
						},
						Done: true,
					},
				}, nil
			}
			return []message.AssistantChunk{
				{
					BaseChunk: message.BaseChunk{ID: "asst-plan-done"},
					Content: []message.Block{
						&message.TextBlock{Text: "Plan mode evaluated."},
					},
					Done: true,
				},
			}, nil
		},
	}

	h, err := New(Config{
		ID:          "thread-plan",
		DB:          db,
		Model:       newTestModel(t, prov),
		Tools:       reg,
		Bash:        &fakeBashRunner{},
		Spiller:     newTestSpiller(t),
		Permissions: perm,
		Queue:       &fakeQueue{},
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	var toolMsgs []*message.Tool
	for ev, err := range h.Stream(ctx, Input{Message: message.NewUserText("Try view, write, and unknown")}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		if ev.Type == EventMessage {
			if tm, ok := ev.Message.(*message.Tool); ok {
				toolMsgs = append(toolMsgs, tm)
			}
		}
	}

	if len(toolMsgs) != 3 {
		t.Fatalf("expected 3 tool messages, got %d", len(toolMsgs))
	}
	if toolMsgs[0].IsError {
		t.Errorf("expected view to succeed in ModePlan, got error: %s", msgText(toolMsgs[0]))
	}
	viewOut, ok := toolMsgs[0].StructuredContent.(api.ViewOutput)
	if !ok || !strings.Contains(viewOut.Content, "hello plan") {
		t.Errorf("expected view to succeed in ModePlan with content 'hello plan': ok=%v, content=%+v", ok, toolMsgs[0].StructuredContent)
	}
	if !toolMsgs[1].IsError || !strings.Contains(msgText(toolMsgs[1]), tools.ErrPermissionDenied.Error()) {
		t.Errorf("expected write to be denied in ModePlan, got: %s", msgText(toolMsgs[1]))
	}
	if !toolMsgs[2].IsError || !strings.Contains(msgText(toolMsgs[2]), tools.ErrToolNotFound.Error()) {
		t.Errorf("expected nonexistent_tool to fail Prepare, got: %s", msgText(toolMsgs[2]))
	}
}

func TestValidationAndContextCancellation(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	_, reg, perm := newTestToolsRegistry(t)
	model := newTestModel(t, nil)
	logger := log.Default()

	bash := &fakeBashRunner{}
	spiller := newTestSpiller(t)
	q := &fakeQueue{}

	// 1. Validate all required Config fields
	if _, err := New(Config{DB: db, Model: model, Tools: reg, Bash: bash, Spiller: spiller, Permissions: perm, Queue: q, Logger: logger}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing ID error = %v, want %v", err, ErrInvalidConfig)
	}
	if _, err := New(Config{ID: "   ", DB: db, Model: model, Tools: reg, Bash: bash, Spiller: spiller, Permissions: perm, Queue: q, Logger: logger}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("whitespace ID error = %v, want %v", err, ErrInvalidConfig)
	}
	if _, err := New(Config{ID: "thread-valid", Model: model, Tools: reg, Bash: bash, Spiller: spiller, Permissions: perm, Queue: q, Logger: logger}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing DB error = %v, want %v", err, ErrInvalidConfig)
	}
	if _, err := New(Config{ID: "thread-valid", DB: db, Tools: reg, Bash: bash, Spiller: spiller, Permissions: perm, Queue: q, Logger: logger}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing Model error = %v, want %v", err, ErrInvalidConfig)
	}
	if _, err := New(Config{ID: "thread-valid", DB: db, Model: model, Bash: bash, Spiller: spiller, Permissions: perm, Queue: q, Logger: logger}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing Tools error = %v, want %v", err, ErrInvalidConfig)
	}
	if _, err := New(Config{ID: "thread-valid", DB: db, Model: model, Tools: reg, Spiller: spiller, Permissions: perm, Queue: q, Logger: logger}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing Bash error = %v, want %v", err, ErrInvalidConfig)
	}
	if _, err := New(Config{ID: "thread-valid", DB: db, Model: model, Tools: reg, Bash: bash, Permissions: perm, Queue: q, Logger: logger}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing Spiller error = %v, want %v", err, ErrInvalidConfig)
	}
	if _, err := New(Config{ID: "thread-valid", DB: db, Model: model, Tools: reg, Bash: bash, Spiller: spiller, Logger: logger}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing Permissions error = %v, want %v", err, ErrInvalidConfig)
	}
	if _, err := New(Config{ID: "thread-valid", DB: db, Model: model, Tools: reg, Bash: bash, Spiller: spiller, Permissions: perm, Logger: logger}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing Queue error = %v, want %v", err, ErrInvalidConfig)
	}
	if _, err := New(Config{ID: "thread-valid", DB: db, Model: model, Tools: reg, Bash: bash, Spiller: spiller, Permissions: perm, Queue: q}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing Logger error = %v, want %v", err, ErrInvalidConfig)
	}

	h, err := New(Config{
		ID:          "thread-valid",
		DB:          db,
		Model:       model,
		Tools:       reg,
		Bash:        bash,
		Spiller:     spiller,
		Permissions: perm,
		Queue:       q,
		Logger:      logger,
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	// 2. Nil message -> ErrNilMessage
	if _, _, err := drainStream(ctx, h, nil); !errors.Is(err, ErrNilMessage) {
		t.Errorf("Stream nil message error = %v, want %v", err, ErrNilMessage)
	}

	// 3. Context cancellation before Stream / LoadState
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := drainStream(cancelCtx, h, message.NewUserText("hi")); !errors.Is(err, context.Canceled) {
		t.Errorf("Stream with canceled ctx error = %v, want %v", err, context.Canceled)
	}
	for _, err := range h.Stream(cancelCtx, Input{Decisions: map[string]tools.PermissionDecision{}}) {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Resume with canceled ctx error = %v, want %v", err, context.Canceled)
		}
	}
	if _, err := h.LoadState(cancelCtx); !errors.Is(err, context.Canceled) {
		t.Errorf("LoadState with canceled ctx error = %v, want %v", err, context.Canceled)
	}

	// 4. Context cancellation during LLM streaming
	streamCancelCtx, streamCancel := context.WithCancel(ctx)
	cancelProv := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			streamCancel()
			return []string{"chunk-1", "chunk-2"}, "resp-cancel", nil
		},
	}
	hCancel, err := New(Config{
		ID:          "thread-stream-cancel",
		DB:          db,
		Model:       newTestModel(t, cancelProv),
		Tools:       reg,
		Bash:        bash,
		Spiller:     spiller,
		Permissions: perm,
		Queue:       q,
		Logger:      logger,
	})
	if err != nil {
		t.Fatalf("New hCancel error: %v", err)
	}
	if _, _, err := drainStream(streamCancelCtx, hCancel, message.NewUserText("hi")); err == nil {
		t.Errorf("expected error when context is canceled during streaming, got nil")
	}
}

func TestCallerSuppliedUserMessageID(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	_, reg, perm := newTestToolsRegistry(t)
	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"response"}, "msg-asst-1", nil
		},
	}

	h, err := New(Config{
		ID:          "thread-custom-id",
		DB:          db,
		Model:       newTestModel(t, prov),
		Tools:       reg,
		Bash:        &fakeBashRunner{},
		Spiller:     newTestSpiller(t),
		Permissions: perm,
		Queue:       &fakeQueue{},
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	customID := uuid.Must(uuid.NewV7()).String()
	userMsg := message.NewUserText("Hello with custom id")
	userMsg.SetID(customID)

	var emittedUserMsgID string
	for ev, err := range h.Stream(ctx, Input{Message: userMsg}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		if ev.Type == EventMessage && ev.Message != nil && ev.Message.Role() == message.RoleUser {
			emittedUserMsgID = ev.Message.GetID()
		}
	}

	if emittedUserMsgID != customID {
		t.Fatalf("emitted user message ID = %q, want exact %q", emittedUserMsgID, customID)
	}
}

type fakeQueue struct {
	mu       sync.Mutex
	items    []QueueItem
	onPopMsg func()
}

func (q *fakeQueue) Peek() (QueueItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return QueueItem{}, false
	}
	return q.items[0], true
}

func (q *fakeQueue) PopCommand() (QueueItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 || q.items[0].Kind != QueueKindCommand {
		return QueueItem{}, false
	}
	it := q.items[0]
	q.items = q.items[1:]
	return it, true
}

func (q *fakeQueue) PopMessages() []message.Message {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.onPopMsg != nil {
		q.onPopMsg()
	}
	if len(q.items) == 0 {
		return nil
	}
	var msgs []message.Message
	for len(q.items) > 0 {
		if q.items[0].Kind == QueueKindCommand {
			break
		}
		it := q.items[0]
		q.items = q.items[1:]
		switch p := it.Payload.(type) {
		case QueueMessagePayload:
			msgs = append(msgs, p.Message)
		case QueueTaskPayload:
			msgs = append(msgs, p.Message)
		}
	}
	return msgs
}

type fakeBashRunner struct {
	mu       sync.Mutex
	executed []std.BashRequest
	handler  func(ctx context.Context, req std.BashRequest) (*std.BashResult, error)
}

func (b *fakeBashRunner) Bash(ctx context.Context, req std.BashRequest) (*std.BashResult, error) {
	b.mu.Lock()
	b.executed = append(b.executed, req)
	h := b.handler
	b.mu.Unlock()
	if h != nil {
		return h(ctx, req)
	}
	if req.Output != nil {
		_, _ = req.Output.Write([]byte("output of " + req.Command))
	}
	return &std.BashResult{ExitCode: 0}, nil
}

func TestInboxDrainAfterToolExecution(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	workDir, reg, perm := newTestToolsRegistry(t)

	targetFile := filepath.Join(workDir, "hello.txt")
	if err := os.WriteFile(targetFile, []byte("inbox-test-file\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var toolRequested bool
	prov := &fakeProvider{
		stepHandler: func(_ context.Context, req *llm.Request, turn int) ([]message.AssistantChunk, error) {
			if turn == 1 {
				toolRequested = true
				return []message.AssistantChunk{
					{
						BaseChunk: message.BaseChunk{ID: "asst-tool-call"},
						Content: []message.Block{
							&message.ToolCall{
								ID:   "call-view-1",
								Name: "view",
								Args: map[string]any{"path": "hello.txt"},
							},
						},
						Done: true,
					},
				}, nil
			}

			// Verify in turn 2 that the inbox message is visible in req.Messages
			var foundInbox bool
			for _, m := range req.Messages {
				if strings.Contains(msgText(m), "task completed from inbox") {
					foundInbox = true
					break
				}
			}
			if !foundInbox {
				t.Errorf("turn 2 LLM request did not contain inbox message: %+v", req.Messages)
			}

			return []message.AssistantChunk{
				{
					BaseChunk: message.BaseChunk{ID: "asst-final"},
					Content: []message.Block{
						&message.TextBlock{Text: "All done."},
					},
					Done: true,
				},
			}, nil
		},
	}

	inboxMsg := message.NewUserTextMeta("task completed from inbox", map[string]any{MetaSystemKind: SystemKindTask})
	var inboxDrained bool
	var q *fakeQueue
	q = &fakeQueue{
		onPopMsg: func() {
			if toolRequested && !inboxDrained {
				inboxDrained = true
				q.items = append(q.items, QueueItem{
					ID:      "task-item-1",
					Kind:    QueueKindTask,
					Payload: QueueTaskPayload{Message: inboxMsg},
				})
			}
		},
	}

	h, err := New(Config{
		ID:          "thread-inbox-test",
		DB:          db,
		Model:       newTestModel(t, prov),
		Tools:       reg,
		Permissions: perm,
		Queue:       q,
		Bash:        &fakeBashRunner{},
		Spiller:     newTestSpiller(t),
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	var emittedMessages []message.Message
	for ev, err := range h.Stream(ctx, Input{Message: message.NewUserText("Read file")}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		if ev.Type == EventMessage {
			emittedMessages = append(emittedMessages, ev.Message)
		}
	}

	if !inboxDrained {
		t.Fatal("expected inbox to be drained")
	}

	// Verify emittedMessages contains:
	// 0: User ("Read file")
	// 1: Assistant (ToolCall)
	// 2: Tool (Result)
	// 3: Assistant (Final "All done.")
	// (Inbox messages are NOT emitted to the user-facing stream to avoid polluting chat history)
	if len(emittedMessages) != 4 {
		t.Fatalf("expected 4 emitted messages, got %d: %+v", len(emittedMessages), emittedMessages)
	}

	finalAsst := emittedMessages[3]
	if !strings.Contains(msgText(finalAsst), "All done.") {
		t.Fatalf("emittedMessages[3] = %q, want final assistant message", msgText(finalAsst))
	}

	// Verify checkpointed state contains the inbox message
	state, err := h.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}
	if len(state.Messages) != 5 {
		t.Fatalf("expected 5 messages in state, got %d", len(state.Messages))
	}
	if !strings.Contains(msgText(state.Messages[3]), "task completed from inbox") {
		t.Fatalf("state.Messages[3] = %q, want inbox message", msgText(state.Messages[3]))
	}
	if state.Messages[3].GetID() == "" {
		t.Fatal("expected non-empty ID for drained inbox message in state")
	}
}

func TestInboxDrainDuringAgentWithoutTools(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	_, reg, perm := newTestToolsRegistry(t)

	var queueMsg bool
	var drained bool
	inboxMsg := message.NewUserTextMeta("inbox message arrived during agent turn", map[string]any{MetaSystemKind: SystemKindTask})

	prov := &fakeProvider{
		stepHandler: func(_ context.Context, req *llm.Request, turn int) ([]message.AssistantChunk, error) {
			if turn == 1 {
				// While agent turn 1 is running, queue an inbox message!
				queueMsg = true
				return []message.AssistantChunk{
					{
						BaseChunk: message.BaseChunk{ID: "asst-first"},
						Content: []message.Block{
							&message.TextBlock{Text: "Initial response without tools."},
						},
						Done: true,
					},
				}, nil
			}

			// In turn 2, verify agent received the inbox message!
			var foundInbox bool
			for _, m := range req.Messages {
				if strings.Contains(msgText(m), "inbox message arrived during agent turn") {
					foundInbox = true
					break
				}
			}
			if !foundInbox {
				t.Errorf("turn 2 LLM request did not contain inbox message: %+v", req.Messages)
			}

			return []message.AssistantChunk{
				{
					BaseChunk: message.BaseChunk{ID: "asst-second"},
					Content: []message.Block{
						&message.TextBlock{Text: "Acknowledged inbox message."},
					},
					Done: true,
				},
			}, nil
		},
	}

	var q *fakeQueue
	q = &fakeQueue{
		onPopMsg: func() {
			if queueMsg && !drained {
				drained = true
				q.items = append(q.items, QueueItem{
					ID:      "msg-item-1",
					Kind:    QueueKindMessage,
					Payload: QueueMessagePayload{Message: inboxMsg},
				})
			}
		},
	}

	h, err := New(Config{
		ID:          "thread-inbox-agent-loop",
		DB:          db,
		Model:       newTestModel(t, prov),
		Tools:       reg,
		Permissions: perm,
		Queue:       q,
		Bash:        &fakeBashRunner{},
		Spiller:     newTestSpiller(t),
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	var emittedMessages []message.Message
	for ev, err := range h.Stream(ctx, Input{Message: message.NewUserText("Start turn")}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		if ev.Type == EventMessage {
			emittedMessages = append(emittedMessages, ev.Message)
		}
	}

	if !drained {
		t.Fatal("expected inbox to be drained after agent step")
	}

	// Sequence for emittedMessages:
	// 0: User ("Start turn")
	// 1: Assistant ("Initial response without tools.")
	// 2: Assistant ("Acknowledged inbox message.")
	// (Inbox messages are internal and not emitted to the user-facing stream)
	if len(emittedMessages) != 3 {
		t.Fatalf("expected 3 emitted messages, got %d: %+v", len(emittedMessages), emittedMessages)
	}

	if !strings.Contains(msgText(emittedMessages[1]), "Initial response without tools") {
		t.Fatalf("emittedMessages[1] = %q, want first assistant message", msgText(emittedMessages[1]))
	}
	if !strings.Contains(msgText(emittedMessages[2]), "Acknowledged inbox message") {
		t.Fatalf("emittedMessages[2] = %q, want final assistant message", msgText(emittedMessages[2]))
	}

	// Verify checkpointed state contains 4 messages:
	state, err := h.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}
	if len(state.Messages) != 4 {
		t.Fatalf("expected 4 messages in state, got %d", len(state.Messages))
	}
	if !strings.Contains(msgText(state.Messages[2]), "inbox message arrived during agent turn") {
		t.Fatalf("state.Messages[2] = %q, want inbox message", msgText(state.Messages[2]))
	}
}

func TestInboxDirectTurnWithEmptyInput(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	_, reg, perm := newTestToolsRegistry(t)

	var drained bool
	prov := &fakeProvider{
		stepHandler: func(_ context.Context, _ *llm.Request, _ int) ([]message.AssistantChunk, error) {
			return []message.AssistantChunk{
				{
					BaseChunk: message.BaseChunk{ID: "asst-reply"},
					Content: []message.Block{
						&message.TextBlock{Text: "Processed inbox task."},
					},
					Done: true,
				},
			}, nil
		},
	}
	q := &fakeQueue{
		items: []QueueItem{
			{
				ID:      "task-item-1",
				Kind:    QueueKindTask,
				Payload: QueueTaskPayload{Message: message.NewUserTextMeta("inbox task notification", map[string]any{MetaSystemKind: SystemKindTask})},
			},
		},
		onPopMsg: func() {
			drained = true
		},
	}

	h, err := New(Config{
		ID:          "thread-inbox-empty-input",
		DB:          db,
		Model:       newTestModel(t, prov),
		Tools:       reg,
		Permissions: perm,
		Queue:       q,
		Bash:        &fakeBashRunner{},
		Spiller:     newTestSpiller(t),
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	// Calling Stream with empty Input{} initiates an inbox drain turn!
	var emittedMessages []message.Message
	for ev, err := range h.Stream(ctx, Input{}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		if ev.Type == EventMessage {
			emittedMessages = append(emittedMessages, ev.Message)
		}
	}

	if !drained {
		t.Fatal("expected inbox to be drained on Input{} turn")
	}

	// Only the assistant response is emitted
	if len(emittedMessages) != 1 || !strings.Contains(msgText(emittedMessages[0]), "Processed inbox task.") {
		t.Fatalf("expected 1 emitted assistant message, got %+v", emittedMessages)
	}

	// But state contains both the drained inbox user message and the assistant reply
	state, err := h.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}
	if len(state.Messages) != 2 {
		t.Fatalf("expected 2 messages in state, got %d: %+v", len(state.Messages), state.Messages)
	}
}

func TestCommandNodeDoesNotWakeAgent(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	_, reg, perm := newTestToolsRegistry(t)

	cmdID := uuid.Must(uuid.NewV7()).String()
	q := &fakeQueue{
		items: []QueueItem{
			{
				ID:      cmdID,
				Kind:    QueueKindCommand,
				Payload: QueueCommandPayload{Command: "echo direct"},
			},
		},
	}
	bash := &fakeBashRunner{}

	prov := &fakeProvider{
		stepHandler: func(_ context.Context, _ *llm.Request, _ int) ([]message.AssistantChunk, error) {
			t.Fatal("agent should NOT be woken by command execution")
			return nil, nil
		},
	}

	h, err := New(Config{
		ID:          "thread-command-no-agent",
		DB:          db,
		Model:       newTestModel(t, prov),
		Tools:       reg,
		Permissions: perm,
		Queue:       q,
		Bash:        bash,
		Spiller:     newTestSpiller(t),
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	var events []Event
	for ev, err := range h.Stream(ctx, Input{}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		events = append(events, ev)
	}

	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != EventCommandStarted || events[0].CommandStarted == nil || events[0].CommandStarted.ID != cmdID {
		t.Fatalf("expected EventCommandStarted %s, got %+v", cmdID, events[0])
	}
	if events[1].Type != EventCommandCompleted || events[1].CommandCompleted == nil || events[1].CommandCompleted.ID != cmdID {
		t.Fatalf("expected EventCommandCompleted %s, got %+v", cmdID, events[1])
	}
	if events[2].Type != EventContextUpdated || events[2].Context == nil {
		t.Fatalf("expected EventContextUpdated, got %+v", events[2])
	}
	if events[3].Type != EventCompleted || events[3].Message != nil {
		t.Fatalf("expected EventCompleted with nil Message, got %+v", events[3])
	}

	state, err := h.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}
	if len(state.Messages) != 1 {
		t.Fatalf("expected 1 message in state, got %d", len(state.Messages))
	}
	cmdMsg := state.Messages[0]
	if SystemKindOf(cmdMsg) != SystemKindCommand {
		t.Fatalf("expected SystemKindCommand, got %q", SystemKindOf(cmdMsg))
	}
	if !strings.Contains(cmdMsg.GetContent().Text(), `<bash_command command="echo direct" exit_code="0">`) {
		t.Fatalf("expected bash_command tag in state, got %s", cmdMsg.GetContent().Text())
	}
}

func TestUnifiedQueueOrderingAcrossGraph(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	_, reg, perm := newTestToolsRegistry(t)

	q := &fakeQueue{}
	bash := &fakeBashRunner{}

	var toolRequested bool
	var queuePopulated bool
	q.onPopMsg = func() {
		if toolRequested && !queuePopulated {
			queuePopulated = true
			q.items = append(q.items,
				QueueItem{ID: "u1", Kind: QueueKindMessage, Payload: QueueMessagePayload{Message: message.NewUserText("user msg 1")}},
				QueueItem{ID: "u2", Kind: QueueKindMessage, Payload: QueueMessagePayload{Message: message.NewUserText("user msg 2")}},
				QueueItem{ID: "t1", Kind: QueueKindTask, Payload: QueueTaskPayload{Message: message.NewUserTextMeta("task update 1", map[string]any{MetaSystemKind: SystemKindTask})}},
				QueueItem{ID: "cmd-c1", Kind: QueueKindCommand, Payload: QueueCommandPayload{Command: "git status"}},
			)
		}
	}

	var toolTurnRan bool
	var agentSecondTurnRan bool
	prov := &fakeProvider{
		stepHandler: func(_ context.Context, req *llm.Request, turn int) ([]message.AssistantChunk, error) {
			if turn == 1 {
				toolTurnRan = true
				toolRequested = true
				return []message.AssistantChunk{
					{
						BaseChunk: message.BaseChunk{ID: "asst-tool-call"},
						Content: []message.Block{
							&message.ToolCall{
								ID:   "tc-1",
								Name: "view",
								Args: map[string]any{"path": "hello.txt"},
							},
						},
						Done: true,
					},
				}, nil
			}

			// Turn 2 should see the initial user message, tool call, tool result, U1, U2, and T1!
			agentSecondTurnRan = true
			var foundU1, foundU2, foundT1 bool
			for _, m := range req.Messages {
				txt := msgText(m)
				if strings.Contains(txt, "user msg 1") {
					foundU1 = true
				}
				if strings.Contains(txt, "user msg 2") {
					foundU2 = true
				}
				if strings.Contains(txt, "task update 1") {
					foundT1 = true
				}
			}
			if !foundU1 || !foundU2 || !foundT1 {
				t.Errorf("turn 2 missing expected messages (u1=%v, u2=%v, t1=%v): %+v", foundU1, foundU2, foundT1, req.Messages)
			}

			return []message.AssistantChunk{
				{
					BaseChunk: message.BaseChunk{ID: "asst-done"},
					Content: []message.Block{
						&message.TextBlock{Text: "All done."},
					},
					Done: true,
				},
			}, nil
		},
	}

	h, err := New(Config{
		ID:          "thread-unified-queue-ordering",
		DB:          db,
		Model:       newTestModel(t, prov),
		Tools:       reg,
		Permissions: perm,
		Queue:       q,
		Bash:        bash,
		Spiller:     newTestSpiller(t),
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	var emittedMessages []message.Message
	var cmdStartedCount, cmdCompletedCount int
	for ev, err := range h.Stream(ctx, Input{Message: message.NewUserText("Initial turn")}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		switch ev.Type {
		case EventMessage:
			emittedMessages = append(emittedMessages, ev.Message)
		case EventCommandStarted:
			cmdStartedCount++
		case EventCommandCompleted:
			cmdCompletedCount++
		}
	}

	if !toolTurnRan {
		t.Fatal("expected first agent tool turn to run")
	}
	if !agentSecondTurnRan {
		t.Fatal("expected second agent turn to run after inbox drain")
	}
	if cmdStartedCount != 1 || cmdCompletedCount != 1 {
		t.Fatalf("expected 1 command started/completed, got started=%d completed=%d", cmdStartedCount, cmdCompletedCount)
	}

	// Verify emittedMessages:
	// 0: User ("Initial turn")
	// 1: Assistant (tool call)
	// 2: Tool (result)
	// 3: User ("user msg 1")  -- emitted by InboxNode
	// 4: User ("user msg 2")  -- emitted by InboxNode
	// (task update 1 is NOT emitted because SystemKindOf != "")
	// 5: Assistant ("All done.")
	if len(emittedMessages) != 6 {
		t.Fatalf("expected 6 emitted messages, got %d: %+v", len(emittedMessages), emittedMessages)
	}
	if !strings.Contains(msgText(emittedMessages[3]), "user msg 1") {
		t.Errorf("expected emittedMessages[3] = user msg 1, got %q", msgText(emittedMessages[3]))
	}
	if !strings.Contains(msgText(emittedMessages[4]), "user msg 2") {
		t.Errorf("expected emittedMessages[4] = user msg 2, got %q", msgText(emittedMessages[4]))
	}

	// Verify state has all messages in exact order:
	// 0: Initial turn (U0)
	// 1: Assistant (tool call)
	// 2: Tool (result)
	// 3: user msg 1 (U1)
	// 4: user msg 2 (U2)
	// 5: task update 1 (T1)
	// 6: Assistant ("All done.")
	// 7: <bash_command command="git status"...> (C1)
	state, err := h.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}
	if len(state.Messages) != 8 {
		t.Fatalf("expected 8 messages in state, got %d: %+v", len(state.Messages), state.Messages)
	}

	lastMsg := state.Messages[7]
	if SystemKindOf(lastMsg) != SystemKindCommand {
		t.Fatalf("expected last message to be SystemKindCommand, got %q", SystemKindOf(lastMsg))
	}
	if !strings.Contains(lastMsg.GetContent().Text(), `<bash_command command="git status"`) {
		t.Fatalf("expected git status command in last message, got %s", lastMsg.GetContent().Text())
	}
}

func TestTodosToolExecutionAndCheckpoint(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	_, reg, perm := newTestToolsRegistry(t)

	prov := &fakeProvider{
		stepHandler: func(_ context.Context, _ *llm.Request, turn int) ([]message.AssistantChunk, error) {
			if turn == 1 {
				return []message.AssistantChunk{
					{
						BaseChunk: message.BaseChunk{ID: "asst-todos-call"},
						Content: []message.Block{
							&message.TextBlock{Text: "Updating checklist."},
							&message.ToolCall{
								ID:   "call-todos-1",
								Name: "todos",
								Args: map[string]any{
									"todos": []any{
										map[string]any{
											"content":     "Step 1",
											"active_text": "Doing step 1",
											"status":      "in_progress",
										},
										map[string]any{
											"content":     "Step 2",
											"active_text": "Queued step 2",
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
					BaseChunk: message.BaseChunk{ID: "asst-final"},
					Content: []message.Block{
						&message.TextBlock{Text: "Checklist created."},
					},
					Done: true,
				},
			}, nil
		},
	}

	h, err := New(Config{
		ID:          "thread-todos",
		DB:          db,
		Model:       newTestModel(t, prov),
		Tools:       reg,
		Bash:        &fakeBashRunner{},
		Spiller:     newTestSpiller(t),
		Permissions: perm,
		Queue:       &fakeQueue{},
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	var todosEvents []Event
	for ev, err := range h.Stream(ctx, Input{Message: message.NewUserText("Start work")}) {
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		if ev.Type == EventTodosUpdated {
			todosEvents = append(todosEvents, ev)
		}
	}

	if len(todosEvents) != 1 {
		t.Fatalf("expected 1 EventTodosUpdated, got %d", len(todosEvents))
	}
	if len(todosEvents[0].Todos) != 2 {
		t.Fatalf("expected 2 items in EventTodosUpdated, got %d", len(todosEvents[0].Todos))
	}
	if todosEvents[0].Todos[0].Content != "Step 1" || todosEvents[0].Todos[0].Status != tools.TodoStatusInProgress {
		t.Fatalf("unexpected todo item 0: %+v", todosEvents[0].Todos[0])
	}

	// Verify State checkpoint includes Todos
	state, err := h.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState error: %v", err)
	}
	if len(state.Todos) != 2 {
		t.Fatalf("expected 2 todos in checkpointed State, got %d", len(state.Todos))
	}
	if state.Todos[0].ActiveText != "Doing step 1" || state.Todos[1].Status != tools.TodoStatusPending {
		t.Fatalf("unexpected checkpointed todos: %+v", state.Todos)
	}
}

func TestMessageUpdateContextOptions(t *testing.T) {
	first := message.NewUserText("first")
	second := message.NewUserText("second")
	third := message.NewUserText("third")
	cases := []struct {
		name   string
		update func(State, MessageUpdateOptions) State
		want   message.MessageList
	}{
		{"prepend", func(s State, opts MessageUpdateOptions) State { return s.PrependMessage(first, opts) }, message.MessageList{first, second}},
		{"append", func(s State, opts MessageUpdateOptions) State { return s.AppendMessage(third, opts) }, message.MessageList{second, third}},
		{"append multiple", func(s State, opts MessageUpdateOptions) State {
			return s.AppendMessages(message.MessageList{first, third}, opts)
		}, message.MessageList{second, first, third}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			initial := State{Messages: message.MessageList{second}, Context: api.ContextState{UsedTokens: 999, CachedTokens: 12}}
			deferred := tc.update(initial.Copy(), MessageUpdateOptions{SkipContextRecalculation: true})
			if !reflect.DeepEqual(deferred.Messages, tc.want) || !reflect.DeepEqual(deferred.Context, initial.Context) {
				t.Fatalf("deferred update changed context or message order: %+v", deferred)
			}

			opts := MessageUpdateOptions{Static: StaticContext{SystemPrompt: 42}, Limits: workspace.ModelLimits{Context: 100000, Output: 1000}}
			updated := tc.update(initial.Copy(), opts)
			if !reflect.DeepEqual(updated.Messages, tc.want) || !reflect.DeepEqual(updated.Context, computeContextState(opts.Static, updated, opts.Limits)) {
				t.Fatalf("update did not recalculate context: %+v", updated)
			}
			defaults := tc.update(initial.Copy(), MessageUpdateOptions{})
			if defaults.Context.MaxTokens != 128000 {
				t.Fatalf("zero-value options did not recalculate with default limits: %+v", defaults.Context)
			}
		})
	}
}

func TestDeferredMessageUpdatesBeforeCompaction(t *testing.T) {
	static := StaticContext{SystemPrompt: 10}
	limits := workspace.ModelLimits{Context: 100000, Output: 1000}
	original := State{PendingCompaction: &CompactInput{}, Context: api.ContextState{UsedTokens: 999, CachedTokens: 80, Compactions: 2}}
	summary := message.NewUserText("summary")
	first := message.NewUserText("first")
	second := message.NewUserText("second")
	deferred := MessageUpdateOptions{SkipContextRecalculation: true}
	state := original.AppendMessage(first, deferred)
	state = state.AppendMessages(message.MessageList{second}, deferred)
	state = state.PrependMessage(summary, deferred)
	if !reflect.DeepEqual(state.Context, original.Context) {
		t.Fatalf("context changed during deferred updates: %+v", state.Context)
	}
	state = state.ApplyCompaction(static, limits)
	if !reflect.DeepEqual(state.Messages, message.MessageList{summary, first, second}) {
		t.Fatalf("unexpected message order: %+v", state.Messages)
	}
	if state.PendingCompaction != nil || state.Context.CachedTokens != 0 || state.Context.Compactions != 3 || state.Context.LastCompacted.IsZero() {
		t.Fatalf("unexpected compaction state: %+v", state)
	}
	if want := computeContextState(static, state, limits); !reflect.DeepEqual(state.Context, want) {
		t.Fatalf("context not recalculated after compaction: got %+v, want %+v", state.Context, want)
	}
}

func TestAutoCompactionTurnAndCommandBoundaries(t *testing.T) {
	ctx := context.Background()
	_, reg, perm := newTestToolsRegistry(t)
	q := &fakeQueue{}
	prov := &fakeProvider{
		stepHandler: func(_ context.Context, _ *llm.Request, turn int) ([]message.AssistantChunk, error) {
			text := "answer"
			var metrics *message.TokenMetrics
			switch turn {
			case 1:
				text = strings.Repeat("a", 4000)
				metrics = &message.TokenMetrics{TotalTokens: 1000}
			case 2:
				text = "previous conversation summary"
			}
			return []message.AssistantChunk{{Content: []message.Block{&message.TextBlock{Text: text}}, Metrics: metrics, Done: true}}, nil
		},
	}
	h, err := New(Config{
		ID: "compact-boundaries", DB: openTestSQLite(t), Model: newTestModel(t, prov),
		Tools: reg, Permissions: perm, Queue: q, Bash: &fakeBashRunner{},
		Spiller: newTestSpiller(t), Logger: log.Default(),
		Limits: workspace.ModelLimits{Context: 10000, Output: 1000},
	})
	if err != nil {
		t.Fatal(err)
	}

	run := func(input Input) []Event {
		t.Helper()
		var events []Event
		for event, err := range h.Stream(ctx, input) {
			if err != nil {
				t.Fatal(err)
			}
			events = append(events, event)
		}
		return events
	}
	assertNoCompaction := func(events []Event) {
		t.Helper()
		for _, event := range events {
			if event.Type == EventCompactionStarted || event.Type == EventCompactionCompleted {
				t.Fatalf("unexpected compaction event: %+v", event)
			}
		}
	}

	assertNoCompaction(run(Input{Message: message.NewUserText("first request")}))
	state, err := h.LoadState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Context.UsedTokens != state.Context.TriggerTokens || len(prov.recordedTurns()) != 1 {
		t.Fatalf("expected one completed turn at the threshold: context=%+v calls=%d", state.Context, len(prov.recordedTurns()))
	}

	q.items = append(q.items, QueueItem{Kind: QueueKindCommand, Payload: QueueCommandPayload{Command: "show status"}})
	assertNoCompaction(run(Input{}))
	state, err = h.LoadState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Context.UsedTokens < state.Context.TriggerTokens || len(prov.recordedTurns()) != 1 {
		t.Fatalf("command unexpectedly invoked the model: context=%+v calls=%d", state.Context, len(prov.recordedTurns()))
	}

	const prompt = "new request with exact stack trace\nline 42"
	events := run(Input{Message: message.NewUserText(prompt)})
	starts, completions := 0, 0
	var chunks []string
	for _, event := range events {
		switch event.Type {
		case EventCompactionStarted:
			starts++
		case EventCompactionCompleted:
			completions++
		case EventChunk:
			chunks = append(chunks, extractBlocksText(event.Chunk.Content))
		}
	}
	if len(chunks) != 1 || chunks[0] != "answer" {
		t.Fatalf("expected only assistant answer chunks, got %v", chunks)
	}
	turns := prov.recordedTurns()
	if starts != 1 || completions != 1 || len(turns) != 3 {
		t.Fatalf("expected one compaction and one answer: starts=%d completions=%d calls=%d", starts, completions, len(turns))
	}
	if len(turns[1].messages) != 3 || msgText(turns[1].messages[0]) != "first request" || msgText(turns[1].messages[1]) != strings.Repeat("a", 4000) {
		t.Fatalf("summarization did not preserve the prior prefix: %+v", turns[1].messages)
	}
	messages := turns[2].messages
	if len(messages) != 3 || SystemKindOf(messages[0]) != SystemKindCompaction || SystemKindOf(messages[1]) != SystemKindCommand || msgText(messages[2]) != prompt {
		t.Fatalf("agent did not receive summary, command output, and exact prompt: %+v", messages)
	}
	if strings.Contains(msgText(messages[0]), "Continue working on the latest user request above") {
		t.Fatal("entry summary contains a continuation directive")
	}
}

func TestManualCompactionDoesNotStreamSummaryChunks(t *testing.T) {
	ctx := context.Background()
	_, reg, perm := newTestToolsRegistry(t)
	provider := &fakeProvider{
		stepHandler: func(_ context.Context, _ *llm.Request, turn int) ([]message.AssistantChunk, error) {
			text := "answer"
			if turn == 2 {
				text = "private summary"
			}
			return []message.AssistantChunk{{Content: []message.Block{&message.TextBlock{Text: text}}, Done: true}}, nil
		},
	}
	h, err := New(Config{
		ID: "manual-compact-stream", DB: openTestSQLite(t), Model: newTestModel(t, provider),
		Tools: reg, Permissions: perm, Queue: &fakeQueue{}, Bash: &fakeBashRunner{},
		Spiller: newTestSpiller(t), Logger: log.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range h.Stream(ctx, Input{Message: message.NewUserText("request")}) {
		if err != nil {
			t.Fatal(err)
		}
	}
	started, completed, finished := 0, 0, 0
	var chunks []string
	for event, err := range h.Stream(ctx, Input{Compact: &CompactInput{Focus: "focus"}}) {
		if err != nil {
			t.Fatal(err)
		}
		switch event.Type {
		case EventChunk:
			chunks = append(chunks, extractBlocksText(event.Chunk.Content))
		case EventCompactionStarted:
			started++
			if event.CompactionStarted.Trigger != "manual" || event.CompactionStarted.Focus != "focus" {
				t.Fatalf("unexpected compaction start: %+v", event.CompactionStarted)
			}
		case EventCompactionCompleted:
			completed++
			if event.CompactionCompleted.Summary != "private summary" {
				t.Fatalf("unexpected compaction summary: %+v", event.CompactionCompleted)
			}
		case EventCompleted:
			finished++
		}
	}
	if started != 1 || completed != 1 || finished != 1 || len(provider.recordedTurns()) != 2 {
		t.Fatalf("unexpected lifecycle events: started=%d completed=%d finished=%d calls=%d", started, completed, finished, len(provider.recordedTurns()))
	}
	if len(chunks) != 0 {
		t.Fatalf("expected no summary or agent chunks, got %v", chunks)
	}
	state, err := h.LoadState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 1 || SystemKindOf(state.Messages[0]) != SystemKindCompaction || state.Context.Compactions != 1 {
		t.Fatalf("compaction was not persisted: %+v", state)
	}
}

func TestConsumeGraphStreamFiltersNonAgentChunks(t *testing.T) {
	chunk := message.AssistantChunk{Content: []message.Block{&message.TextBlock{Text: "text"}}}
	events := []graph.StreamEvent{
		{Node: "compact", Event: graph.EventLLMChunk, Data: chunk},
		{Node: "tools", Event: graph.EventLLMChunk, Data: chunk},
		{Node: "agent", Event: graph.EventLLMChunk, Data: chunk},
	}
	var received []Event
	(&harness{}).consumeGraphStream(func(yield func(graph.StreamEvent, error) bool) {
		for _, event := range events {
			if !yield(event, nil) {
				return
			}
		}
	}, func(event Event, err error) bool {
		if err != nil {
			t.Fatal(err)
		}
		received = append(received, event)
		return true
	})
	if len(received) != 1 || received[0].Type != EventChunk || extractBlocksText(received[0].Chunk.Content) != "text" {
		t.Fatalf("expected only agent chunk, got %+v", received)
	}
}
