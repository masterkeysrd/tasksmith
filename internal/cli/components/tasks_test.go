package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func TestTasksComponent(t *testing.T) {
	store.ResetSession()
	store.ClearSlot()
	defer store.ResetSession()
	defer store.ClearSlot()

	render := renderComponent(t, 100)

	// When 0 running tasks, component renders nothing
	got := render(Tasks())
	if got != "" {
		t.Fatalf("expected empty string when no tasks running, got %q", got)
	}

	// When 1 running task
	store.UpsertTask(api.Task{
		ID:      "t-1",
		Command: "sleep 10",
		Status:  api.TaskStatusRunning,
	})

	got = render(Tasks())
	if !strings.Contains(got, "●") || !strings.Contains(got, "sleep 10") || !strings.Contains(got, "running") {
		t.Fatalf("expected dot, sleep 10, and running status, got %q", got)
	}

	// When 2 running tasks (one with Title and Command)
	store.UpsertTask(api.Task{
		ID:      "t-2",
		Title:   "Run Unit Tests",
		Command: "go test ./...",
		Status:  api.TaskStatusPending,
	})

	got = render(Tasks())
	if !strings.Contains(got, "sleep 10") || !strings.Contains(got, "Run Unit Tests · go test ./...") || !strings.Contains(got, "pending") {
		t.Fatalf("expected both tasks in list, got %q", got)
	}

	// When a slot is active, Tasks hides to avoid clutter
	store.EnqueueSlot(store.SlotItem{Kind: store.SlotKindPicker})
	got = render(Tasks())
	if got != "" {
		t.Fatalf("expected empty string when slot is active, got %q", got)
	}
	store.ClearSlot()

	// When tasks are completed, indicator disappears
	store.UpsertTask(api.Task{
		ID:      "t-1",
		Command: "sleep 10",
		Status:  api.TaskStatusCompleted,
	})
	store.UpsertTask(api.Task{
		ID:      "t-2",
		Command: "go test ./...",
		Status:  api.TaskStatusFailed,
	})

	got = render(Tasks())
	if got != "" {
		t.Fatalf("expected empty string when tasks are finished, got %q", got)
	}
}
