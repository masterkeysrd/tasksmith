package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

// askQuestionTool implements the ask_question tool for structured user questions.
type askQuestionTool struct{}

var _ tools.Handler[api.AskQuestionInput, api.AskQuestionOutput] = (*askQuestionTool)(nil)

func (t *askQuestionTool) Prepare(ctx context.Context, in api.AskQuestionInput) (*tools.Plan, error) {
	if err := validateQuestions(in.Questions); err != nil {
		return nil, err
	}

	target := strings.TrimSpace(in.Title)
	if target == "" && len(in.Questions) > 0 {
		target = strings.TrimSpace(in.Questions[0].Question)
	}

	return &tools.Plan{
		ToolName: ToolAskQuestion,
		Action:   tools.ActionRead,
		Safety:   tools.SafetySafe,
		Target:   target,
		Summary:  fmt.Sprintf("Ask user (%d question(s))", len(in.Questions)),
		ReadOnly: true,
	}, nil
}

func (t *askQuestionTool) Execute(ctx context.Context, in api.AskQuestionInput) (api.AskQuestionOutput, error) {
	// ask_question is fulfilled via the harness interrupt/resume flow in ToolsNode.
	// If Execute is invoked directly without an answer, return an empty output.
	return api.AskQuestionOutput{}, nil
}

func validateQuestions(questions []api.Question) error {
	if len(questions) < 1 || len(questions) > 5 {
		return fmt.Errorf("%w: must have 1 to 5 questions, got %d", tools.ErrInvalidConfig, len(questions))
	}

	for i, q := range questions {
		if err := validateQuestion(q); err != nil {
			return fmt.Errorf("%w: question %d: %w", tools.ErrInvalidConfig, i, err)
		}
	}
	return nil
}

func validateQuestion(q api.Question) error {
	switch api.QuestionType(q.Type) {
	case api.QuestionTypeSingle, api.QuestionTypeMulti, api.QuestionTypeText, api.QuestionTypeConfirm:
		// valid
	default:
		return fmt.Errorf("invalid question type %q", q.Type)
	}

	if strings.TrimSpace(q.Question) == "" {
		return fmt.Errorf("question text is required")
	}

	desc := strings.TrimSpace(q.Description)
	if desc == "" {
		return fmt.Errorf("description is required")
	}
	if len([]rune(q.Description)) > 300 {
		return fmt.Errorf("description must be at most 300 runes")
	}

	switch api.QuestionType(q.Type) {
	case api.QuestionTypeSingle, api.QuestionTypeMulti:
		if len(q.Options) < 2 || len(q.Options) > 5 {
			return fmt.Errorf("must have 2 to 5 options, got %d", len(q.Options))
		}
		seenIDs := make(map[string]bool)
		for j, opt := range q.Options {
			if strings.TrimSpace(opt.ID) == "" {
				return fmt.Errorf("option %d: id is required", j)
			}
			if strings.TrimSpace(opt.Label) == "" {
				return fmt.Errorf("option %d: label is required", j)
			}
			if seenIDs[opt.ID] {
				return fmt.Errorf("option %d: duplicate id %q", j, opt.ID)
			}
			seenIDs[opt.ID] = true
			if len([]rune(opt.Description)) > 100 {
				return fmt.Errorf("option %d: description must be at most 100 runes", j)
			}
		}
	case api.QuestionTypeText, api.QuestionTypeConfirm:
		if len(q.Options) > 0 {
			return fmt.Errorf("must not have options for %q questions", q.Type)
		}
	}
	return nil
}
