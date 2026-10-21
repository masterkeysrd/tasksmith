package harness

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
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
	db, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
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
				deltas = append(deltas, extractBlocksText(ev.Chunk.Content))
			}
		case EventCompleted:
			completed = ev.Message
		}
	}
	return deltas, completed, nil
}

func TestSingleTurnStreaming(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"Task", "Smith", " v2", " ready."}, "msg-assistant-1", nil
		},
	}

	h, err := New(Config{
		DB:     db,
		Model:  newTestModel(t, prov),
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
		if m.Role() != message.RoleAssistant {
			continue
		}
		u, err := uuid.Parse(m.GetID())
		if err != nil || u.Version() != 7 {
			t.Errorf("state.Messages[%d].GetID() = %q, want valid UUIDv7 (err=%v)", i, m.GetID(), err)
		}
	}
}

func TestSystemPromptIsolation(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)

	prov := &fakeProvider{
		handler: func(_ context.Context, _ *llm.Request) ([]string, string, error) {
			return []string{"Understood."}, "resp-sys-1", nil
		},
	}

	sysPrompt := "You are TaskSmith v2.\n\nFollow strict Go conventions."
	h, err := New(Config{
		DB:           db,
		Model:        newTestModel(t, prov),
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

	prov := &fakeProvider{
		handler: func(_ context.Context, req *llm.Request) ([]string, string, error) {
			lastMsg := req.Messages[len(req.Messages)-1]
			return []string{"Echo: " + msgText(lastMsg)}, "", nil
		},
	}

	h, err := New(Config{
		DB:     db,
		Model:  newTestModel(t, prov),
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

func TestValidationAndContextCancellation(t *testing.T) {
	ctx := context.Background()
	db := openTestSQLite(t)
	model := newTestModel(t, nil)
	logger := log.Default()

	// 1. Validate all 3 required Config fields
	if _, err := New(Config{Model: model, Logger: logger}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing DB error = %v, want %v", err, ErrInvalidConfig)
	}
	if _, err := New(Config{DB: db, Logger: logger}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing Model error = %v, want %v", err, ErrInvalidConfig)
	}
	if _, err := New(Config{DB: db, Model: model}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("missing Logger error = %v, want %v", err, ErrInvalidConfig)
	}

	h, err := New(Config{
		DB:     db,
		Model:  model,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	// 2. Empty threadID -> ErrEmptyThreadID on Stream and LoadState; nil input -> ErrNilMessage
	if _, _, err := drainStream(ctx, h, "", message.NewUserText("hi")); !errors.Is(err, ErrEmptyThreadID) {
		t.Errorf("Stream empty threadID error = %v, want %v", err, ErrEmptyThreadID)
	}
	if _, _, err := drainStream(ctx, h, "   ", message.NewUserText("hi")); !errors.Is(err, ErrEmptyThreadID) {
		t.Errorf("Stream whitespace threadID error = %v, want %v", err, ErrEmptyThreadID)
	}
	if _, _, err := drainStream(ctx, h, "thread-valid", nil); !errors.Is(err, ErrNilMessage) {
		t.Errorf("Stream nil message error = %v, want %v", err, ErrNilMessage)
	}
	if _, err := h.LoadState(ctx, ""); !errors.Is(err, ErrEmptyThreadID) {
		t.Errorf("LoadState empty threadID error = %v, want %v", err, ErrEmptyThreadID)
	}

	// 3. Context cancellation before Stream
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := drainStream(cancelCtx, h, "thread-cancel", message.NewUserText("hi")); !errors.Is(err, context.Canceled) {
		t.Errorf("Stream with canceled ctx error = %v, want %v", err, context.Canceled)
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
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("New hCancel error: %v", err)
	}
	if _, _, err := drainStream(streamCancelCtx, hCancel, "thread-stream-cancel", message.NewUserText("hi")); err == nil {
		t.Errorf("expected error when context is canceled during streaming, got nil")
	}
}
