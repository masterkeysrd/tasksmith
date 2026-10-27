package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

type todosTool struct{}

func (t *todosTool) Prepare(_ context.Context, in api.TodosInput) (*tools.Plan, error) {
	for i, item := range in.Todos {
		if strings.TrimSpace(item.Content) == "" {
			return nil, fmt.Errorf("%w: todo item %d content is required", std.ErrInvalidArgument, i)
		}
		status := tools.TodoStatus(strings.TrimSpace(item.Status))
		if !status.IsValid() {
			return nil, fmt.Errorf("%w: todo item %d has invalid status %q", std.ErrInvalidArgument, i, item.Status)
		}
	}

	summary := fmt.Sprintf("Update task checklist (%d items)", len(in.Todos))
	return &tools.Plan{
		ToolName: ToolTodos,
		Action:   tools.ActionRead,
		Safety:   tools.SafetySafe,
		Summary:  summary,
		ReadOnly: true,
	}, nil
}

func (t *todosTool) Execute(_ context.Context, in api.TodosInput) (api.TodosOutput, error) {
	var completed, inProgress, pending int
	normalized := make([]api.TodoItem, len(in.Todos))

	for i, item := range in.Todos {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			return api.TodosOutput{}, fmt.Errorf("%w: todo item %d content is required", std.ErrInvalidArgument, i)
		}
		statusStr := strings.TrimSpace(item.Status)
		status := tools.TodoStatus(statusStr)
		if !status.IsValid() {
			return api.TodosOutput{}, fmt.Errorf("%w: todo item %d has invalid status %q", std.ErrInvalidArgument, i, item.Status)
		}

		switch status {
		case tools.TodoStatusCompleted:
			completed++
		case tools.TodoStatusInProgress:
			inProgress++
		case tools.TodoStatusPending:
			pending++
		}

		normalized[i] = api.TodoItem{
			Content:    content,
			ActiveText: strings.TrimSpace(item.ActiveText),
			Status:     string(status),
		}
	}

	msg := fmt.Sprintf("Checklist updated: %d total (%d completed, %d in progress, %d pending).", len(normalized), completed, inProgress, pending)

	return api.TodosOutput{
		Todos:      normalized,
		Total:      len(normalized),
		Completed:  completed,
		InProgress: inProgress,
		Pending:    pending,
		Message:    msg,
	}, nil
}
