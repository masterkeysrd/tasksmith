package timeline

import (
	"reflect"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

func TestQuestionRecordRoundTrip(t *testing.T) {
	tl, _, ctx := openTestTimeline(t, "questions")
	record := QuestionRecord{Request: tools.QuestionRequest{ID: "req", ToolCallID: "call", Questions: []api.Question{{Type: "text", Question: "Why?", Description: "Details"}}}, Response: tools.QuestionResponse{RequestID: "req", ToolCallID: "call", Answers: []tools.QuestionAnswer{{Question: "Why?", Text: "Because"}}}}
	entry := NewQuestionEntry("questions", record)
	if err := tl.Append(ctx, entry); err != nil {
		t.Fatal(err)
	}
	got, err := tl.Get(ctx, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindQuestion || !reflect.DeepEqual(got.Question, &record) {
		t.Fatalf("unexpected question record: %+v", got.Question)
	}
	clone := got.Clone()
	clone.Question.Request.Questions[0].Question = "Changed"
	if got.Question.Request.Questions[0].Question != "Why?" {
		t.Fatal("clone aliases original")
	}
}
