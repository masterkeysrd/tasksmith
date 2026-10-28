package harness

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

func TestPendingInteractionBlocksCompactionAndQueue(t *testing.T) {
	for _, kind := range []string{"permission", "question", "mixed"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			_, registry, perms := newTestToolsRegistry(t)
			queue := &fakeQueue{}
			provider := &fakeProvider{stepHandler: func(_ context.Context, _ *llm.Request, turn int) ([]message.AssistantChunk, error) {
				if turn != 1 {
					return []message.AssistantChunk{{Content: []message.Block{&message.TextBlock{Text: "done"}}, Done: true}}, nil
				}
				var blocks []message.Block
				if kind != "question" {
					blocks = append(blocks, &message.ToolCall{ID: "write", Name: "write", Args: map[string]any{"path": "file.txt", "content": "text"}})
				}
				if kind != "permission" {
					blocks = append(blocks, &message.ToolCall{ID: "ask", Name: "ask_question", Args: map[string]any{"questions": []any{map[string]any{"type": "text", "question": "Why?", "description": "Explain."}}}})
				}
				return []message.AssistantChunk{{Content: blocks, Done: true}}, nil
			}}
			h, err := New(Config{ID: "pending-" + kind, DB: openTestSQLite(t), Model: newTestModel(t, provider), Tools: registry, Bash: &fakeBashRunner{}, Spiller: newTestSpiller(t), Permissions: perms, Queue: queue, Logger: log.Default()})
			if err != nil {
				t.Fatal(err)
			}
			for _, err := range h.Stream(ctx, Input{Message: message.NewUserText("start")}) {
				if err != nil {
					t.Fatal(err)
				}
			}
			impl := h.(*harness)
			before, err := impl.graph.Load(ctx, graph.Location{ThreadID: impl.id})
			if err != nil || before == nil || before.IsDone() {
				t.Fatalf("expected interrupted checkpoint: %v %v", before, err)
			}
			want := ErrPendingPermissionsUnresolved
			if kind == "question" {
				want = ErrPendingQuestionsUnresolved
			}
			for _, input := range []Input{{Compact: &CompactInput{Focus: "focus"}}, {}} {
				var got error
				for _, err := range h.Stream(ctx, input) {
					got = err
				}
				if !errors.Is(got, want) {
					t.Fatalf("input %+v: got %v, want %v", input, got, want)
				}
				after, err := impl.graph.Load(ctx, graph.Location{ThreadID: impl.id})
				if err != nil || after == nil || !reflect.DeepEqual(before, after) {
					t.Fatalf("checkpoint changed: before=%+v after=%+v err=%v", before, after, err)
				}
			}
			queue.items = append(queue.items, QueueItem{Kind: QueueKindCommand, Payload: QueueCommandPayload{Command: "show status"}})
			var got error
			for _, err := range h.Stream(ctx, Input{}) {
				got = err
			}
			if !errors.Is(got, want) || len(queue.items) != 1 {
				t.Fatalf("queued input: error=%v queue=%+v", got, queue.items)
			}
			state, err := h.LoadState(ctx)
			if err != nil {
				t.Fatal(err)
			}
			decisions := map[string]tools.PermissionDecision{}
			answers := map[string]tools.QuestionResponse{}
			for _, p := range state.PendingPermissions {
				decisions[p.ID] = tools.PermissionDecision{Kind: tools.DecisionReject}
			}
			for _, q := range state.PendingQuestions {
				answers[q.ID] = tools.QuestionResponse{Answers: []tools.QuestionAnswer{{Question: "Why?", Text: "Reason"}}}
			}
			for _, err := range h.Stream(ctx, Input{Decisions: decisions, Answers: answers}) {
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(queue.items) != 0 {
				t.Fatalf("queued command not processed after resume: %+v", queue.items)
			}
		})
	}
}
