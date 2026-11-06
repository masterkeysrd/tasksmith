package store

import (
	"context"
	"strings"
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

func TestSetWorkspaceMCPServersAnnouncesTimelineWarnings(t *testing.T) {
	ClearTranscript()
	SetWorkspaceWarnings(nil)
	ClearMCPWarningAnnouncements()
	ResetApp()
	defer func() {
		SetWorkspaceMCPServers(nil)
		SetWorkspaceWarnings(nil)
		ClearTranscript()
		ResetApp()
	}()

	unhealthy := []api.MCPServer{
		{Name: "notes", Status: api.MCPStatusConnected},
		{Name: "github", Status: api.MCPStatusAuthRequired, Error: `missing credential environment variable "GITHUB_TOKEN"`},
		{Name: "search", Status: api.MCPStatusError, Error: `connect MCP server "search": calling "initialize": connection refused`},
	}
	SetWorkspaceMCPServers(unhealthy)

	warnings := transcriptWarnings(t)
	if len(warnings) != 2 {
		t.Fatalf("expected a warning per unhealthy server, got %v", warnings)
	}
	if !strings.Contains(warnings[0], "github") || !strings.Contains(warnings[0], "requires authentication") {
		t.Errorf("unexpected auth warning: %q", warnings[0])
	}
	if !strings.Contains(warnings[0], "GITHUB_TOKEN") {
		t.Errorf("auth warning should name the missing credential: %q", warnings[0])
	}
	if !strings.Contains(warnings[1], "search") || !strings.Contains(warnings[1], "connection refused") {
		t.Errorf("unexpected connect warning: %q", warnings[1])
	}
	if strings.Contains(warnings[1], "connect MCP server") {
		t.Errorf("warning leaked transport wrappers: %q", warnings[1])
	}
}

func TestMCPServerWarningsStayOutOfTheHeader(t *testing.T) {
	ClearTranscript()
	SetWorkspaceWarnings(nil)
	ClearMCPWarningAnnouncements()
	ResetApp()
	defer func() {
		SetWorkspaceMCPServers(nil)
		SetWorkspaceWarnings(nil)
		ClearTranscript()
		ResetApp()
	}()

	// The startup banner freezes once the timeline starts, so MCP health that
	// lands there both jitters the banner and goes stale. Workspace warnings are
	// the only thing the header may show.
	SetWorkspaceWarnings([]string{"provider warning"})
	SetWorkspaceMCPServers([]api.MCPServer{
		{Name: "github", Status: api.MCPStatusAuthRequired},
		{Name: "search", Status: api.MCPStatusError, Error: "connection refused"},
	})

	got := CurrentHeaderData().Warnings
	if len(got) != 1 || got[0] != "provider warning" {
		t.Fatalf("expected only the workspace warning in the header, got %v", got)
	}
	if len(transcriptWarnings(t)) != 2 {
		t.Errorf("expected both MCP problems in the timeline, got %v", transcriptWarnings(t))
	}

	// Re-fetching the workspace must leave the header alone apart from its own
	// warnings, and must not duplicate a warning the daemon happens to report.
	SetWorkspaceWarnings([]string{"provider warning", "provider warning"})
	if got := CurrentHeaderData().Warnings; len(got) != 1 || got[0] != "provider warning" {
		t.Fatalf("expected duplicates to collapse, got %v", got)
	}

	// Connecting a server must not disturb the header either.
	SetWorkspaceMCPServers([]api.MCPServer{{Name: "github", Status: api.MCPStatusConnected}})
	if got := CurrentHeaderData().Warnings; len(got) != 1 || got[0] != "provider warning" {
		t.Fatalf("expected the header to be unchanged after connect, got %v", got)
	}

	SetWorkspaceMCPServers(nil)
	if got := CurrentHeaderData().Warnings; len(got) != 1 || got[0] != "provider warning" {
		t.Fatalf("expected the header to be unchanged once the listing clears, got %v", got)
	}
}

func TestMCPServerWarningsAnnounceOncePerProblem(t *testing.T) {
	ClearTranscript()
	SetWorkspaceWarnings(nil)
	ClearMCPWarningAnnouncements()
	ResetApp()
	defer func() {
		SetWorkspaceMCPServers(nil)
		SetWorkspaceWarnings(nil)
		ClearTranscript()
		ResetApp()
	}()

	broken := []api.MCPServer{{Name: "search", Status: api.MCPStatusError, Error: "connection refused"}}

	// The listing is polled while servers settle and whenever /mcp is opened, so
	// the same problem must be said once no matter how often it is read.
	SetWorkspaceMCPServers(broken)
	SetWorkspaceMCPServers(broken)
	SetWorkspaceMCPServers(broken)
	if warnings := transcriptWarnings(t); len(warnings) != 1 {
		t.Fatalf("expected repeated identical listings to announce once, got %v", warnings)
	}

	// A new failure reason is new information and deserves to be said again.
	SetWorkspaceMCPServers([]api.MCPServer{{Name: "search", Status: api.MCPStatusError, Error: "server exited"}})
	warnings := transcriptWarnings(t)
	if len(warnings) != 2 || !strings.Contains(warnings[1], "server exited") {
		t.Fatalf("expected the new failure reason to be announced, got %v", warnings)
	}

	// A problem that turns into a different problem is also new information.
	SetWorkspaceMCPServers([]api.MCPServer{{Name: "search", Status: api.MCPStatusAuthRequired}})
	warnings = transcriptWarnings(t)
	if len(warnings) != 3 || !strings.Contains(warnings[2], "requires authentication") {
		t.Fatalf("expected the auth requirement to be announced, got %v", warnings)
	}
}

func TestMCPServerWarningsSilenceOnceHealthy(t *testing.T) {
	ClearTranscript()
	SetWorkspaceWarnings(nil)
	ClearMCPWarningAnnouncements()
	ResetApp()
	defer func() {
		SetWorkspaceMCPServers(nil)
		SetWorkspaceWarnings(nil)
		ClearTranscript()
		ResetApp()
	}()

	SetWorkspaceMCPServers([]api.MCPServer{{Name: "github", Status: api.MCPStatusAuthRequired}})
	SetWorkspaceMCPServers([]api.MCPServer{{Name: "github", Status: api.MCPStatusConnected}})
	SetWorkspaceMCPServers([]api.MCPServer{{Name: "github", Status: api.MCPStatusConnecting}})
	if warnings := transcriptWarnings(t); len(warnings) != 1 {
		t.Fatalf("expected a healthy or still-connecting server to stay silent, got %v", warnings)
	}

	// A server that breaks again after connecting is worth reporting again.
	SetWorkspaceMCPServers([]api.MCPServer{{Name: "github", Status: api.MCPStatusAuthRequired}})
	if warnings := transcriptWarnings(t); len(warnings) != 2 {
		t.Fatalf("expected a server that breaks again to be announced again, got %v", warnings)
	}
}

func TestMCPServerWarningsReannouncedForANewListing(t *testing.T) {
	ClearTranscript()
	SetWorkspaceWarnings(nil)
	ClearMCPWarningAnnouncements()
	ResetApp()
	defer func() {
		SetWorkspaceMCPServers(nil)
		SetWorkspaceWarnings(nil)
		ClearTranscript()
		ResetApp()
	}()

	broken := []api.MCPServer{{Name: "github", Status: api.MCPStatusAuthRequired}}
	SetWorkspaceMCPServers(broken)

	// A listing that no longer contains the server forgets its history, so
	// re-adding it — or starting over in another workspace — reports it again.
	SetWorkspaceMCPServers(nil)
	SetWorkspaceMCPServers(broken)
	if warnings := transcriptWarnings(t); len(warnings) != 2 {
		t.Fatalf("expected the server to be announced again after leaving the listing, got %v", warnings)
	}

	ClearTranscript()
	ClearMCPWarningAnnouncements()
	SetWorkspaceMCPServers(broken)
	SetWorkspaceMCPServers(broken)
	if warnings := transcriptWarnings(t); len(warnings) != 1 {
		t.Fatalf("expected clearing announcements to reset the dedup memory, got %v", warnings)
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
