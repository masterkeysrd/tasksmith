package daemon

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

// seedWorkspaceTool drops a Tool manifest into the workspace tier before any
// API call so the lazily-resolved workspace runtime loads it on first use.
func seedWorkspaceTool(t *testing.T, wsDir, name string) string {
	t.Helper()
	dir := filepath.Join(wsDir, ".agents", "tools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".tool.md")
	body := "---\napiVersion: warp/v1alpha1\nkind: Tool\nmetadata:\n  name: " + name +
		"\n  description: Toggle test tool\nspec:\n  annotations:\n    isReadOnly: true\n---\nToggle test body.\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// seedWorkspaceMCP drops an MCP manifest into the workspace tier.
func seedWorkspaceMCP(t *testing.T, wsDir, name string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(wsDir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(wsDir, ".agents", name+".mcp.yaml")
	body := "apiVersion: warp/v1alpha1\nkind: MCP\nmetadata:\n  name: " + name +
		"\n  description: Toggle test MCP server\nspec:\n  type: stdio\n  command: [\"" + name + "-mcp\"]\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func findAPITool(list []api.Tool, name string) (api.Tool, bool) {
	for _, tool := range list {
		if tool.Name == name {
			return tool, true
		}
	}
	return api.Tool{}, false
}

func findAPIMCPServer(list []api.MCPServer, name string) (api.MCPServer, bool) {
	for _, server := range list {
		if server.Name == name {
			return server, true
		}
	}
	return api.MCPServer{}, false
}

func TestWorkspaceToolsAPIListsAndToggles(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()
	path := seedWorkspaceTool(t, wsDir, "linter")

	list, err := cli.ListTools(ctx, "")
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	tool, ok := findAPITool(list, "linter")
	if !ok {
		t.Fatalf("linter missing from listing: %+v", list)
	}
	if !tool.Enabled {
		t.Error("linter enabled = false, want true (spec.enabled omitted defaults true)")
	}

	list, err = cli.SetToolEnabled(ctx, "linter", false)
	if err != nil {
		t.Fatalf("SetToolEnabled(false): %v", err)
	}
	tool, _ = findAPITool(list, "linter")
	if tool.Enabled {
		t.Error("after disable: enabled = true, want false")
	}
	if tool.DisabledBy != api.DisabledByManifest {
		t.Errorf("after disable: disabled by = %q, want %q", tool.DisabledBy, api.DisabledByManifest)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "enabled: false") {
		t.Fatalf("manifest not patched:\n%s", data)
	}

	if _, err := cli.SetToolEnabled(ctx, "ghosttool", false); err == nil {
		t.Fatal("expected error for unknown tool")
	} else {
		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
			t.Fatalf("unknown tool error = %v, want 404 APIError", err)
		}
	}

	list, err = cli.SetToolEnabled(ctx, "linter", true)
	if err != nil {
		t.Fatalf("SetToolEnabled(true): %v", err)
	}
	if tool, _ := findAPITool(list, "linter"); !tool.Enabled {
		t.Error("after re-enable: enabled = false, want true")
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "enabled: false") {
		t.Fatalf("enabled: false survived re-enable:\n%s", data)
	}
}

// TestWorkspaceToolsAPIBuiltInDisableUsesPolicy verifies disabling a tool with
// no workspace-writable manifest lands as a WORKSPACE.md exclusion rather than
// patching a global or embedded file.
func TestWorkspaceToolsAPIBuiltInDisableUsesPolicy(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()

	list, err := cli.SetToolEnabled(ctx, "web_search", false)
	if err != nil {
		t.Fatalf("SetToolEnabled(web_search, false): %v", err)
	}
	tool, ok := findAPITool(list, "web_search")
	if !ok {
		t.Fatalf("web_search missing from listing: %+v", list)
	}
	if tool.Enabled {
		t.Error("web_search still enabled after disable")
	}
	if tool.DisabledBy != api.DisabledByWorkspacePolicy {
		t.Errorf("disabled by = %q, want %q", tool.DisabledBy, api.DisabledByWorkspacePolicy)
	}
	data, err := os.ReadFile(filepath.Join(wsDir, "WORKSPACE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "web_search") {
		t.Fatalf("WORKSPACE.md missing exclusion:\n%s", data)
	}

	if _, err := cli.SetToolEnabled(ctx, "web_search", true); err != nil {
		t.Fatalf("SetToolEnabled(web_search, true): %v", err)
	}
	data, _ = os.ReadFile(filepath.Join(wsDir, "WORKSPACE.md"))
	if strings.Contains(string(data), "web_search") {
		t.Fatalf("WORKSPACE.md still mentions web_search after re-enable:\n%s", data)
	}
}

func TestWorkspaceMCPsAPITogglesAndMergesDisabled(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()
	path := seedWorkspaceMCP(t, wsDir, "notes")

	list, err := cli.ListMCPs(ctx)
	if err != nil {
		t.Fatalf("ListMCPs: %v", err)
	}
	server, ok := findAPIMCPServer(list, "notes")
	if !ok {
		t.Fatalf("notes missing from listing: %+v", list)
	}
	if !server.Enabled {
		t.Error("notes enabled = false, want true")
	}

	list, err = cli.SetMCPEnabled(ctx, "notes", false)
	if err != nil {
		t.Fatalf("SetMCPEnabled(false): %v", err)
	}
	server, _ = findAPIMCPServer(list, "notes")
	if server.Enabled {
		t.Error("after disable: enabled = true, want false")
	}
	if server.DisabledBy != api.DisabledByManifest {
		t.Errorf("after disable: disabled by = %q, want %q", server.DisabledBy, api.DisabledByManifest)
	}
	if len(server.Tools) != 0 {
		t.Errorf("disabled server exposes %d tools, want 0", len(server.Tools))
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "enabled: false") {
		t.Fatalf("manifest not patched:\n%s", data)
	}

	// A follow-up plain listing must still surface the disabled server so the
	// UI can offer re-enabling it.
	list, err = cli.ListMCPs(ctx)
	if err != nil {
		t.Fatalf("ListMCPs after disable: %v", err)
	}
	if server, ok := findAPIMCPServer(list, "notes"); !ok || server.Enabled {
		t.Fatalf("disabled server missing or still enabled in plain listing: %+v ok=%v", server, ok)
	}

	if _, err := cli.RestartMCP(ctx, "notes"); err == nil {
		t.Fatal("restart succeeded for a disabled server")
	} else {
		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden {
			t.Fatalf("disabled server restart = %v, want 403 APIError", err)
		}
	}

	if _, err := cli.SetMCPEnabled(ctx, "ghostserver", false); err == nil {
		t.Fatal("expected error for unknown MCP server")
	} else {
		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
			t.Fatalf("unknown server error = %v, want 404 APIError", err)
		}
	}

	if _, err := cli.SetMCPEnabled(ctx, "notes", true); err != nil {
		t.Fatalf("SetMCPEnabled(true): %v", err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "enabled: false") {
		t.Fatalf("enabled: false survived re-enable:\n%s", data)
	}
}
