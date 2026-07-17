package tools

import (
	"context"
)

// PendingQuestion represents a question that needs to be answered by the user.
type PendingQuestion struct {
	ToolCallID    string   `json:"tool_call_id"`
	Question      string   `json:"question"`
	Options       []string `json:"options"`
	IsMultiSelect bool     `json:"is_multi_select"`
}

// QuestionAnswer represents the user's answer to a pending question.
type QuestionAnswer struct {
	ToolCallID string   `json:"tool_call_id"`
	Selected   []string `json:"selected,omitempty"`
	WriteIn    string   `json:"write_in,omitempty"`
}

// AskQuestion asks the user a question.
func (h *ToolHandlers) AskQuestion(ctx context.Context, in AskQuestionArgs) (AskQuestionOutput, error) {
	return AskQuestionOutput{Success: false}, nil
}

// Schedule schedules a timer or cron task.
func (h *ToolHandlers) Schedule(ctx context.Context, in ScheduleArgs) (ScheduleOutput, error) {
	return ScheduleOutput{Success: false, Error: "not implemented"}, nil
}
