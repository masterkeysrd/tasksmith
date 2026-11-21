package store

import (
	"slices"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestWarningsProjection(t *testing.T) {
	SetWorkspaceWarnings(nil)
	SetWorkspaceMCPServers(nil)
	ClearTranscript()
	t.Cleanup(func() {
		SetWorkspaceWarnings(nil)
		SetWorkspaceMCPServers(nil)
		ClearTranscript()
	})

	auth := api.MCPServer{Name: "notes", Status: api.MCPStatusAuthRequired, AuthType: api.MCPAuthBearer, Error: `missing credential environment variable "NOTES_TOKEN"`}
	broken := api.MCPServer{Name: "search", Status: api.MCPStatusError, Error: "connection refused"}
	const sandboxWarning = "sandbox enforcement unavailable"
	SetWorkspaceWarnings([]string{"provider warning", "provider warning", "manifest warning", sandboxWarning}, sandboxWarning)
	SetWorkspaceMCPServers([]api.MCPServer{
		auth,
		{Name: "in-flight", Status: api.MCPStatusConnecting},
		broken,
		{Name: "healthy", Status: api.MCPStatusConnected},
		{Name: "disconnected", Status: api.MCPStatusDisconnected},
	})
	AppendWarning("historical warning")

	snapshot := func() []Warning {
		s := GetApp()
		return (warningSources{workspace: s.Warnings, mcp: s.MCPServers}).warnings()
	}
	want := []Warning{
		{Source: "Workspace", Message: "provider warning"},
		{Source: "Workspace", Message: "manifest warning"},
		{Source: "Workspace", Message: sandboxWarning},
		{Source: "MCP", Message: api.MCPServerWarning(auth)},
		{Source: "MCP", Message: api.MCPServerWarning(broken)},
	}
	if got := snapshot(); !slices.Equal(got, want) {
		t.Fatalf("warnings = %#v, want %#v", got, want)
	}

	auth.Status = api.MCPStatusConnected
	SetWorkspaceMCPServers([]api.MCPServer{auth})
	if got := snapshot(); !slices.Equal(got, want[:3]) {
		t.Fatalf("warnings after MCP recovery/removal = %#v, want %#v", got, want[:3])
	}
	SetWorkspaceWarnings(nil)
	if got := snapshot(); len(got) != 0 {
		t.Fatalf("historical transcript warnings must not be projected: %#v", got)
	}
	if got := transcriptWarnings(t); !slices.Equal(got, []string{"historical warning"}) {
		t.Fatalf("only the explicit warning should reach the transcript, got %q", got)
	}
}
