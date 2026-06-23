package tools

import (
	"context"
	"testing"
)

func TestTodosValidationAndFormatting(t *testing.T) {
	handlers := NewHandlers(nil, "")

	// 1. Test validation error with invalid status
	_, err := handlers.Todos(context.Background(), TodosArgs{
		Todos: []TodosArgsTodosItem{
			{
				Description: "Valid task description",
				Status:      "invalid_status",
			},
		},
	})
	if err == nil {
		t.Error("expected error for invalid status, got nil")
	}

	// 2. Test validation error with empty description
	_, err = handlers.Todos(context.Background(), TodosArgs{
		Todos: []TodosArgsTodosItem{
			{
				Description: "",
				Status:      "pending",
			},
		},
	})
	if err == nil {
		t.Error("expected error for empty description, got nil")
	}

	// 3. Test successful validation and return values
	out, err := handlers.Todos(context.Background(), TodosArgs{
		Todos: []TodosArgsTodosItem{
			{
				Description: "Task 1",
				Status:      "pending",
			},
			{
				Description: "Task 2",
				Status:      "in_progress",
				ActiveText:  "working hard",
			},
			{
				Description: "Task 3",
				Status:      "completed",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(out.Todos) != 3 {
		t.Fatalf("expected 3 returned items, got %d", len(out.Todos))
	}

	if out.Todos[0].Description != "Task 1" || out.Todos[0].Status != "pending" {
		t.Errorf("incorrect fields at index 0: %+v", out.Todos[0])
	}
	if out.Todos[1].ActiveText != "working hard" {
		t.Errorf("incorrect ActiveText at index 1: %+v", out.Todos[1])
	}

	// 4. Test TextContent formatting
	expectedText := "Success: Updated task list with 1 pending, 1 in progress, 1 completed."
	if out.TextContent() != expectedText {
		t.Errorf("expected TextContent %q, got %q", expectedText, out.TextContent())
	}
}
