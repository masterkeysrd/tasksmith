package daemon

import (
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

func TestToAPISessionWithTasks(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	sess := &session.Session{
		ID:    "sess-1",
		Title: "Test Session",
		Config: session.SessionConfig{
			Agent: "developer",
			Model: "mock-model",
		},
		CreatedAt: now,
		UpdatedAt: now,
		State: &session.State{
			Status: session.StatusThinking,
			Tasks: []tasks.Snapshot{
				{
					ID:           "task-1",
					Kind:         "bash",
					Title:        "go build",
					Command:      "go build ./...",
					Status:       tasks.StatusRunning,
					ExitCode:     0,
					LogPath:      "/tmp/task-1.log",
					StartedAt:    now,
					DurationMs:   250,
					Backgrounded: true,
				},
			},
		},
	}

	apiSess := toAPISession(sess)
	if apiSess.ID != "sess-1" || apiSess.Title != "Test Session" {
		t.Fatalf("unexpected API session ID or Title: %+v", apiSess)
	}
	if len(apiSess.Tasks) != 1 {
		t.Fatalf("expected 1 task in api.Session, got %d", len(apiSess.Tasks))
	}
	task := apiSess.Tasks[0]
	if task.ID != "task-1" || task.Kind != "bash" || task.Status != api.TaskStatusRunning || !task.Backgrounded {
		t.Fatalf("unexpected API task fields: %+v", task)
	}
}

func TestToAPIEventTaskUpdated(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	rawMsg := broker.Message{
		ID:    "evt-1",
		Topic: api.SessionStreamTopic("sess-1"),
		Event: session.TaskUpdatedEvent{
			SessionID: "sess-1",
			Task: tasks.Snapshot{
				ID:           "task-2",
				Kind:         "download",
				Title:        "fetch data",
				Status:       tasks.StatusCompleted,
				ExitCode:     0,
				LogPath:      "/tmp/task-2.log",
				StartedAt:    now,
				FinishedAt:   now.Add(time.Second),
				DurationMs:   1000,
				Backgrounded: true,
			},
		},
	}

	d := &Daemon{}
	apiEv, err := d.FormatEvent(rawMsg)
	if err != nil {
		t.Fatalf("toAPIEvent error: %v", err)
	}

	if apiEv.Type != api.EventTaskUpdated {
		t.Fatalf("apiEv.Type = %q, want %q", apiEv.Type, api.EventTaskUpdated)
	}

	payload, ok := apiEv.Payload.(api.TaskUpdatedEvent)
	if !ok {
		t.Fatalf("expected api.TaskUpdatedEvent payload, got %T", apiEv.Payload)
	}
	if payload.SessionID != "sess-1" || payload.Task.ID != "task-2" || payload.Task.Status != api.TaskStatusCompleted {
		t.Fatalf("unexpected TaskUpdatedEvent payload: %+v", payload)
	}
}

func TestToAPIEventStatusChanged(t *testing.T) {
	rawMsg := broker.Message{
		ID:    "evt-status-1",
		Topic: api.SessionStreamTopic("sess-1"),
		Event: session.StatusChangedEvent{
			SessionID: "sess-1",
			Status:    session.StatusExecuting,
		},
	}

	d := &Daemon{}
	apiEv, err := d.FormatEvent(rawMsg)
	if err != nil {
		t.Fatalf("FormatEvent error: %v", err)
	}

	if apiEv.Type != api.EventStatusChanged {
		t.Fatalf("apiEv.Type = %q, want %q", apiEv.Type, api.EventStatusChanged)
	}

	payload, ok := apiEv.Payload.(api.StatusChangedEvent)
	if !ok {
		t.Fatalf("expected api.StatusChangedEvent payload, got %T", apiEv.Payload)
	}
	if payload.SessionID != "sess-1" || payload.Status != api.StatusExecuting {
		t.Fatalf("unexpected StatusChangedEvent payload: %+v", payload)
	}
}

func TestToAPISessionWithTodos(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	sess := &session.Session{
		ID:    "sess-todos",
		Title: "Todos Session",
		Config: session.SessionConfig{
			Agent: "developer",
		},
		CreatedAt: now,
		UpdatedAt: now,
		State: &session.State{
			Status: session.StatusThinking,
			Todos: []tools.TodoItem{
				{
					Content:    "Implement feature",
					ActiveText: "Implementing feature",
					Status:     tools.TodoStatusInProgress,
				},
				{
					Content:    "Run tests",
					ActiveText: "Running tests",
					Status:     tools.TodoStatusPending,
				},
			},
		},
	}

	apiSess := toAPISession(sess)
	if len(apiSess.Todos) != 2 {
		t.Fatalf("expected 2 todos, got %d", len(apiSess.Todos))
	}
	if apiSess.Todos[0].Content != "Implement feature" || apiSess.Todos[0].Status != "in_progress" {
		t.Fatalf("unexpected todo[0]: %+v", apiSess.Todos[0])
	}
	if apiSess.Todos[1].Content != "Run tests" || apiSess.Todos[1].Status != "pending" {
		t.Fatalf("unexpected todo[1]: %+v", apiSess.Todos[1])
	}
}

func TestToAPIEventTodosUpdated(t *testing.T) {
	rawMsg := broker.Message{
		ID:    "evt-todos-1",
		Topic: api.SessionStreamTopic("sess-todos"),
		Event: session.TodosUpdatedEvent{
			SessionID: "sess-todos",
			Todos: []tools.TodoItem{
				{
					Content:    "Deploy",
					ActiveText: "Deploying",
					Status:     tools.TodoStatusCompleted,
				},
			},
		},
	}

	d := &Daemon{}
	apiEv, err := d.FormatEvent(rawMsg)
	if err != nil {
		t.Fatalf("FormatEvent error: %v", err)
	}

	if apiEv.Type != api.EventTodosUpdated {
		t.Fatalf("apiEv.Type = %q, want %q", apiEv.Type, api.EventTodosUpdated)
	}

	payload, ok := apiEv.Payload.(api.TodosUpdatedEvent)
	if !ok {
		t.Fatalf("expected api.TodosUpdatedEvent payload, got %T", apiEv.Payload)
	}
	if payload.SessionID != "sess-todos" || len(payload.Todos) != 1 || payload.Todos[0].Status != "completed" {
		t.Fatalf("unexpected TodosUpdatedEvent payload: %+v", payload)
	}
}
