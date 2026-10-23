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
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/std"
	"github.com/masterkeysrd/tasksmith/internal/tools"
	"github.com/masterkeysrd/tasksmith/internal/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
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

func newTestToolsRegistry(t *testing.T) (string, *tools.Container) {
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
		Std:     stdLib,
		Sandbox: sb,
		Guard:   guard,
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

	return dir, container
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

func drainStream(ctx context.Context, h *Harness, threadID string, input message.Message) ([]string, *message.Assistant, error) {
	var deltas []string
	var completed *message.Assistant
	for ev, err := range h.Stream(ctx, threadID, input) {
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
	_, reg := newTestToolsRegistry(t)
	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"Task", "Smith", " v2", " ready."}, "msg-assistant-1", nil
		},
	}

	h, err := New(Config{
		DB:     db,
		Model:  newTestModel(t, prov),
		Tools:  reg,
		Logger: log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	deltas, reply, err := drainStream(ctx, h, "thread-1", message.NewUserText("Hi there"))
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
	_, reg := newTestToolsRegistry(t)

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
		DB:     db,
		Model:  model,
		Tools:  reg,
		Logger: log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	// Verify empty state before any turn on thread
	initialState, err := h.LoadState(ctx, "thread-alice")
	if err != nil {
		t.Fatalf("LoadState initial error: %v", err)
	}
	if len(initialState.Messages) != 0 {
		t.Fatalf("expected 0 messages initially, got %d", len(initialState.Messages))
	}

	// Turn 1
	_, reply1, err := drainStream(ctx, h, "thread-alice", message.NewUserText("My name is Alice"))
	if err != nil {
		t.Fatalf("Turn 1 Stream error: %v", err)
	}
	if got := msgText(reply1); got != "Nice to meet you, Alice!" {
		t.Errorf("Turn 1 reply = %q, want %q", got, "Nice to meet you, Alice!")
	}

	// Turn 2 on the same threadID across a freshly constructed Harness sharing the same SQLite DB
	h2, err := New(Config{
		DB:     db,
		Model:  model,
		Tools:  reg,
		Logger: log.Default(),
	})
	if err != nil {
		t.Fatalf("New h2 error: %v", err)
	}

	_, reply2, err := drainStream(ctx, h2, "thread-alice", message.NewUserText("What is my name?"))
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
	state, err := h2.LoadState(ctx, "thread-alice")
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
	_, reg := newTestToolsRegistry(t)

	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"Understood."}, "resp-sys-1", nil
		},
	}

	sysPrompt := "You are TaskSmith v2.\n\nFollow strict Go conventions."
	h, err := New(Config{
		DB:           db,
		Model:        newTestModel(t, prov),
		Tools:        reg,
		SystemPrompt: sysPrompt,
		Logger:       log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	_, _, err = drainStream(ctx, h, "thread-sys", message.NewUserText("Ping"))
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
	state, err := h.LoadState(ctx, "thread-sys")
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
	_, reg := newTestToolsRegistry(t)

	prov := &fakeProvider{
		handler: func(_ context.Context, req *llm.Request) ([]string, string, error) {
			lastMsg := req.Messages[len(req.Messages)-1]
			return []string{"Echo: " + msgText(lastMsg)}, "", nil
		},
	}

	h, err := New(Config{
		DB:     db,
		Model:  newTestModel(t, prov),
		Tools:  reg,
		Logger: log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	if _, _, err := drainStream(ctx, h, "thread-A", message.NewUserText("Message for A")); err != nil {
		t.Fatalf("Stream thread-A error: %v", err)
	}
	if _, _, err := drainStream(ctx, h, "thread-B", message.NewUserText("Message for B")); err != nil {
		t.Fatalf("Stream thread-B error: %v", err)
	}

	stateA, err := h.LoadState(ctx, "thread-A")
	if err != nil {
		t.Fatalf("LoadState thread-A error: %v", err)
	}
	stateB, err := h.LoadState(ctx, "thread-B")
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
	workDir, reg := newTestToolsRegistry(t)

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
		DB:     db,
		Model:  newTestModel(t, prov),
		Tools:  reg,
		Logger: log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	var emittedMessages []message.Message
	var completedMsg message.Message

	for ev, err := range h.Stream(ctx, "thread-tools", message.NewUserText("Read hello.txt")) {
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
	state, err := h.LoadState(ctx, "thread-tools")
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
	workDir, reg := newTestToolsRegistry(t)

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
		DB:     db,
		Model:  newTestModel(t, prov),
		Tools:  reg,
		Logger: log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	// 1. Calling Resume on a thread with no pending permissions -> ErrNoPendingPermissions
	for _, err := range h.Resume(ctx, "thread-perm", nil) {
		if !errors.Is(err, ErrNoPendingPermissions) {
			t.Fatalf("Resume without pending permissions error = %v, want %v", err, ErrNoPendingPermissions)
		}
	}

	// 2. Run Stream -> ls executes immediately, while the 2 write calls pause at ToolsNode with EventInterrupted
	var streamToolMsgs []*message.Tool
	var interruptedPerms []tools.PermissionRequest
	for ev, err := range h.Stream(ctx, "thread-perm", message.NewUserText("Write two files")) {
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

	statePaused, err := h.LoadState(ctx, "thread-perm")
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
	for _, err := range h.Resume(ctx, "thread-perm", partialDecisions) {
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
	for ev, err := range h.Resume(ctx, "thread-perm", fullDecisions) {
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
	finalState, err := h.LoadState(ctx, "thread-perm")
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
	workDir, reg := newTestToolsRegistry(t)

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
		DB:     db,
		Model:  newTestModel(t, prov),
		Tools:  reg,
		Logger: log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	// Turn 1: Trigger permission interrupt
	var interrupted bool
	for ev, err := range h.Stream(ctx, "thread-cancel-perm", message.NewUserText("Write never-written.txt")) {
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
	for ev, err := range h.Stream(ctx, "thread-cancel-perm", message.NewUserText("Actually, cancel that and do something else")) {
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

	state, err := h.LoadState(ctx, "thread-cancel-perm")
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
		if !(state.Messages[i-1].GetID() < state.Messages[i].GetID()) {
			t.Errorf("expected strictly increasing UUIDv7 IDs: state.Messages[%d].ID (%s) >= state.Messages[%d].ID (%s)",
				i-1, state.Messages[i-1].GetID(), i, state.Messages[i].GetID())
		}
	}
}

func TestMixedToolCallsAndPlanModeDeny(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	workDir, reg := newTestToolsRegistry(t)

	if err := os.WriteFile(filepath.Join(workDir, "readme.txt"), []byte("hello plan\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Put registry in ModePlan: view is allowed, write is denied (AccessDeny without interrupt) and unknown tool fails Prepare
	reg.SetMode(tools.ModePlan)

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
		DB:     db,
		Model:  newTestModel(t, prov),
		Tools:  reg,
		Logger: log.Default(),
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	var toolMsgs []*message.Tool
	for ev, err := range h.Stream(ctx, "thread-plan", message.NewUserText("Try view, write, and unknown")) {
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
	_, reg := newTestToolsRegistry(t)
	model := newTestModel(t, nil)
	logger := log.Default()

	// 1. Validate all 4 required Config fields
	if _, err := New(Config{Model: model, Tools: reg, Logger: logger}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing DB error = %v, want %v", err, ErrInvalidConfig)
	}
	if _, err := New(Config{DB: db, Tools: reg, Logger: logger}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing Model error = %v, want %v", err, ErrInvalidConfig)
	}
	if _, err := New(Config{DB: db, Model: model, Logger: logger}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing Tools error = %v, want %v", err, ErrInvalidConfig)
	}
	if _, err := New(Config{DB: db, Model: model, Tools: reg}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing Logger error = %v, want %v", err, ErrInvalidConfig)
	}

	h, err := New(Config{
		DB:     db,
		Model:  model,
		Tools:  reg,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	// 2. Empty threadID -> ErrEmptyThreadID on Stream, Resume, and LoadState; nil input -> ErrNilMessage
	if _, _, err := drainStream(ctx, h, "", message.NewUserText("hi")); !errors.Is(err, ErrEmptyThreadID) {
		t.Errorf("Stream empty threadID error = %v, want %v", err, ErrEmptyThreadID)
	}
	if _, _, err := drainStream(ctx, h, "   ", message.NewUserText("hi")); !errors.Is(err, ErrEmptyThreadID) {
		t.Errorf("Stream whitespace threadID error = %v, want %v", err, ErrEmptyThreadID)
	}
	if _, _, err := drainStream(ctx, h, "thread-valid", nil); !errors.Is(err, ErrNilMessage) {
		t.Errorf("Stream nil message error = %v, want %v", err, ErrNilMessage)
	}
	for _, err := range h.Resume(ctx, "", nil) {
		if !errors.Is(err, ErrEmptyThreadID) {
			t.Errorf("Resume empty threadID error = %v, want %v", err, ErrEmptyThreadID)
		}
	}
	if _, err := h.LoadState(ctx, ""); !errors.Is(err, ErrEmptyThreadID) {
		t.Errorf("LoadState empty threadID error = %v, want %v", err, ErrEmptyThreadID)
	}

	// 3. Context cancellation before Stream / Resume / LoadState
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := drainStream(cancelCtx, h, "thread-cancel", message.NewUserText("hi")); !errors.Is(err, context.Canceled) {
		t.Errorf("Stream with canceled ctx error = %v, want %v", err, context.Canceled)
	}
	for _, err := range h.Resume(cancelCtx, "thread-cancel", nil) {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Resume with canceled ctx error = %v, want %v", err, context.Canceled)
		}
	}
	if _, err := h.LoadState(cancelCtx, "thread-cancel"); !errors.Is(err, context.Canceled) {
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
		DB:     db,
		Model:  newTestModel(t, cancelProv),
		Tools:  reg,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("New hCancel error: %v", err)
	}
	if _, _, err := drainStream(streamCancelCtx, hCancel, "thread-stream-cancel", message.NewUserText("hi")); err == nil {
		t.Errorf("expected error when context is canceled during streaming, got nil")
	}
}
