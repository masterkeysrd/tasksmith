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
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
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
			sess, _, err := mgr.Create(ctx, api.CreateSessionRequest{Title: "Pending"})
			if err != nil {
				t.Fatal(err)
			}
			if err := sess.Send(ctx, api.SendRequest{Content: "start"}); err != nil {
				t.Fatal(err)
			}
			if err := sess.Wait(ctx); err != nil {
				t.Fatal(err)
			}
			if (len(sess.PendingPermissions) == 1) != (kind != "question") || (len(sess.PendingQuestions) == 1) != (kind != "permission") {
				t.Fatalf("unexpected pending state: %+v", sess.State)
			}
			h := sess.Harness
			before, err := h.LoadState(ctx)
			if err != nil {
				t.Fatal(err)
			}
			status := sess.Status
			if err := sess.Send(ctx, api.SendRequest{Compact: &api.CompactRequest{}}); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("compact: %v", err)
			}
			if err := sess.Send(ctx, api.SendRequest{Command: "printf queued"}); err != nil {
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
				if err := sess.Send(ctx, api.SendRequest{Decision: &api.PermissionDecision{RequestID: sess.PendingPermissions[0].ID, Kind: api.DecisionReject}}); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "mixed" {
				if len(sess.PendingQuestions) != 1 || sess.Running || len(sess.queue) != 2 {
					t.Fatalf("partial resolution changed waiting state: %+v", sess.State)
				}
				if err := sess.Send(ctx, api.SendRequest{Compact: &api.CompactRequest{}}); !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("compact after partial response: %v", err)
				}
			}
			if kind != "permission" {
				if err := sess.Send(ctx, api.SendRequest{Question: &api.QuestionResponse{RequestID: sess.PendingQuestions[0].ID, Answers: []api.QuestionAnswer{{Question: "Why?", Text: "Because"}}}}); err != nil {
					t.Fatal(err)
				}
			}
			if err := sess.Wait(ctx); err != nil {
				t.Fatal(err)
			}
			if sess.Status != api.StatusIdle || len(sess.PendingPermissions) != 0 || len(sess.PendingQuestions) != 0 || len(sess.queue) != 0 {
				t.Fatalf("work not completed: state=%+v queue=%+v", sess.State, sess.queue)
			}
			allEntries, err := sess.Timeline.List(ctx)
			if err != nil {
				t.Fatalf("list entries: %v", err)
			}
			var entries []timeline.Entry
			for _, e := range allEntries {
				if e.Kind == api.TimelineKindCommand {
					entries = append(entries, e)
				}
			}
			if len(entries) != 1 {
				t.Fatalf("command entries: %+v", entries)
			}
		})
	}
}

func TestWaitingStatusProtectsResponseHandoff(t *testing.T) {
	for _, status := range []api.SessionStatus{api.StatusWaitingPermission, api.StatusWaitingQuestion} {
		s := &Session{State: &State{Status: status}}
		if err := s.startTurn(harness.Input{Compact: &api.CompactRequest{}}); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("status %s: compact error %v", status, err)
		}
		if err := s.startTurn(harness.Input{}); err != nil || s.Running || s.Status != status {
			t.Fatalf("status %s: empty turn error %v, state %+v", status, err, s.State)
		}
	}
}
