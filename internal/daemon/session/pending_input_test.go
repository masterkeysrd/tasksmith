package session

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/harness"
)

func TestPendingInteractionDefersQueuedWork(t *testing.T) {
	for _, kind := range []string{"permission", "question", "mixed"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			step := 0
			provider := &fakeProvider{chunkHandler: func(_ context.Context, req *llm.Request) ([]message.AssistantChunk, error) {
				step++
				var blocks []message.Block
				if step == 1 {
					if kind != "question" {
						blocks = append(blocks, &message.ToolCall{ID: "write", Name: "write", Args: map[string]any{"path": "foo.txt", "content": "bar"}})
					}
					if kind != "permission" {
						blocks = append(blocks, &message.ToolCall{ID: "ask", Name: "ask_question", Args: map[string]any{"questions": []any{map[string]any{"type": "text", "question": "Why?", "description": "Explain."}}}})
					}
				} else {
					blocks = append(blocks, &message.TextBlock{Text: "finished"})
				}
				return []message.AssistantChunk{{BaseChunk: message.BaseChunk{ID: uuid.NewString()}, Model: req.Model, Content: blocks, Done: true}}, nil
			}}
			_, _, _, _, mgr := setupTestEnv(t, provider)
			defer mgr.Close()
			sess, _, err := mgr.Create(ctx, CreateParams{Title: "Pending"})
			if err != nil {
				t.Fatal(err)
			}
			if err := sess.Send(ctx, SendInput{Message: message.NewUserText("start")}); err != nil {
				t.Fatal(err)
			}
			if err := sess.Wait(ctx); err != nil {
				t.Fatal(err)
			}
			if (len(sess.PendingPermissions) == 1) != (kind != "question") || (len(sess.PendingQuestions) == 1) != (kind != "permission") {
				t.Fatalf("unexpected pending state: %+v", sess.State)
			}
			h, err := sess.harness(ctx)
			if err != nil {
				t.Fatal(err)
			}
			before, err := h.LoadState(ctx)
			if err != nil {
				t.Fatal(err)
			}
			status := sess.Status
			if err := sess.Send(ctx, SendInput{Compact: &api.CompactRequest{}}); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("compact: %v", err)
			}
			if err := sess.Send(ctx, SendInput{Command: "printf queued"}); err != nil {
				t.Fatal(err)
			}
			sess.Enqueue(message.NewUserTextMeta("background completed", map[string]any{harness.MetaSystemKind: harness.SystemKindTask}))
			after, err := h.LoadState(ctx)
			if err != nil || !reflect.DeepEqual(before, after) || sess.Status != status || sess.Running {
				t.Fatalf("waiting state changed: state=%+v status=%s running=%v err=%v", after, sess.Status, sess.Running, err)
			}
			if len(sess.VisibleQueue()) != 1 || len(sess.queue) != 2 {
				t.Fatalf("queued work missing: %+v", sess.queue)
			}
			if kind != "question" {
				if err := sess.Send(ctx, SendInput{Decision: &api.PermissionDecision{RequestID: sess.PendingPermissions[0].ID, Kind: api.DecisionReject}}); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "mixed" {
				if !sess.WaitingQuestion || sess.Running || len(sess.queue) != 2 {
					t.Fatalf("partial resolution changed waiting state: %+v", sess.State)
				}
				if err := sess.Send(ctx, SendInput{Compact: &api.CompactRequest{}}); !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("compact after partial response: %v", err)
				}
			}
			if kind != "permission" {
				if err := sess.Send(ctx, SendInput{Question: &api.QuestionResponse{RequestID: sess.PendingQuestions[0].ID, Answers: []api.QuestionAnswer{{Question: "Why?", Text: "Because"}}}}); err != nil {
					t.Fatal(err)
				}
			}
			if err := sess.Wait(ctx); err != nil {
				t.Fatal(err)
			}
			if sess.Status != StatusIdle || sess.WaitingPermission || sess.WaitingQuestion || len(sess.queue) != 0 {
				t.Fatalf("work not completed: state=%+v queue=%+v", sess.State, sess.queue)
			}
			entries, err := sess.Timeline.ListByKind(ctx, api.TimelineKindCommand)
			if err != nil || len(entries) != 1 {
				t.Fatalf("command entries: %+v, %v", entries, err)
			}
		})
	}
}

func TestWaitingStatusProtectsResponseHandoff(t *testing.T) {
	for _, status := range []Status{StatusWaitingPermission, StatusWaitingQuestion} {
		s := &Session{State: &State{Status: status}}
		if err := s.startTurn(harness.Input{Compact: &harness.CompactInput{}}); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("status %s: compact error %v", status, err)
		}
		if err := s.startTurn(harness.Input{}); err != nil || s.Running || s.Status != status {
			t.Fatalf("status %s: empty turn error %v, state %+v", status, err, s.State)
		}
	}
}
