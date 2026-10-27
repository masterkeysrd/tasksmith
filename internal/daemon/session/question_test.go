package session

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

func TestQuestionSessionResume(t *testing.T) {
	ctx := context.Background()
	step := 0
	provider := &fakeProvider{chunkHandler: func(_ context.Context, req *llm.Request) ([]message.AssistantChunk, error) {
		step++
		if step == 1 {
			return []message.AssistantChunk{{BaseChunk: message.BaseChunk{ID: uuid.NewString()}, Model: req.Model, Content: []message.Block{&message.ToolCall{ID: "ask", Name: "ask_question", Args: map[string]any{"questions": []any{map[string]any{"type": "text", "question": "Why?", "description": "Give a reason."}}}}}, Done: true}}, nil
		}
		return []message.AssistantChunk{{BaseChunk: message.BaseChunk{ID: uuid.NewString()}, Model: req.Model, Content: []message.Block{&message.TextBlock{Text: "finished"}}, Done: true}}, nil
	}}
	_, _, _, pub, mgr := setupTestEnv(t, provider)
	sess, _, err := mgr.Create(ctx, CreateParams{Title: "Questions"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Send(ctx, SendInput{Message: message.NewUserText("start")}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if sess.Status != StatusWaitingQuestion || len(sess.PendingQuestions) != 1 {
		t.Fatalf("unexpected state: %+v", sess.State)
	}
	var requested bool
	for _, event := range pub.recorded() {
		if _, ok := event.(QuestionRequestedEvent); ok {
			requested = true
		}
	}
	if !requested {
		t.Fatal("missing question event")
	}
	req := sess.PendingQuestions[0]
	if err := sess.Send(ctx, SendInput{Question: &tools.QuestionResponse{RequestID: req.ID, Answers: []tools.QuestionAnswer{{Question: "Why?", Text: "Because"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	entries, err := sess.Timeline.ListByKind(ctx, timeline.KindQuestion)
	if err != nil || len(entries) != 1 || entries[0].Question.Response.Answers[0].Text != "Because" {
		t.Fatalf("question records: %+v, %v", entries, err)
	}
	if step != 2 {
		t.Fatalf("expected resumed model, got %d turns", step)
	}
}
