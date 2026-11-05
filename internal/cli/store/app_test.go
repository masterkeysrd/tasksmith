package store

import (
	"context"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
)

func TestStoreApp(t *testing.T) {
	ResetApp()
	defer ResetApp()

	if GetApp().CtrlCPending {
		t.Fatal("expected CtrlCPending to be false after ResetApp")
	}
	if GetApp().Hint != DefaultHint {
		t.Fatalf("expected Hint to be %q after ResetApp, got %q", DefaultHint, GetApp().Hint)
	}

	SetCtrlCPending(true)
	if !GetApp().CtrlCPending {
		t.Fatal("expected CtrlCPending to be true after SetCtrlCPending(true)")
	}

	SetCtrlCPending(false)
	if GetApp().CtrlCPending {
		t.Fatal("expected CtrlCPending to be false after SetCtrlCPending(false)")
	}

	SetHint("custom hint")
	if GetApp().Hint != "custom hint" {
		t.Fatalf("expected Hint to be %q, got %q", "custom hint", GetApp().Hint)
	}

	SetCtrlCPending(true)
	ResetApp()
	if GetApp().CtrlCPending {
		t.Fatal("expected CtrlCPending to be false after ResetApp")
	}
	if GetApp().Hint != DefaultHint {
		t.Fatalf("expected Hint to reset to %q, got %q", DefaultHint, GetApp().Hint)
	}
}

func TestStoreAppWarnings(t *testing.T) {
	SetWorkspaceWarnings(nil)
	ResetApp()
	defer func() {
		SetWorkspaceWarnings(nil)
		ResetApp()
	}()

	if GetApp().Warnings != nil {
		t.Fatal("expected Warnings to be nil initially")
	}

	SetWorkspaceWarnings([]string{"w1", "w2"})
	app := GetApp()
	if app.Warnings == nil || len(*app.Warnings) != 2 || (*app.Warnings)[0] != "w1" || (*app.Warnings)[1] != "w2" {
		t.Fatalf("unexpected warnings: %v", app.Warnings)
	}

	// ResetApp preserves warnings
	ResetApp()
	app = GetApp()
	if app.Warnings == nil || len(*app.Warnings) != 2 {
		t.Fatalf("expected warnings preserved after ResetApp: %v", app.Warnings)
	}

	// SetWorkspaceWarnings(nil) clears warnings
	SetWorkspaceWarnings(nil)
	if GetApp().Warnings != nil {
		t.Fatal("expected Warnings to be nil after SetWorkspaceWarnings(nil)")
	}
}

func TestStoreAppAgents(t *testing.T) {
	ResetApp()
	defer ResetApp()

	if GetApp().Agents != nil {
		t.Fatal("expected Agents to be nil initially")
	}

	SetWorkspaceAgents([]api.Agent{{Name: "main"}, {Name: "reviewer"}})
	app := GetApp()
	if app.Agents == nil || len(*app.Agents) != 2 || (*app.Agents)[0].Name != "main" || (*app.Agents)[1].Name != "reviewer" {
		t.Fatalf("unexpected agents: %v", app.Agents)
	}

	// ResetApp clears the agents cache (like commands and skills)
	ResetApp()
	if GetApp().Agents != nil {
		t.Fatalf("expected Agents to be cleared after ResetApp: %v", GetApp().Agents)
	}

	// SetWorkspaceAgents(nil) clears agents
	SetWorkspaceAgents([]api.Agent{{Name: "main"}})
	SetWorkspaceAgents(nil)
	if GetApp().Agents != nil {
		t.Fatal("expected Agents to be nil after SetWorkspaceAgents(nil)")
	}
}

