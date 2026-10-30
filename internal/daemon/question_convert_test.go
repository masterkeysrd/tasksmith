package daemon

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

func TestQuestionResponseConversionPreservesNo(t *testing.T) {
	questions := []api.Question{{Type: "text", Question: "Why?"}, {Type: "confirm", Question: "Proceed?"}}
	input := tools.QuestionResponse{Answers: []tools.QuestionAnswer{{Question: "Why?", Text: "Reason"}, {Question: "Proceed?", Confirmed: false}}}
	output := session.ToAPIQuestionResponse(input, questions)
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"text":"Reason","confirmed"`) || !strings.Contains(string(data), `"confirmed":false`) || output.Answers[0].Confirmed != nil || output.Answers[1].Confirmed == nil {
		t.Fatalf("unexpected encoded response: %s", data)
	}
	if restored := fromAPIQuestionResponse(output); restored.Answers[1].Confirmed {
		t.Fatalf("no answer changed: %+v", restored)
	}
}
