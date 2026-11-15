package components

import (
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func TestInvokeAgentToolWidget(t *testing.T) {
	call := &api.ToolCall{
		Name: "invoke_agent",
		Args: map[string]any{
			"agent":     "explore",
			"title":     "Find auth middleware",
			"workspace": "inherit",
		},
	}
	response := &api.Message{
		Name:              "invoke_agent",
		StructuredContent: []byte(`{"session_id":"sess-sub-1","agent":"explore","title":"Find auth middleware","status":"running"}`),
	}
	failure := &api.Message{
		Name:    "invoke_agent",
		IsError: true,
		Content: []api.Block{{Type: api.BlockText, Text: "unknown agent persona\nline 2"}},
	}

	render := renderComponent(t, 100)

	// 1. Spawning
	got := render(MessageTool(MessageToolProps{
		Tool: store.ToolPart{Call: call, Status: store.ToolRunning},
	}))
	if !strings.Contains(got, "Spawning") || !strings.Contains(got, "@explore") || !strings.Contains(got, "Find auth middleware") {
		t.Fatalf("expected spawning @explore, got %q", got)
	}

	// 2. Spawned with SessionID
	got = render(MessageTool(MessageToolProps{
		Tool: store.ToolPart{Call: call, Response: response, Status: store.ToolSucceeded},
	}))
	if !strings.Contains(got, "Spawned") || !strings.Contains(got, "(sess-sub-1)") {
		t.Fatalf("expected spawned with sess-sub-1, got %q", got)
	}

	// 3. Worktree suffix
	callWorktree := &api.ToolCall{
		Name: "invoke_agent",
		Args: map[string]any{
			"agent":     "general",
			"title":     "Refactor parser",
			"workspace": "worktree",
		},
	}
	responseWorktree := &api.Message{
		Name:              "invoke_agent",
		StructuredContent: []byte(`{"session_id":"sess-sub-2","agent":"general","title":"Refactor parser","status":"running"}`),
	}
	got = render(MessageTool(MessageToolProps{
		Tool: store.ToolPart{Call: callWorktree, Response: responseWorktree, Status: store.ToolSucceeded},
	}))
	if !strings.Contains(got, "(sess-sub-2, worktree)") {
		t.Fatalf("expected worktree suffix, got %q", got)
	}

	// 4. Failed
	got = render(MessageTool(MessageToolProps{
		Tool: store.ToolPart{Call: call, Response: failure, Status: store.ToolFailed},
	}))
	if !strings.Contains(got, "Spawned") || !strings.Contains(got, "unknown agent persona") {
		t.Fatalf("expected failure message, got %q", got)
	}
}

func TestManageAgentsToolWidget(t *testing.T) {
	render := renderComponent(t, 100)

	// List
	listCall := &api.ToolCall{
		Name: "manage_agents",
		Args: map[string]any{"action": "list"},
	}
	listResp := &api.Message{
		Name:              "manage_agents",
		StructuredContent: []byte(`{"agents":[{"session_id":"s1"},{"session_id":"s2"}]}`),
	}
	got := render(MessageTool(MessageToolProps{
		Tool: store.ToolPart{Call: listCall, Response: listResp, Status: store.ToolSucceeded},
	}))
	if !strings.Contains(got, "Listed agents") || !strings.Contains(got, "(2 agents)") {
		t.Fatalf("expected listed 2 agents, got %q", got)
	}

	// Status
	statusCall := &api.ToolCall{
		Name: "manage_agents",
		Args: map[string]any{"action": "status", "session_id": "sub-123"},
	}
	statusResp := &api.Message{
		Name:              "manage_agents",
		StructuredContent: []byte(`{"status":"executing"}`),
	}
	got = render(MessageTool(MessageToolProps{
		Tool: store.ToolPart{Call: statusCall, Response: statusResp, Status: store.ToolSucceeded},
	}))
	if !strings.Contains(got, "Checked agent") || !strings.Contains(got, "sub-123") || !strings.Contains(got, "(executing)") {
		t.Fatalf("expected checked agent sub-123 (executing), got %q", got)
	}

	// Send
	sendCall := &api.ToolCall{
		Name: "manage_agents",
		Args: map[string]any{"action": "send", "session_id": "sub-123", "message": "hello"},
	}
	sendResp := &api.Message{
		Name:              "manage_agents",
		StructuredContent: []byte(`{"status":"thinking"}`),
	}
	got = render(MessageTool(MessageToolProps{
		Tool: store.ToolPart{Call: sendCall, Response: sendResp, Status: store.ToolSucceeded},
	}))
	if !strings.Contains(got, "Messaged agent") || !strings.Contains(got, "sub-123") || !strings.Contains(got, "(thinking)") {
		t.Fatalf("expected messaged agent sub-123 (thinking), got %q", got)
	}

	// Kill
	killCall := &api.ToolCall{
		Name: "manage_agents",
		Args: map[string]any{"action": "kill", "session_id": "sub-123"},
	}
	killResp := &api.Message{
		Name:              "manage_agents",
		StructuredContent: []byte(`{"status":"idle"}`),
	}
	got = render(MessageTool(MessageToolProps{
		Tool: store.ToolPart{Call: killCall, Response: killResp, Status: store.ToolSucceeded},
	}))
	if !strings.Contains(got, "Killed agent") || !strings.Contains(got, "sub-123") || !strings.Contains(got, "(idle)") {
		t.Fatalf("expected killed agent sub-123 (idle), got %q", got)
	}
}

func TestTasksComponentUnifiedSubagentsAndTasks(t *testing.T) {
	store.ResetSession()
	store.ClearSlot()
	defer store.ResetSession()
	defer store.ClearSlot()

	render := renderComponent(t, 100)

	// Subagent running with activity
	sub := api.Session{
		ID:        "sub-1",
		Title:     "Audit auth middleware",
		Status:    api.StatusExecuting,
		Activity:  "Searching codebase",
		CreatedAt: time.Now().Add(-10 * time.Second),
		Config:    api.SessionConfig{Agent: "explore"},
	}
	store.UpsertSubagent(sub)

	// Task running
	task := api.Task{
		ID:        "t-1",
		Command:   "npm test",
		Status:    api.TaskStatusRunning,
		StartedAt: time.Now().Add(-5 * time.Second),
	}
	store.UpsertTask(task)

	got := render(Tasks())
	if !strings.Contains(got, "Agent(explore: Audit auth middleware)") {
		t.Fatalf("expected Agent(explore: Audit auth middleware), got %q", got)
	}
	if !strings.Contains(got, "Searching codebase") {
		t.Fatalf("expected activity Searching codebase, got %q", got)
	}
	if !strings.Contains(got, "npm test") {
		t.Fatalf("expected task npm test, got %q", got)
	}
}

func TestSubagentAlert(t *testing.T) {
	store.ResetSession()
	store.ClearSlot()
	defer store.ResetSession()
	defer store.ClearSlot()

	render := renderComponent(t, 100)

	// No waiting subagents -> returns empty
	if got := render(SubagentAlert()); got != "" {
		t.Fatalf("expected empty alert when no waiting subagents, got %q", got)
	}

	// Subagent waiting for permission
	subPerm := api.Session{
		ID:        "sub-perm",
		Title:     "Install dependencies",
		Status:    api.StatusWaitingPermission,
		CreatedAt: time.Now(),
		Config:    api.SessionConfig{Agent: "explore"},
		PendingPermissions: []api.PermissionRequest{
			{
				ID:       "req-1",
				ToolName: "bash",
				Target:   "npm install express",
			},
		},
	}
	store.UpsertSubagent(subPerm)

	got := render(SubagentAlert())
	if !strings.Contains(got, "⚠ Subagent @explore (Install dependencies) requires permission") {
		t.Fatalf("expected permission warning line, got %q", got)
	}
	if !strings.Contains(got, "bash · npm install express") {
		t.Fatalf("expected tool and target line, got %q", got)
	}
	if !strings.Contains(got, "ctrl+y allow once • ctrl+g manage subagent") {
		t.Fatalf("expected action hints, got %q", got)
	}

	// Active slot hides the alert
	store.EnqueueSlot(store.SlotItem{Kind: store.SlotKindPicker})
	if got := render(SubagentAlert()); got != "" {
		t.Fatalf("expected alert hidden when slot active, got %q", got)
	}
	store.ClearSlot()

	// Subagent waiting for question
	store.ResetSession()
	subQuestion := api.Session{
		ID:        "sub-quest",
		Title:     "Configure database",
		Status:    api.StatusWaitingQuestion,
		CreatedAt: time.Now(),
		Config:    api.SessionConfig{Agent: "general"},
		PendingQuestions: []api.QuestionRequest{
			{
				ID: "q-1",
				Questions: []api.Question{
					{Question: "Use postgres or sqlite?"},
				},
			},
		},
	}
	store.UpsertSubagent(subQuestion)

	got = render(SubagentAlert())
	if !strings.Contains(got, "⚠ Subagent @general (Configure database) is waiting for an answer") {
		t.Fatalf("expected question warning line, got %q", got)
	}
	if !strings.Contains(got, "ctrl+g manage subagent") {
		t.Fatalf("expected manage hint, got %q", got)
	}
}
