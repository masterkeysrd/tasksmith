package components

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func createQuestionInstance(t *testing.T, props QuestionPromptProps) (*trail.Instance, *mock.Backend) {
	t.Helper()
	b := mock.New(100, 35)
	app := trail.SimpleFC("QuestionTest", func() trail.Node { return QuestionPrompt(props) })
	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inst.Unmount)
	inst.Frame()
	return inst, b
}

func questionCell(t *testing.T, b *mock.Backend, text string) (backend.Cell, int) {
	t.Helper()
	frame := b.LastFrame().Surface
	for y := 0; y < frame.Height; y++ {
		var row strings.Builder
		var positions []int
		for x := 0; x < frame.Width; x++ {
			content := frame.CellAt(x, y).Content
			row.WriteString(content)
			for range []byte(content) {
				positions = append(positions, x)
			}
		}
		index := strings.Index(row.String(), text)
		if index >= 0 {
			return frame.CellAt(positions[index], y), y
		}
	}
	t.Fatalf("missing %q in surface:\n%s", text, surfaceText(b))
	return backend.Cell{}, 0
}

func TestQuestionPresentation(t *testing.T) {
	req := api.QuestionRequest{Questions: []api.Question{
		{Type: "multi", Label: "Providers", Question: "Which providers?", Description: "Select providers", Options: []api.QuestionOption{{ID: "github", Label: "GitHub"}, {ID: "google", Label: "Google"}}},
		{Type: "confirm", Label: "Ready", Question: "Proceed?", Description: "Confirm"},
	}}
	inst, b := createQuestionInstance(t, QuestionPromptProps{Request: req})
	scheme := theme.Get()
	title, _ := questionCell(t, b, "Question")
	active, tabY := questionCell(t, b, "[1 Providers]")
	inactive, _ := questionCell(t, b, "2 Ready")
	_, questionY := questionCell(t, b, "Which providers?")
	if title.Style&backend.CellBold == 0 || active.Style&backend.CellBold == 0 || active.Fg != scheme.Color.Icon.Primary || inactive.Fg != scheme.Color.Text.Secondary {
		t.Fatalf("unexpected title/tab styles: title=%+v active=%+v inactive=%+v", title, active, inactive)
	}
	if questionY-tabY < 2 {
		t.Fatalf("question needs space below tabs: tab=%d question=%d", tabY, questionY)
	}
	if !strings.Contains(surfaceText(b), "[ ] GitHub") {
		t.Fatalf("missing unselected checkbox: %s", surfaceText(b))
	}
	pressKey(inst, key.KeySpace, 0)
	if !strings.Contains(surfaceText(b), "[x] GitHub") {
		t.Fatalf("missing selected checkbox: %s", surfaceText(b))
	}
	pressKey(inst, key.KeyEnter, 0)
	activeNext, _ := questionCell(t, b, "[2 Ready]")
	inactivePrevious, _ := questionCell(t, b, "1 Providers")
	if activeNext.Fg != scheme.Color.Icon.Primary || activeNext.Style&backend.CellBold == 0 || inactivePrevious.Fg != scheme.Color.Text.Secondary {
		t.Fatalf("tab highlighting did not move: active=%+v inactive=%+v", activeNext, inactivePrevious)
	}
	pressKey(inst, key.KeyEnter, 0)
	review, reviewY := questionCell(t, b, "Review your answers")
	label, answerY := questionCell(t, b, "Providers:")
	answer, _ := questionCell(t, b, "GitHub")
	_, submitY := questionCell(t, b, "Submit answers")
	activeReview, _ := questionCell(t, b, "[Review]")
	if review.Style&backend.CellBold == 0 || activeReview.Style&backend.CellBold == 0 || activeReview.Fg != scheme.Color.Icon.Primary {
		t.Fatalf("review styles missing: review=%+v tab=%+v", review, activeReview)
	}
	if answer.Style&backend.CellBold == 0 || answer.Fg != scheme.Color.Text.Primary || label.Fg != scheme.Color.Text.Secondary {
		t.Fatalf("answer styles missing: label=%+v answer=%+v", label, answer)
	}
	if answerY-reviewY < 2 || submitY-answerY < 3 {
		t.Fatalf("review spacing missing: review=%d answer=%d submit=%d", reviewY, answerY, submitY)
	}
}

