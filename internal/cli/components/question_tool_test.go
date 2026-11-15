package components

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func TestQuestionToolWidget(t *testing.T) {
	rawRender := renderComponent(t, 100)
	render := func(node trail.Node) string {
		lines := strings.Split(rawRender(node), "\n")
		for i := range lines {
			lines[i] = strings.TrimRight(lines[i], " ")
		}
		return strings.Join(lines, "\n")
	}
	tool := store.ToolPart{Status: store.ToolRunning, Call: &api.ToolCall{Name: "ask_question", Args: map[string]any{
		"questions": []any{
			map[string]any{"type": "single", "question": "Project?", "options": []any{map[string]any{"id": "web", "label": "Web app"}}},
			map[string]any{"type": "multi", "question": "Languages?", "options": []any{map[string]any{"id": "ts", "label": "TypeScript"}}},
		},
	}}}
	waiting := render(MessageTool(MessageToolProps{Tool: tool}))
	if waiting != "Asking questions:\n  Project?\n  Languages?" {
		t.Fatalf("waiting widget = %q", waiting)
	}
	single := store.ToolPart{Status: store.ToolPending, Call: &api.ToolCall{Name: "ask_question", Args: map[string]any{"questions": []any{map[string]any{"type": "text", "question": "Why?"}}}}}
	if got := render(MessageTool(MessageToolProps{Tool: single})); got != "Asking question:\n  Why?" {
		t.Fatalf("single waiting widget = %q", got)
	}
	tool.Response = &api.Message{StructuredContent: json.RawMessage(`{"answers":[{"question":"Project?","selected":["web"]},{"question":"Languages?","selected":["ts"],"text":"Python"}]}`)}
	tool.Status = store.ToolSucceeded
	answered := render(MessageTool(MessageToolProps{Tool: tool}))
	if answered != "Questions asked:\n  Project?\n    > Web app\n  Languages?\n    > TypeScript, Python" {
		t.Fatalf("answered widget = %q", answered)
	}
	tool.Question = &api.QuestionRecord{Request: api.QuestionRequest{Questions: []api.Question{{Type: "confirm", Question: "Proceed?"}}}, Response: api.QuestionResponse{Answers: []api.QuestionAnswer{{Question: "Proceed?", Confirmed: new(false)}}}}
	got := render(MessageTool(MessageToolProps{Tool: tool}))
	if got != "Question asked:\n  Proceed?\n    > No" {
		t.Fatalf("recorded answer widget = %q", got)
	}
	tool.Question.Response.Canceled = true
	if got = render(MessageTool(MessageToolProps{Tool: tool})); got != "Question asked:\n  Proceed?\n    > Canceled" {
		t.Fatalf("canceled widget = %q", got)
	}
	tool.Question = nil
	tool.Response = &api.Message{IsError: true, Content: []api.Block{{Text: "invalid questions"}}}
	if got = render(MessageTool(MessageToolProps{Tool: tool})); !strings.Contains(got, "Unable to ask questions:") || !strings.Contains(got, "invalid questions") {
		t.Fatalf("error widget = %q", got)
	}
}

func TestQuestionToolColors(t *testing.T) {
	b := mock.New(80, 20)
	tool := store.ToolPart{Call: &api.ToolCall{Name: "ask_question"}, Question: &api.QuestionRecord{
		Request:  api.QuestionRequest{Questions: []api.Question{{Type: "text", Question: "Why?"}}},
		Response: api.QuestionResponse{Answers: []api.QuestionAnswer{{Question: "Why?", Text: "Because"}}},
	}}
	app := trail.SimpleFC("QuestionToolTest", func() trail.Node { return MessageTool(MessageToolProps{Tool: tool}) })
	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Unmount()
	inst.Frame()
	question, _ := questionCell(t, b, "Why?")
	answer, _ := questionCell(t, b, "Because")
	scheme := theme.Get()
	if question.Fg != scheme.Color.Text.Secondary || answer.Fg != scheme.Color.Text.Primary || answer.Style&backend.CellBold == 0 {
		t.Fatalf("unexpected colors: question=%+v answer=%+v", question, answer)
	}
}
