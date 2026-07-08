package tools

import (
	"context"
)

// AskQuestion asks the user a question.
func (h *ToolHandlers) AskQuestion(ctx context.Context, in AskQuestionArgs) (AskQuestionOutput, error) {
	return AskQuestionOutput{Success: false}, nil
}

// Schedule schedules a timer or cron task.
func (h *ToolHandlers) Schedule(ctx context.Context, in ScheduleArgs) (ScheduleOutput, error) {
	return ScheduleOutput{Success: false, Error: "not implemented"}, nil
}