func TestStoreAppTools(t *testing.T) {
	ResetApp()
	defer ResetApp()

	if GetApp().Tools != nil || GetApp().ToolsAgent != "" {
		t.Fatalf("expected Tools cache to be empty initially: %v/%q", GetApp().Tools, GetApp().ToolsAgent)
	}

	// The cache records the agent the statuses were classified for so the
	// browser knows when a different agent requires a fresh listing.
	SetWorkspaceTools([]api.Tool{{Name: "view", Status: api.ToolStatusEager}}, "main")
	app := GetApp()
	if app.Tools == nil || len(*app.Tools) != 1 || (*app.Tools)[0].Name != "view" {
		t.Fatalf("unexpected tools: %v", app.Tools)
	}
	if app.ToolsAgent != "main" {
		t.Errorf("ToolsAgent = %q, want main", app.ToolsAgent)
	}

	// Re-classifying for another agent replaces both the tools and the label.
	SetWorkspaceTools([]api.Tool{{Name: "view", Status: api.ToolStatusDenied}}, "reviewer")
	if got := GetApp().ToolsAgent; got != "reviewer" {
		t.Errorf("ToolsAgent = %q, want reviewer", got)
	}

	// ResetApp clears the tools cache, and an empty listing clears it too.
	ResetApp()
	if GetApp().Tools != nil || GetApp().ToolsAgent != "" {
		t.Fatalf("expected Tools cleared after ResetApp: %v/%q", GetApp().Tools, GetApp().ToolsAgent)
	}
	SetWorkspaceTools([]api.Tool{{Name: "view"}}, "main")
	SetWorkspaceTools(nil, "")
	if GetApp().Tools != nil {
		t.Fatalf("expected Tools to be nil after clearing: %v", GetApp().Tools)
	}
}

// An empty toolset is a valid cached result: the fetched flag has to record it,
// otherwise the browser would re-fetch on every render.
func TestStoreAppToolsFetchedFlag(t *testing.T) {
	ResetApp()
	defer ResetApp()

	if GetApp().ToolsFetched {
		t.Fatal("expected ToolsFetched to be false before the first listing")
	}

	SetWorkspaceTools(nil, "main")
	app := GetApp()
	if !app.ToolsFetched {
		t.Fatal("expected an empty listing to mark the tools as fetched")
	}
	if app.Tools != nil || app.ToolsAgent != "main" {
		t.Fatalf("unexpected empty-listing cache state: %v/%q", app.Tools, app.ToolsAgent)
	}

	ResetApp()
	if GetApp().ToolsFetched {
		t.Fatal("expected ResetApp to clear the fetched flag")
	}
}

func TestStoreAppDefaultAgent(t *testing.T) {
	ResetApp()
	defer ResetApp()

	if GetApp().DefaultAgent != "" {
		t.Fatalf("expected DefaultAgent to be empty initially, got %q", GetApp().DefaultAgent)
	}
	SetDefaultAgent("main")
	if GetApp().DefaultAgent != "main" {
		t.Errorf("DefaultAgent = %q, want main", GetApp().DefaultAgent)
	}
}

func TestEffectiveToolAgent(t *testing.T) {
	ResetApp()
	ResetSession()
	t.Cleanup(func() {
		ResetApp()
		ResetSession()
	})

	// With no session agent, the workspace default agent classifies the tools.
	SetDefaultAgent("workspace-default")
	if got := effectiveToolAgent(); got != "workspace-default" {
		t.Errorf("effectiveToolAgent() = %q, want workspace-default", got)
	}

	// An active session's agent wins over the workspace default.
	SetSession(&api.Session{ID: "sess-1", Config: api.SessionConfig{Agent: "reviewer"}})
	if got := effectiveToolAgent(); got != "reviewer" {
		t.Errorf("effectiveToolAgent() = %q, want reviewer", got)
	}
}

func TestUseHintMountUnmount(t *testing.T) {
	ResetApp()
	defer ResetApp()

	Child := trail.SimpleFC("Child", func() trail.Node {
		UseHint("child hint")
		return trail.Text("child")
	})

	Parent := trail.SimpleFC("Parent", func() trail.Node {
		ctrlC := UseApp(func(s AppState) bool {
			return s.CtrlCPending
		})
		if !ctrlC {
			return Child()
		}
		return trail.Text("no child")
	})

	b := mock.New(80, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = trail.Run(ctx, Parent(), trail.WithBackend(b))
	}()

	// Wait for child to mount and set hint
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if GetApp().Hint == "child hint" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if GetApp().Hint != "child hint" {
		t.Fatalf("expected hint to be %q while child mounted, got %q", "child hint", GetApp().Hint)
	}

	// Trigger unmount of child by changing CtrlCPending
	SetCtrlCPending(true)

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if GetApp().Hint == DefaultHint {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if GetApp().Hint != DefaultHint {
		t.Fatalf("expected hint to be restored to %q on child unmount, got %q", DefaultHint, GetApp().Hint)
	}
}
