package store

import (
	"reflect"
	"slices"
	"testing"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
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

	if GetApp().Warnings != nil || len(CurrentHeaderData().Warnings) != 0 {
		t.Fatal("expected both warning projections to be empty initially")
	}

	const routine = "provider not configured"
	const sandbox = "sandbox enforcement unavailable"
	SetWorkspaceWarnings([]string{routine, sandbox, routine}, sandbox, sandbox)
	assertWarnings := func() {
		t.Helper()
		if got := GetApp().Warnings; got == nil || !slices.Equal(*got, []string{routine, sandbox}) {
			t.Fatalf("expected both workspace warnings once, got %v", got)
		}
		if got := CurrentHeaderData().Warnings; !slices.Equal(got, []string{sandbox}) {
			t.Fatalf("expected only the sandbox warning once in the header, got %q", got)
		}
	}
	assertWarnings()

	ResetApp()
	assertWarnings()

	SetWorkspaceWarnings([]string{routine})
	if got := CurrentHeaderData().Warnings; len(got) != 0 {
		t.Fatalf("routine-only refresh left header warnings: %q", got)
	}

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

	env := testutils.NewEnv(t, 80, 24)
	env.Render(Parent())

	if GetApp().Hint != "child hint" {
		t.Fatalf("expected hint to be %q while child mounted, got %q", "child hint", GetApp().Hint)
	}

	// Trigger unmount of child by changing CtrlCPending
	SetCtrlCPending(true)
	env.Flush()

	if GetApp().Hint != DefaultHint {
		t.Fatalf("expected hint to be restored to %q on child unmount, got %q", DefaultHint, GetApp().Hint)
	}
}

func TestSetWorkspaceMCPServersReplacesCachedRoster(t *testing.T) {
	SetWorkspaceMCPServers(nil)
	t.Cleanup(func() { SetWorkspaceMCPServers(nil) })

	auth := api.MCPServer{Name: "notes", Status: api.MCPStatusAuthRequired}
	broken := api.MCPServer{Name: "search", Status: api.MCPStatusError, Error: "connection refused"}
	connected := api.MCPServer{Name: "notes", Status: api.MCPStatusConnected}
	for _, listing := range [][]api.MCPServer{{auth, broken}, {connected}, nil, {broken}} {
		SetWorkspaceMCPServers(listing)
		cached := GetApp().MCPServers
		if listing == nil {
			if cached != nil {
				t.Fatalf("empty roster did not clear the cache: %+v", *cached)
			}
			continue
		}
		if cached == nil {
			t.Fatalf("missing cached roster for %+v", listing)
		}
		if !reflect.DeepEqual(*cached, listing) {
			t.Fatalf("cached roster = %+v, want %+v", *cached, listing)
		}
	}
}

func transcriptWarnings(t *testing.T) []string {
	t.Helper()
	var warnings []string
	state := GetTranscript()
	if state.Items == nil {
		return nil
	}
	for _, item := range *state.Items {
		if item.Kind != TranscriptKindNotice {
			continue
		}
		notice, ok := item.Data.(NoticeData)
		if !ok || notice.Severity != NoticeSeverityWarning {
			continue
		}
		warnings = append(warnings, notice.Text)
	}
	return warnings
}
