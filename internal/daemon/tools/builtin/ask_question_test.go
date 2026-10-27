package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestAskQuestionValidation(t *testing.T) {
	options := []api.QuestionOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}
	valid := api.Question{Type: "single", Question: "Choose?", Description: "Choose an option.", Options: options}
	for _, kind := range []string{"single", "multi", "text", "confirm"} {
		q := valid
		q.Type = kind
		if kind == "text" || kind == "confirm" {
			q.Options = nil
		}
		if _, err := (&askQuestionTool{}).Prepare(context.Background(), api.AskQuestionInput{Questions: []api.Question{q}}); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
	cases := map[string][]api.Question{
		"empty":               nil,
		"too many":            {valid, valid, valid, valid, valid, valid},
		"missing description": {{Type: "text", Question: "Why?"}},
		"long description":    {{Type: "text", Question: "Why?", Description: strings.Repeat("é", 301)}},
		"too few options":     {{Type: "single", Question: "Why?", Description: "Details", Options: options[:1]}},
		"too many options":    {{Type: "multi", Question: "Why?", Description: "Details", Options: []api.QuestionOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}, {ID: "c", Label: "C"}, {ID: "d", Label: "D"}, {ID: "e", Label: "E"}, {ID: "f", Label: "F"}}}},
		"duplicate":           {{Type: "single", Question: "Why?", Description: "Details", Options: []api.QuestionOption{{ID: "a", Label: "A"}, {ID: "a", Label: "B"}}}},
		"long option":         {{Type: "single", Question: "Why?", Description: "Details", Options: []api.QuestionOption{{ID: "a", Label: "A", Description: strings.Repeat("é", 101)}, {ID: "b", Label: "B"}}}},
		"confirm options":     {{Type: "confirm", Question: "Why?", Description: "Details", Options: options}},
		"invalid type":        {{Type: "other", Question: "Why?", Description: "Details"}},
	}
	for name, questions := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := (&askQuestionTool{}).Prepare(context.Background(), api.AskQuestionInput{Questions: questions}); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