func TestQuestionMultiWriteInAndReview(t *testing.T) {
	req := api.QuestionRequest{Title: "Configure", Description: "Choose settings", Questions: []api.Question{
		{Type: "multi", Label: "Providers", Question: "Which providers?", Description: "Select providers", Options: []api.QuestionOption{{ID: "github", Label: "GitHub"}, {ID: "google", Label: "Google"}}},
		{Type: "confirm", Label: "Ready", Question: "Proceed?", Description: "Confirm"},
	}}
	var got api.QuestionResponse
	inst, b := createQuestionInstance(t, QuestionPromptProps{Request: req, OnSubmit: func(_ api.QuestionRequest, response api.QuestionResponse, done func(error)) { got = response }})
	if text := surfaceText(b); !strings.Contains(text, "Configure") || !strings.Contains(text, "Select providers") {
		t.Fatalf("missing question: %s", text)
	}
	pressKey(inst, key.KeySpace, 0)
	pressKey(inst, key.KeyDown, 0)
	pressKey(inst, key.KeyDown, 0)
	pressKey(inst, key.KeySpace, 0)
	typeText(inst, "GitLab")
	pressKey(inst, key.KeyEnter, 0)
	pressKey(inst, key.KeyEnter, 0)
	pressKey(inst, key.KeyDown, 0)
	pressKey(inst, key.KeyEnter, 0)
	if text := surfaceText(b); !strings.Contains(text, "Review your answers") || !strings.Contains(text, "GitHub, GitLab") || !strings.Contains(text, "No") {
		t.Fatalf("unexpected review: %s", text)
	}
	pressKey(inst, key.KeyEnter, 0)
	if len(got.Answers) != 2 || !reflect.DeepEqual(got.Answers[0].Selected, []string{"github"}) || got.Answers[0].Text != "GitLab" || got.Answers[1].Confirmed == nil || *got.Answers[1].Confirmed {
		t.Fatalf("unexpected answer: %+v", got)
	}
}

func TestQuestionSingleRetryAndCancel(t *testing.T) {
	req := api.QuestionRequest{Questions: []api.Question{{Type: "single", Question: "Choose?", Description: "Details", Options: []api.QuestionOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}}}}
	var responses []api.QuestionResponse
	inst, b := createQuestionInstance(t, QuestionPromptProps{Request: req, OnSubmit: func(_ api.QuestionRequest, response api.QuestionResponse, done func(error)) {
		responses = append(responses, response)
		done(errors.New("send failed"))
	}})
	pressKey(inst, key.KeyEnter, 0)
	if !strings.Contains(surfaceText(b), "send failed") {
		t.Fatalf("missing error: %s", surfaceText(b))
	}
	pressKey(inst, key.KeyEnter, 0)
	if len(responses) != 2 || responses[1].Answers[0].Selected[0] != "a" {
		t.Fatalf("retry lost answer: %+v", responses)
	}
	pressKey(inst, key.KeyEsc, 0)
	if !strings.Contains(surfaceText(b), "Cancel this question?") {
		t.Fatalf("missing cancellation confirmation: %s", surfaceText(b))
	}
	pressKey(inst, key.KeyEnter, 0)
	if len(responses) != 3 || !responses[2].Canceled {
		t.Fatalf("expected cancellation: %+v", responses)
	}
}

func TestQuestionSingleWriteIn(t *testing.T) {
	req := api.QuestionRequest{Questions: []api.Question{{Type: "single", Question: "Choose?", Description: "Details", Options: []api.QuestionOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}}}}
	var got api.QuestionResponse
	inst, _ := createQuestionInstance(t, QuestionPromptProps{Request: req, OnSubmit: func(_ api.QuestionRequest, response api.QuestionResponse, done func(error)) { got = response }})
	pressKey(inst, key.KeyDown, 0)
	pressKey(inst, key.KeyDown, 0)
	pressKey(inst, key.KeyEnter, 0)
	typeText(inst, "Custom")
	pressKey(inst, key.KeyEnter, 0)
	if len(got.Answers) != 1 || got.Answers[0].Text != "Custom" || len(got.Answers[0].Selected) != 0 {
		t.Fatalf("unexpected answer: %+v", got)
	}
}

