package command

import (
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func TestSubagentCommandRegistration(t *testing.T) {
	reg := NewRegistry()
	cmd, ok := reg.Lookup("/subagents")
	if !ok {
		t.Fatal("expected /subagents command to be registered")
	}
	if cmd.Name != "subagents" {
		t.Fatalf("expected name subagents, got %s", cmd.Name)
	}

	aliasCmd, ok := reg.Lookup("/subagent")
	if !ok {
		t.Fatal("expected /subagent alias to be registered")
	}
	if aliasCmd.Name != "subagents" {
		t.Fatalf("expected alias to resolve to subagents, got %s", aliasCmd.Name)
	}
}

func TestHandleSubagentsEnqueuesSlot(t *testing.T) {
	store.ClearSlot()
	defer store.ClearSlot()

	err := handleSubagents(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	active := store.GetSlot().Active
	if active == nil {
		t.Fatal("expected active slot item after handleSubagents")
	}
	if active.Kind != store.SlotKindPicker {
		t.Fatalf("expected SlotKindPicker, got %s", active.Kind)
	}
}

func TestOpenSubagentEnqueuesViewerSlot(t *testing.T) {
	store.ClearSlot()
	defer store.ClearSlot()

	store.OpenSubagent("test-sub-1")

	active := store.GetSlot().Active
	if active == nil {
		t.Fatal("expected active slot item after OpenSubagent")
	}
	if active.Kind != store.SlotKindPicker {
		t.Fatalf("expected SlotKindPicker, got %s", active.Kind)
	}
	if !strings.Contains(active.ID, "subagent-viewer-test-sub-1") {
		t.Fatalf("expected active.ID to contain subagent-viewer-test-sub-1, got %s", active.ID)
	}
}

func TestSubagentsBrowserRendering(t *testing.T) {
	store.ResetSession()
	store.ClearSlot()
	defer store.ResetSession()
	defer store.ClearSlot()

	// 1. When no subagents at all
	got := renderNode(t, SubagentsBrowser("slot-1"))
	if !strings.Contains(got, "Subagents") || !strings.Contains(got, "No subagents.") {
		t.Fatalf("expected empty subagents message, got %q", got)
	}

	// 2. When only idle subagents exist
	store.UpsertSubagent(api.Session{
		ID:        "sub-idle",
		Title:     "Old completed task",
		Status:    api.StatusIdle,
		CreatedAt: time.Now().Add(-1 * time.Hour),
		UpdatedAt: time.Now().Add(-50 * time.Minute),
		Config:    api.SessionConfig{Agent: "explore"},
	})

	got = renderNode(t, SubagentsBrowser("slot-1"))
	if !strings.Contains(got, "No running subagents.") {
		t.Fatalf("expected 'No running subagents.' message, got %q", got)
	}

	// 3. When active subagent exists
	store.UpsertSubagent(api.Session{
		ID:        "sub-active",
		Title:     "Audit auth middleware",
		Status:    api.StatusExecuting,
		CreatedAt: time.Now().Add(-2 * time.Minute),
		Config:    api.SessionConfig{Agent: "explore"},
	})

	got = renderNode(t, SubagentsBrowser("slot-1"))
	if !strings.Contains(got, "@explore") || !strings.Contains(got, "Audit auth middleware") || !strings.Contains(got, "executing") {
		t.Fatalf("expected active subagent in browser, got %q", got)
	}

	// 4. Waiting permission status formats without raw underscore
	store.UpsertSubagent(api.Session{
		ID:        "sub-perm",
		Title:     "Run ollama ls",
		Status:    api.StatusWaitingPermission,
		CreatedAt: time.Now().Add(-20 * time.Second),
		Config:    api.SessionConfig{Agent: "general"},
	})
	got = renderNode(t, SubagentsBrowser("slot-1"))
	if !strings.Contains(got, "Waiting Permission") || strings.Contains(got, "waiting_permission") {
		t.Fatalf("expected 'waiting permission' without raw underscore, got %q", got)
	}
}

func TestSubagentViewerRendering(t *testing.T) {
	store.ResetSession()
	store.ClearSlot()
	defer store.ResetSession()
	defer store.ClearSlot()

	sub := api.Session{
		ID:        "sub-view-1",
		Title:     "Audit auth middleware",
		Status:    api.StatusExecuting,
		CreatedAt: time.Now().Add(-5 * time.Minute),
		Config:    api.SessionConfig{Agent: "explore"},
		PendingPermissions: []api.PermissionRequest{
			{
				ID:       "req-1",
				ToolName: "bash",
				Target:   "git diff",
			},
		},
	}
	store.UpsertSubagent(sub)

	got := renderNode(t, SubagentViewer(SubagentViewerProps{
		SubagentID: "sub-view-1",
	}))

	// Header
	if !strings.Contains(got, "@explore") || !strings.Contains(got, "Audit auth middleware") || !strings.Contains(got, "executing") {
		t.Fatalf("expected subagent header, got %q", got)
	}

	// Permission prompt rendered inline
	if !strings.Contains(got, "bash") || !strings.Contains(got, "git diff") {
		t.Fatalf("expected inline permission prompt for bash, got %q", got)
	}

	// Keyboard hint
	if !strings.Contains(got, "Return") {
		t.Fatalf("expected Return keyboard hint, got %q", got)
	}
}
