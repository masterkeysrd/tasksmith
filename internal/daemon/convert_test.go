package daemon

import (
	"testing"
	"time"

	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

func TestCatalogModelProjection(t *testing.T) {
	m := models.Merge("provider/model", "openai", &workspace.ProviderModel{
		ID: "model", Label: "Friendly", Limits: workspace.ModelLimits{Context: 5000},
		Capabilities: &workspace.ModelCapabilities{Tools: new(false)},
		Cost:         &workspace.ModelCost{Input: 3, Tiers: []workspace.ModelCostTier{{Label: "large", ConditionType: "context", ConditionSize: 100, Input: 5}}},
	}, &profile.Profile{Name: "Built-in", Limits: profile.Limits{Context: 9000, Output: 700}, Capabilities: profile.Capabilities{ToolCall: true}, Pricing: profile.Pricing{Input: 1, Output: 2}})
	got := catalogToAPIModel(m)
	if got.Label != "Friendly" || got.ContextLimit != 5000 || got.OutputLimit != 700 || got.Capabilities.Tools == nil || *got.Capabilities.Tools {
		t.Fatalf("unexpected effective model: %+v", got)
	}
	if got.Pricing == nil || got.Pricing.Input != 3 || got.Pricing.Output != 0 || len(got.Pricing.Tiers) != 1 || got.Pricing.Tiers[0].ConditionSize != 100 {
		t.Fatalf("unexpected pricing: %+v", got.Pricing)
	}
}

func TestThinkingDescriptions(t *testing.T) {
	values := []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "auto", "custom", "HIGH"}
	got := toAPIThinkingValues("effort", values)
	for i, value := range values {
		if got[i].Value != value {
			t.Fatalf("value %d changed: %+v", i, got[i])
		}
		if (got[i].Description == "") != (value == "custom") {
			t.Fatalf("unexpected description for %q: %q", value, got[i].Description)
		}
	}
	if other := toAPIThinkingValues("toggle", []string{"high"}); other[0].Description != "" {
		t.Fatalf("non-effort value received a hint: %+v", other)
	}
	model := catalogToAPIModel(models.Merge("test/model", "test", &workspace.ProviderModel{ID: "model", Capabilities: &workspace.ModelCapabilities{Thinking: []workspace.ThinkingCapability{{Type: "effort", AllowedEfforts: values}}}}, nil))
	if len(model.Thinking) != 1 || len(model.Thinking[0].Values) != len(values) || model.Thinking[0].Values[2].Description == "" {
		t.Fatalf("workspace model missing descriptions: %+v", model.Thinking)
	}
	model = catalogToAPIModel(models.Merge("test/model", "test", nil, &profile.Profile{Capabilities: profile.Capabilities{ReasoningOptions: []profile.ReasoningOption{{Type: "effort", Values: values}}}}))
	if len(model.Thinking) != 1 || len(model.Thinking[0].Values) != len(values) || model.Thinking[0].Values[2].Description == "" || model.Thinking[0].Values[8].Description != "" {
		t.Fatalf("profile model has incorrect descriptions: %+v", model.Thinking)
	}
}

func TestSessionDiagnosticsConversion(t *testing.T) {
	sess := &session.Session{ID: "test", State: session.NewState(), Diagnostics: []models.Diagnostic{{Code: "temperature_unsupported", Setting: "temperature", Message: "omitted"}}}
	want := api.SessionDiagnostic{Code: "temperature_unsupported", Setting: "temperature", Message: "omitted"}
	converted := toAPISession(sess)
	if len(converted.Diagnostics) != 1 || converted.Diagnostics[0] != want {
		t.Fatalf("session diagnostics: %+v", converted.Diagnostics)
	}
	d := &Daemon{}
	event, err := d.FormatEvent(broker.Message{Topic: api.TopicSessions, Event: session.UpdatedEvent{Session: sess}})
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := event.Payload.(api.SessionUpdatedEvent)
	if !ok || len(payload.Session.Diagnostics) != 1 || payload.Session.Diagnostics[0] != want {
		t.Fatalf("event diagnostics: %+v", event.Payload)
	}
}

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

func TestToAPIModelWithSlashInID(t *testing.T) {
	pm := workspace.ProviderModel{
		ID:    "ddalcu/Qwen3.8-27B-MLX-Serve-4bit",
		Name:  "qwen3.8-27b-mlx-serve-4bit",
		Label: "Qwen 3.8 (MLX Serve)",
		Limits: workspace.ModelLimits{
			Context: 262144,
			Output:  32768,
		},
	}
	got := catalogToAPIModel(models.Merge("mlx-serve/"+models.ID(pm.ID), "openai", &pm, nil))
	if got.ID != "mlx-serve/ddalcu/Qwen3.8-27B-MLX-Serve-4bit" {
		t.Errorf("got.ID = %q, want %q", got.ID, "mlx-serve/ddalcu/Qwen3.8-27B-MLX-Serve-4bit")
	}
	if got.Provider != "mlx-serve" {
		t.Errorf("got.Provider = %q, want %q", got.Provider, "mlx-serve")
	}
	if got.Label != "Qwen 3.8 (MLX Serve)" {
		t.Errorf("got.Label = %q, want %q", got.Label, "Qwen 3.8 (MLX Serve)")
	}
}
