package store

import (
	"context"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestSessionStoreBasic(t *testing.T) {
	ResetSession()
	defer ResetSession()

	s := GetSession()
	if s.Config.Mode != api.ModeDefault {
		t.Fatalf("expected ModeDefault initially, got %q", s.Config.Mode)
	}
	if s.Ready {
		t.Fatal("expected Ready to be false initially")
	}

	SetMode(api.ModePlan)
	if GetSession().Config.Mode != api.ModePlan {
		t.Fatalf("expected ModePlan, got %q", GetSession().Config.Mode)
	}

	SetModelInfo("Claude 3.7", "high")
	s = GetSession()
	if s.ModelLabel != "Claude 3.7" || s.Thinking != "high" || !s.Ready {
		t.Fatalf("unexpected model info: %+v", s)
	}

	// SetSession with empty mode should default to ModeDefault
	sess := &api.Session{
		ID: "sess-1",
		Config: api.SessionConfig{
			Agent: "coding",
			Model: "claude-3-7",
		},
	}
	SetSession(sess)
	s = GetSession()
	if s.Session != sess || s.Config.Mode != api.ModeDefault {
		t.Fatalf("unexpected session after SetSession: %+v", s)
	}

	// SetSession with explicit mode
	sess.Config.Mode = api.ModeAcceptEdits
	SetSession(sess)
	if GetSession().Config.Mode != api.ModeAcceptEdits {
		t.Fatalf("expected ModeAcceptEdits, got %q", GetSession().Config.Mode)
	}

	ResetSession()
	s = GetSession()
	if s.Config.Mode != api.ModeDefault || s.Session != nil || s.Ready {
		t.Fatalf("expected clean state after ResetSession, got %+v", s)
	}
}

func TestResolveModelInfoNilClient(t *testing.T) {
	ctx := context.Background()

	// Default fallback
	label, thinking := ResolveModelInfo(ctx, nil, "", "", nil)
	if label != "default" || thinking != "auto" {
		t.Fatalf("expected default/auto, got %q/%q", label, thinking)
	}

	// Passed config
	label, thinking = ResolveModelInfo(ctx, nil, "agent1", "gpt-4", nil)
	if label != "gpt-4" || thinking != "auto" {
		t.Fatalf("expected gpt-4/auto, got %q/%q", label, thinking)
	}

	// Sess overrides cfg
	sess := &api.Session{
		Config: api.SessionConfig{
			Agent: "sess-agent",
			Model: "claude-3-7",
		},
	}
	label, thinking = ResolveModelInfo(ctx, nil, "agent1", "gpt-4", sess)
	if label != "claude-3-7" || thinking != "auto" {
		t.Fatalf("expected claude-3-7/auto from sess, got %q/%q", label, thinking)
	}
}

func TestNextPermissionMode(t *testing.T) {
	tests := []struct {
		current api.PermissionMode
		want    api.PermissionMode
	}{
		{"", api.ModeAcceptEdits},
		{api.ModeDefault, api.ModeAcceptEdits},
		{api.ModeAcceptEdits, api.ModePlan},
		{api.ModePlan, api.ModeAsk},
		{api.ModeAsk, api.ModeAuto},
		{api.ModeAuto, api.ModeDefault},
		{api.PermissionMode("other"), api.ModeDefault},
	}

	for _, tt := range tests {
		if got := NextPermissionMode(tt.current); got != tt.want {
			t.Errorf("NextPermissionMode(%q) = %q, want %q", tt.current, got, tt.want)
		}
	}
}

func TestCycleMode(t *testing.T) {
	ResetSession()
	defer ResetSession()

	if GetSession().Config.Mode != api.ModeDefault {
		t.Fatalf("expected initial ModeDefault, got %q", GetSession().Config.Mode)
	}

	expectedSequence := []api.PermissionMode{
		api.ModeAcceptEdits,
		api.ModePlan,
		api.ModeAsk,
		api.ModeAuto,
		api.ModeDefault,
	}

	for _, want := range expectedSequence {
		got := CycleMode()
		if got != want {
			t.Fatalf("CycleMode() = %q, want %q", got, want)
		}
		if GetSession().Config.Mode != want {
			t.Fatalf("GetSession().Config.Mode = %q, want %q", GetSession().Config.Mode, want)
		}
	}
}

func TestTasksStore(t *testing.T) {
	ResetSession()
	defer ResetSession()

	if tasks := GetTasks(); len(tasks) != 0 {
		t.Fatalf("expected 0 initial tasks, got %d", len(tasks))
	}
	if running := GetRunningTasks(); len(running) != 0 {
		t.Fatalf("expected 0 running tasks, got %d", len(running))
	}

	task1 := api.Task{
		ID:      "t-1",
		Command: "sleep 10",
		Status:  api.TaskStatusRunning,
	}
	UpsertTask(task1)

	tasks := GetTasks()
	if len(tasks) != 1 || tasks[0].ID != "t-1" || tasks[0].Status != api.TaskStatusRunning {
		t.Fatalf("unexpected tasks after UpsertTask: %+v", tasks)
	}

	running := GetRunningTasks()
	if len(running) != 1 || running[0].ID != "t-1" {
		t.Fatalf("unexpected running tasks: %+v", running)
	}

	// Update task1 to completed
	task1.Status = api.TaskStatusCompleted
	UpsertTask(task1)

	tasks = GetTasks()
	if len(tasks) != 1 || tasks[0].Status != api.TaskStatusCompleted {
		t.Fatalf("unexpected tasks after completion update: %+v", tasks)
	}

	running = GetRunningTasks()
	if len(running) != 0 {
		t.Fatalf("expected 0 running tasks after completion, got %d", len(running))
	}

	// Upsert second task
	task2 := api.Task{
		ID:      "t-2",
		Command: "npm run dev",
		Status:  api.TaskStatusPending,
	}
	UpsertTask(task2)

	tasks = GetTasks()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	running = GetRunningTasks()
	if len(running) != 1 || running[0].ID != "t-2" {
		t.Fatalf("expected 1 running task (t-2), got %+v", running)
	}

	// Verify NewDraftSession clears tasks
	NewDraftSession("")
	if tasks = GetTasks(); len(tasks) != 0 {
		t.Fatalf("expected 0 tasks after NewDraftSession, got %d", len(tasks))
	}
	if running = GetRunningTasks(); len(running) != 0 {
		t.Fatalf("expected 0 running tasks after NewDraftSession, got %d", len(running))
	}
}