func TestQuestionBackFromText(t *testing.T) {
	req := api.QuestionRequest{Questions: []api.Question{{Type: "confirm", Question: "Proceed?", Description: "Confirm"}, {Type: "text", Question: "Why?", Description: "Explain"}}}
	inst, b := createQuestionInstance(t, QuestionPromptProps{Request: req})
	pressKey(inst, key.KeyEnter, 0)
	typeText(inst, "draft")
	pressKey(inst, key.KeyTab, key.ModShift)
	if !strings.Contains(surfaceText(b), "Proceed?") {
		t.Fatalf("did not navigate back: %s", surfaceText(b))
	}
	pressKey(inst, key.KeyEnter, 0)
	if !strings.Contains(surfaceText(b), "draft") {
		t.Fatalf("lost draft: %s", surfaceText(b))
	}
}

func TestQuestionNavigationPreservesAnswers(t *testing.T) {
	req := api.QuestionRequest{Questions: []api.Question{
		{Type: "single", Question: "Choose?", Description: "Pick", Options: []api.QuestionOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}},
		{Type: "text", Question: "Why?", Description: "Explain"},
	}}
	var got api.QuestionResponse
	inst, b := createQuestionInstance(t, QuestionPromptProps{Request: req, OnSubmit: func(_ api.QuestionRequest, response api.QuestionResponse, _ func(error)) { got = response }})
	pressKey(inst, key.KeyTab, 0)
	if !strings.Contains(surfaceText(b), "Answer this question") {
		t.Fatalf("unanswered question skipped: %s", surfaceText(b))
	}
	pressKey(inst, key.KeyDown, 0)
	pressKey(inst, key.KeyEnter, 0)
	typeText(inst, "Reason")
	pressKey(inst, key.KeyTab, key.ModShift)
	pressKey(inst, key.KeyRight, 0)
	if !strings.Contains(surfaceText(b), "Reason") {
		t.Fatalf("text draft lost: %s", surfaceText(b))
	}
	pressKey(inst, key.KeyTab, 0)
	if !strings.Contains(surfaceText(b), "Review your answers") {
		t.Fatalf("tab did not advance: %s", surfaceText(b))
	}
	pressKey(inst, key.KeyLeft, 0)
	pressKey(inst, key.KeyTab, key.ModShift)
	pressKey(inst, key.KeyTab, 0)
	pressKey(inst, key.KeyTab, 0)
	pressKey(inst, key.KeyEnter, 0)
	if len(got.Answers) != 2 || !reflect.DeepEqual(got.Answers[0].Selected, []string{"b"}) || got.Answers[1].Text != "Reason" {
		t.Fatalf("navigation changed answers: %+v", got)
	}
}

func TestQuestionMultiOtherEnter(t *testing.T) {
	req := api.QuestionRequest{Questions: []api.Question{{Type: "multi", Question: "Choose?", Description: "Pick", Options: []api.QuestionOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}}}}
	var got api.QuestionResponse
	inst, b := createQuestionInstance(t, QuestionPromptProps{Request: req, OnSubmit: func(_ api.QuestionRequest, response api.QuestionResponse, _ func(error)) { got = response }})
	pressKey(inst, key.KeySpace, 0)
	pressKey(inst, key.KeyDown, 0)
	pressKey(inst, key.KeyDown, 0)
	pressKey(inst, key.KeyEnter, 0)
	if !strings.Contains(surfaceText(b), "Answer:") {
		t.Fatalf("custom input not opened: %s", surfaceText(b))
	}
	typeText(inst, "Custom")
	pressKey(inst, key.KeyEnter, 0)
	pressKey(inst, key.KeyEnter, 0)
	if len(got.Answers) != 1 || !reflect.DeepEqual(got.Answers[0].Selected, []string{"a"}) || got.Answers[0].Text != "Custom" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestQuestionTextAnswer(t *testing.T) {
	req := api.QuestionRequest{Questions: []api.Question{{Type: "text", Question: "Why?", Description: "Explain"}}}
	var got api.QuestionResponse
	inst, _ := createQuestionInstance(t, QuestionPromptProps{Request: req, OnSubmit: func(_ api.QuestionRequest, response api.QuestionResponse, done func(error)) { got = response }})
	typeText(inst, "A reason")
	pressKey(inst, key.KeyEnter, 0)
	if len(got.Answers) != 1 || got.Answers[0].Text != "A reason" {
		t.Fatalf("unexpected response: %+v", got)
	}
}
