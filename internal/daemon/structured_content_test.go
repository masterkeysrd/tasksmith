package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
)

func TestStructuredContentConversionFailure(t *testing.T) {
	tool := &message.Tool{
		Base: message.Base{ID: "result"}, ToolCallID: "call",
		StructuredContent: make(chan int),
	}
	converted, err := toAPIMessage(tool)
	var unsupported *json.UnsupportedTypeError
	if converted != nil || !errors.As(err, &unsupported) {
		t.Fatalf("toAPIMessage = (%+v, %v), want an unsupported type error", converted, err)
	}
	event := toAPIEvent("/workspace", session.Event{
		Type: session.EventMessageAdded, SessionID: "session", Message: tool,
	})
	if event.Type != api.EventError || event.Workspace != "/workspace" || event.SessionID != "session" ||
		event.Message != nil || !strings.Contains(event.Error, "marshal structured content") {
		t.Fatalf("conversion failure was not reported: %+v", event)
	}
}

func TestToolStructuredContentInEventsAndHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var step int
	provider := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
			step++
			var content message.Content
			if step == 1 {
				content = message.Content{&message.ToolCall{
					ID: "view-call", Name: "view",
					Args: map[string]any{"path": "source.go", "offset": 2, "limit": 1},
				}}
			} else {
				content = message.Content{&message.TextBlock{Text: "Read complete."}}
			}
			return func(yield func(message.AssistantChunk, error) bool) {
				yield(message.AssistantChunk{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model, Content: content, Done: true,
				}, nil)
			}, nil
		},
	}
	_, cli, workspace := setupDaemonTest(t, provider)
	if err := os.WriteFile(filepath.Join(workspace, "source.go"), []byte("first\nsecond\nthird\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	results := make(chan api.Message, 1)
	subErrors := make(chan error, 1)
	go func() {
		for event, err := range cli.SubscribeWithReady(ctx, sess.ID, func() { close(ready) }) {
			if err != nil {
				subErrors <- err
				return
			}
			if event.Type == api.EventError {
				subErrors <- errors.New(event.Error)
				return
			}
			if event.Type == api.EventMessageAdded && event.Message != nil && event.Message.Role == api.RoleTool {
				results <- *event.Message
				return
			}
		}
	}()
	select {
	case <-ready:
	case err := <-subErrors:
		t.Fatalf("subscribe: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := cli.Send(ctx, sess.ID, api.SendRequest{Content: "Read source.go.", Wait: true}); err != nil {
		t.Fatal(err)
	}
	var live api.Message
	select {
	case live = <-results:
	case err := <-subErrors:
		t.Fatalf("subscribe: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if live.IsError {
		t.Fatalf("view failed: %+v", live)
	}
	var liveOutput api.ViewOutput
	if err := json.Unmarshal(live.StructuredContent, &liveOutput); err != nil {
		t.Fatalf("decode live structured content: %v", err)
	}
	if liveOutput.Path == "" || liveOutput.StartLine != 2 || liveOutput.EndLine != 2 || liveOutput.Content != "second" {
		t.Fatalf("live output = %+v", liveOutput)
	}
	if len(live.Content) != 0 {
		t.Fatalf("expected tool content text block to be omitted when structured content is returned: %+v", live.Content)
	}
	history, err := cli.History(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, persisted := range history {
		if persisted.ID != live.ID {
			continue
		}
		var output api.ViewOutput
		if err := json.Unmarshal(persisted.StructuredContent, &output); err != nil {
			t.Fatalf("decode persisted structured content: %v", err)
		}
		if output != liveOutput {
			t.Fatalf("persisted output = %+v, want %+v", output, liveOutput)
		}
		return
	}
	t.Fatalf("tool result %q missing from history", live.ID)
}
