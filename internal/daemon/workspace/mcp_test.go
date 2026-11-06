package workspace

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/warp"
)

// openMCPSandbox writes a workspace declaring one MCP server with a policy that
// blocks delete_* tools, and opens it.
func openMCPSandbox(t *testing.T, policies string) *Workspace {
	t.Helper()
	setupTestXDG(t)

	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	wsManifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: mcp-sync-ws
spec:
  defaultAgent: main
---
Workspace instructions.
`
	if err := os.WriteFile(filepath.Join(dir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	mcpManifest := `apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: notes
  description: Notes MCP server
spec:
  type: stdio
  command: ["notes-mcp"]
` + policies
	if err := os.WriteFile(filepath.Join(agentsDir, "notes.mcp.yaml"), []byte(mcpManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return ws
}

// discoveredTool builds a tool as the MCP manager reports it after discovery, which
// carries the materialized "<server>__<tool>" name.
func discoveredTool(name, description string) *warp.Tool {
	return &warp.Tool{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindTool,
			Metadata:   warp.Metadata{Name: name, Description: description},
		},
		Spec: warp.ToolSpec{InputSchema: map[string]any{"type": "object"}},
	}
}

// TestSyncMCPToolsPublishesAndBlocks verifies the workspace seam registers a live
// server's permitted tools so agents can reach them, records the policy-blocked
// remainder as denied instead of silently dropping it, and clears both when the
// server disconnects.
func TestSyncMCPToolsPublishesAndBlocks(t *testing.T) {
	ws := openMCPSandbox(t, `  policies:
    tools:
      exclude: ["delete_*"]
`)
	def, ok := ws.MCPDef("notes")
	if !ok {
		t.Fatalf("MCP definition missing: %+v", ws.Catalog().MCPSnapshots())
	}

	raw := []*warp.Tool{
		discoveredTool("notes__echo", "Echo text back"),
		discoveredTool("notes__delete_everything", "Delete everything"),
	}
	if err := ws.SyncMCPTools(def, raw); err != nil {
		t.Fatalf("SyncMCPTools: %v", err)
	}

	names := ws.Catalog().ToolNames()
	if !slices.Contains(names, "notes__echo") {
		t.Errorf("permitted tool must be effective: %v", names)
	}
	if slices.Contains(names, "notes__delete_everything") {
		t.Errorf("policy-blocked tool must not be effective: %v", names)
	}

	// The blocked tool must still be visible as denied so the UI can explain it.
	denied := ws.Catalog().DeniedMCPToolSnapshots()
	if len(denied) != 1 || denied[0].Name != "notes__delete_everything" {
		t.Fatalf("DeniedMCPToolSnapshots() = %+v", denied)
	}
	if denied[0].Status != api.ToolStatusDenied {
		t.Errorf("blocked tool status = %q, want %q", denied[0].Status, api.ToolStatusDenied)
	}

	// The refresh must have run, so the discovered tool is presented as an MCP tool.
	snap, ok := ws.Catalog().GetToolSnapshot("notes__echo")
	if !ok {
		t.Fatalf("permitted tool missing from catalog snapshots: %+v", ws.Catalog().ToolSnapshots())
	}
	if snap.Scope != "MCP (notes)" {
		t.Errorf("discovered tool scope = %q, want %q", snap.Scope, "MCP (notes)")
	}

	// Disconnecting clears both the published and the blocked tools.
	if err := ws.SyncMCPTools(def, nil); err != nil {
		t.Fatalf("SyncMCPTools(nil): %v", err)
	}
	if names := ws.Catalog().ToolNames(); slices.Contains(names, "notes__echo") {
		t.Errorf("clearing a server's tools must unregister them: %v", names)
	}
	if got := ws.Catalog().DeniedMCPToolSnapshots(); len(got) != 0 {
		t.Errorf("clearing a server's tools must clear its blocked tools, got %+v", got)
	}
}

// TestSyncMCPToolsWithoutPoliciesPublishesEverything verifies a server declaring no
// tool policy contributes all of its discovered tools and records nothing as
// denied, so a plain MCP server needs no configuration to be useful.
func TestSyncMCPToolsWithoutPoliciesPublishesEverything(t *testing.T) {
	ws := openMCPSandbox(t, "")
	def, ok := ws.MCPDef("notes")
	if !ok {
		t.Fatal("MCP definition missing")
	}

	raw := []*warp.Tool{
		discoveredTool("notes__echo", "Echo text back"),
		discoveredTool("notes__delete_everything", "Delete everything"),
	}
	if err := ws.SyncMCPTools(def, raw); err != nil {
		t.Fatalf("SyncMCPTools: %v", err)
	}

	names := ws.Catalog().ToolNames()
	for _, want := range []string{"notes__echo", "notes__delete_everything"} {
		if !slices.Contains(names, want) {
			t.Errorf("tool %q must be effective without a policy: %v", want, names)
		}
	}
	if got := ws.Catalog().DeniedMCPToolSnapshots(); len(got) != 0 {
		t.Errorf("no tool should be blocked, got %+v", got)
	}
}

// TestSyncMCPToolsRejectsMissingDefinition verifies the seam fails loudly for a nil
// definition or an unknown server rather than mutating another server's tools.
func TestSyncMCPToolsRejectsMissingDefinition(t *testing.T) {
	ws := openMCPSandbox(t, "")

	if err := ws.SyncMCPTools(nil, nil); err == nil {
		t.Error("expected an error for a nil MCP definition")
	}

	unknown := &warp.MCP{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindMCP,
			Metadata:   warp.Metadata{Name: "not-declared"},
		},
	}
	err := ws.SyncMCPTools(unknown, []*warp.Tool{discoveredTool("not-declared__x", "x")})
	if err == nil || !strings.Contains(err.Error(), "not-declared") {
		t.Errorf("expected an error naming the unknown server, got %v", err)
	}
}

// TestRenderOptionsSuppliesWorkspaceContext verifies the render context handed to
// the MCP manager carries the workspace definition, so endpoint and command
// templates referencing workspace fields resolve after a reload.
func TestRenderOptionsSuppliesWorkspaceContext(t *testing.T) {
	ws := openMCPSandbox(t, "")

	opts := ws.RenderOptions()
	if opts.Workspace == nil {
		t.Fatalf("render options must carry the workspace definition: %+v", opts)
	}
	if opts.Workspace.Def == nil {
		t.Fatalf("render options must carry the parsed WORKSPACE.md: %+v", opts.Workspace)
	}
	if got := opts.Workspace.Def.Metadata.Name; got != "mcp-sync-ws" {
		t.Errorf("rendered workspace name = %q, want %q", got, "mcp-sync-ws")
	}
	if opts.Workspace.RootPath == "" {
		t.Errorf("rendered workspace root path must be set so templates resolve relative paths")
	}
}

// TestMCPServiceAttachDetach verifies a workspace reports no MCP runtime until one
// is attached, and that attaching nil detaches it so MCP callers can fall back to
// manifest-only behavior.
func TestMCPServiceAttachDetach(t *testing.T) {
	ws := openMCPSandbox(t, "")

	if ws.MCP() != nil {
		t.Fatalf("a freshly opened workspace must have no MCP runtime")
	}
	ws.SetMCPService(stubMCPService{})
	if ws.MCP() == nil {
		t.Fatalf("SetMCPService must attach the runtime")
	}
	ws.SetMCPService(nil)
	if ws.MCP() != nil {
		t.Errorf("SetMCPService(nil) must detach the runtime")
	}
}

// stubMCPService satisfies MCPService without behavior.
type stubMCPService struct{}

func (stubMCPService) ListMCPs(context.Context) []api.MCPServer { return nil }

func (stubMCPService) RestartMCP(context.Context, string) (api.MCPServer, error) {
	return api.MCPServer{}, nil
}

func (stubMCPService) AuthenticateMCP(context.Context, string) (api.MCPAuthResponse, error) {
	return api.MCPAuthResponse{}, nil
}

func (stubMCPService) SubmitMCPAuthCallback(context.Context, string, string) error {
	return nil
}

func (stubMCPService) GetMCPPrompt(context.Context, string, string, map[string]string, bool) (api.MCPPromptResponse, error) {
	return api.MCPPromptResponse{}, nil
}

func (stubMCPService) ReadMCPResource(context.Context, string, api.MCPResourceRequest) (api.MCPResourceResponse, error) {
	return api.MCPResourceResponse{}, nil
}

func (stubMCPService) AllowPolicyItem(context.Context, string, api.MCPAllowPolicyRequest) (api.MCPServer, error) {
	return api.MCPServer{}, nil
}
