package tools

import (
	"context"
	"fmt"
	"strings"
)

// Validate checks the task list for correct formatting and status values.
func (in TodosArgs) Validate() error {
	for i, t := range in.Todos {
		if t.Description == "" {
			return fmt.Errorf("todos: task at index %d has empty description", i)
		}
		if t.Status != "pending" && t.Status != "in_progress" && t.Status != "completed" {
			return fmt.Errorf("todos: task at index %d has invalid status %q: must be one of pending, in_progress, completed", i, t.Status)
		}
	}
	return nil
}

// Todos handles validation and updates of the authoritative task list.
func (h *ToolHandlers) Todos(ctx context.Context, in TodosArgs) (TodosOutput, error) {
	if err := in.Validate(); err != nil {
		return TodosOutput{}, err
	}

	var outputTodos []TodosOutputTodosItem
	for _, t := range in.Todos {
		outputTodos = append(outputTodos, TodosOutputTodosItem{
			Description: t.Description,
			Status:      t.Status,
			ActiveText:  t.ActiveText,
		})
	}

	return TodosOutput{
		Todos: outputTodos,
	}, nil
}

// TextContent returns a concise summary of the updated task list to save context window tokens.
func (o TodosOutput) TextContent() string {
	pendings := 0
	inProgress := 0
	completed := 0

	for _, t := range o.Todos {
		switch t.Status {
		case "pending":
			pendings++
		case "in_progress":
			inProgress++
		case "completed":
			completed++
		}
	}

	var parts []string
	if pendings > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", pendings))
	}
	if inProgress > 0 {
		parts = append(parts, fmt.Sprintf("%d in progress", inProgress))
	}
	if completed > 0 {
		parts = append(parts, fmt.Sprintf("%d completed", completed))
	}

	if len(parts) == 0 {
		return "Success: Updated task list with 0 tasks."
	}
	return fmt.Sprintf("Success: Updated task list with %s.", strings.Join(parts, ", "))
}
