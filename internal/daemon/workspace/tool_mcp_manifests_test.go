package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/warp"
)

// toggleToolManifest is a minimal workspace Tool manifest whose body and front
// matter must survive a spec.enabled patch.
const toggleToolManifest = `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: linter
  description: Repo linter tool
spec:
  annotations:
    isReadOnly: true
---
Run the repo linter.
`

// writeToggleToolManifest drops the linter manifest into the workspace tier.
func writeToggleToolManifest(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, ".agents", "tools", "linter.tool.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(toggleToolManifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

// openToggleTestWorkspace opens root as a workspace with the embedded built-in
// tool manifests mounted, so disabling a built-in name exercises the
// WORKSPACE.md policy path.
func openToggleTestWorkspace(t *testing.T, root string) *Workspace {
	t.Helper()
	ws, err := Open(context.Background(), root, WithBuiltinFS(builtinToolManifests(builtinToolNames)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return ws
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// workspaceManifestWithToolExcludes builds a WORKSPACE.md front matter document
// excluding the given tool names.
func workspaceManifestWithToolExcludes(names ...string) string {
	var b strings.Builder
	b.WriteString("---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: policies-ws\nspec:\n  policies:\n    tools:\n      exclude:\n")
	for _, n := range names {
		b.WriteString("        - \"" + n + "\"\n")
	}
	b.WriteString("---\n")
	return b.String()
}

func TestSetToolEnabledPatchesOwnWorkspaceManifest(t *testing.T) {
	setupTestXDG(t)
	root := t.TempDir()
	initWorkspaceTier(t, root)
	toolPath := writeToggleToolManifest(t, root)
	ws := openToggleTestWorkspace(t, root)

	if err := ws.SetToolEnabled("linter", false); err != nil {
		t.Fatalf("SetToolEnabled(false): %v", err)
	}
	if err := ws.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	src := readTestFile(t, toolPath)
	if !strings.Contains(src, "enabled: false") {
		t.Fatalf("manifest missing enabled: false:\n%s", src)
	}
	if !strings.Contains(src, "Run the repo linter.") {
		t.Fatalf("manifest body was not preserved:\n%s", src)
	}
	if !strings.Contains(src, "description: Repo linter tool") {
		t.Fatalf("manifest front matter was not preserved:\n%s", src)
	}
	rec, ok := ws.ToolRecord("linter")
	if !ok {
		t.Fatal("linter record missing after disable")
	}
	if rec.Available {
		t.Error("tool still available after manifest disable")
	}
	if rec.DisabledBy != api.DisabledByManifest {
		t.Errorf("disabled by = %q, want %q", rec.DisabledBy, api.DisabledByManifest)
	}
	if snap, ok := ws.Catalog().GetToolSnapshot("linter"); !ok || snap.Enabled {
		t.Errorf("snapshot should report Enabled=false: %+v ok=%v", snap, ok)
	}

	if err := ws.SetToolEnabled("linter", true); err != nil {
		t.Fatalf("SetToolEnabled(true): %v", err)
	}
	if err := ws.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	src = readTestFile(t, toolPath)
	if strings.Contains(src, "enabled:") {
		t.Fatalf("enabled key should be dropped on re-enable:\n%s", src)
	}
	if rec, ok := ws.ToolRecord("linter"); !ok || !rec.Available {
		t.Fatalf("tool should be available after re-enable: %+v ok=%v", rec, ok)
	}
}

func TestSetToolEnabledBuiltInUsesWorkspacePolicy(t *testing.T) {
	setupTestXDG(t)
	root := t.TempDir()
	initWorkspaceTier(t, root)
	ws := openToggleTestWorkspace(t, root)

	if err := ws.SetToolEnabled("web_search", false); err != nil {
		t.Fatalf("SetToolEnabled(built-in, false): %v", err)
	}
	wsSrc := readTestFile(t, filepath.Join(root, "WORKSPACE.md"))
	if !strings.Contains(wsSrc, "exclude:") || !strings.Contains(wsSrc, "web_search") {
		t.Fatalf("WORKSPACE.md missing tools exclusion:\n%s", wsSrc)
	}
	if err := ws.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if ws.Catalog().IsToolAvailable("web_search") {
		t.Error("excluded built-in should not be available")
	}
	snap, ok := ws.Catalog().GetToolSnapshot("web_search")
	if !ok {
		t.Fatal("excluded built-in missing from snapshots")
	}
	if snap.Enabled {
		t.Error("snapshot should report Enabled=false")
	}
	if snap.DisabledBy != api.DisabledByWorkspacePolicy {
		t.Errorf("disabled by = %q, want %q", snap.DisabledBy, api.DisabledByWorkspacePolicy)
	}
	if strings.Contains(strings.Join(ws.Catalog().ToolNames(), ","), "web_search") {
		t.Fatal("excluded tool must not appear in ToolNames()")
	}

	if err := ws.SetToolEnabled("web_search", true); err != nil {
		t.Fatalf("SetToolEnabled(built-in, true): %v", err)
	}
	if err := ws.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !ws.Catalog().IsToolAvailable("web_search") {
		t.Error("built-in should be available after exclusion removal")
	}
	wsSrc = readTestFile(t, filepath.Join(root, "WORKSPACE.md"))
	if strings.Contains(wsSrc, "web_search") {
		t.Fatalf("WORKSPACE.md should no longer mention web_search:\n%s", wsSrc)
	}
}

func TestSetToolEnabledUnknownName(t *testing.T) {
	setupTestXDG(t)
	root := t.TempDir()
	initWorkspaceTier(t, root)
	ws := openToggleTestWorkspace(t, root)
	if err := ws.SetToolEnabled("nope", false); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("want ErrToolNotFound, got %v", err)
	}
}

// TestSetToolEnabledRejectsMCPToolNames verifies per-tool toggles of discovered
// MCP tools are rejected in favour of toggling the parent server.
func TestSetToolEnabledRejectsMCPToolNames(t *testing.T) {
	ws := openMCPSandbox(t, "")
	def, ok := ws.MCPDef("notes")
	if !ok {
		t.Fatal("notes MCP definition missing")
	}
	if err := ws.SyncMCPTools(def, []*warp.Tool{discoveredTool("notes__echo", "Echo text back")}); err != nil {
		t.Fatalf("SyncMCPTools: %v", err)
	}
	if !ws.Catalog().IsToolAvailable("notes__echo") {
		t.Fatal("published MCP tool should be available while the server is enabled")
	}
	err := ws.SetToolEnabled("notes__echo", false)
	if err == nil || !strings.Contains(err.Error(), "notes") {
		t.Fatalf("want MCP-tool rejection naming parent server, got %v", err)
	}
}

func TestSetMCPEnabledManifestRoundTrip(t *testing.T) {
	ws := openMCPSandbox(t, "")
	def, ok := ws.MCPDef("notes")
	if !ok {
		t.Fatal("notes MCP definition missing")
	}
	if err := ws.SyncMCPTools(def, []*warp.Tool{discoveredTool("notes__echo", "Echo text back")}); err != nil {
		t.Fatalf("SyncMCPTools: %v", err)
	}

	if err := ws.SetMCPEnabled("notes", false); err != nil {
		t.Fatalf("SetMCPEnabled(false): %v", err)
	}
	src := readTestFile(t, def.GetPath())
	if !strings.Contains(src, "enabled: false") {
		t.Fatalf("manifest missing enabled: false:\n%s", src)
	}
	if err := ws.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if ws.Catalog().IsMCPServerAvailable("notes") {
		t.Error("disabled MCP should not be available")
	}
	snap, ok := ws.Catalog().GetMCPSnapshot("notes")
	if !ok {
		t.Fatal("disabled MCP missing from MCPSnapshots()")
	}
	if snap.Enabled {
		t.Error("snapshot should report Enabled=false")
	}
	if snap.DisabledBy != api.DisabledByManifest {
		t.Errorf("disabled by = %q, want %q", snap.DisabledBy, api.DisabledByManifest)
	}
	if snap.Def == nil || snap.Def.Spec.IsEnabled() {
		t.Error("snapshot definition should report disabled spec")
	}
	for _, m := range ws.MCPDefinitions() {
		if m.Metadata.Name == "notes" {
			t.Fatal("disabled MCP must not appear in MCPDefinitions()")
		}
	}
	if strings.Contains(strings.Join(ws.Catalog().ToolNames(), ","), "notes__echo") {
		t.Fatal("tools of a disabled server must not remain effective")
	}

	if err := ws.SetMCPEnabled("notes", true); err != nil {
		t.Fatalf("SetMCPEnabled(true): %v", err)
	}
	src = readTestFile(t, def.GetPath())
	if strings.Contains(src, "enabled:") {
		t.Fatalf("enabled key should be dropped on re-enable:\n%s", src)
	}
	if err := ws.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !ws.Catalog().IsMCPServerAvailable("notes") {
		t.Error("MCP should be available after re-enable")
	}
}

func TestSetMCPEnabledPolicyExclusionRoundTrip(t *testing.T) {
	ws := openMCPSandbox(t, "")
	def, ok := ws.MCPDef("notes")
	if !ok {
		t.Fatal("notes MCP definition missing")
	}

	if err := ws.SetWorkspaceMCPExcluded("notes", true); err != nil {
		t.Fatalf("SetWorkspaceMCPExcluded: %v", err)
	}
	if err := ws.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if ws.Catalog().IsMCPServerAvailable("notes") {
		t.Error("policy-excluded MCP should not be available")
	}
	snap, ok := ws.Catalog().GetMCPSnapshot("notes")
	if !ok {
		t.Fatal("policy-excluded MCP missing from MCPSnapshots()")
	}
	if snap.DisabledBy != api.DisabledByWorkspacePolicy {
		t.Errorf("disabled by = %q, want %q", snap.DisabledBy, api.DisabledByWorkspacePolicy)
	}

	// Enabling a policy-excluded server removes the WORKSPACE.md entry and
	// leaves its manifest untouched.
	if err := ws.SetMCPEnabled("notes", true); err != nil {
		t.Fatalf("SetMCPEnabled(true): %v", err)
	}
	src := readTestFile(t, def.GetPath())
	if strings.Contains(src, "enabled:") {
		t.Fatalf("manifest should stay untouched:\n%s", src)
	}
	if err := ws.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !ws.Catalog().IsMCPServerAvailable("notes") {
		t.Error("MCP should be available after policy removal")
	}
}

func TestSetMCPEnabledUnknown(t *testing.T) {
	setupTestXDG(t)
	root := t.TempDir()
	initWorkspaceTier(t, root)
	ws := openToggleTestWorkspace(t, root)
	if err := ws.SetMCPEnabled("nope", false); !errors.Is(err, ErrMCPNotFound) {
		t.Fatalf("want ErrMCPNotFound, got %v", err)
	}
}

// TestSetWorkspaceExclusionRewritesInferredDoc covers a repo whose WORKSPACE.md
// is instructions-only: the policy edit rewrites it as a full workspace
// definition that keeps the instructions.
func TestSetWorkspaceExclusionRewritesInferredDoc(t *testing.T) {
	setupTestXDG(t)
	root := t.TempDir()
	wsPath := filepath.Join(root, "WORKSPACE.md")
	writeTestFile(t, wsPath, "# My project\n\nBuild with make.\n")
	ws := openToggleTestWorkspace(t, root)

	if err := ws.SetWorkspaceToolExcluded("web_search", true); err != nil {
		t.Fatalf("SetWorkspaceToolExcluded: %v", err)
	}
	src := readTestFile(t, wsPath)
	if !strings.Contains(src, "kind: Workspace") {
		t.Fatalf("expected full workspace definition:\n%s", src)
	}
	if !strings.Contains(src, "Build with make.") {
		t.Fatalf("original instructions were not preserved:\n%s", src)
	}
	if !strings.Contains(src, "web_search") {
		t.Fatalf("exclusion missing:\n%s", src)
	}
}

// TestSetWorkspaceExclusionCreatesMissingDoc verifies WORKSPACE.md is created
// when a repo has none at all.
func TestSetWorkspaceExclusionCreatesMissingDoc(t *testing.T) {
	setupTestXDG(t)
	root := t.TempDir()
	ws := openToggleTestWorkspace(t, root)
	if err := ws.SetWorkspaceToolExcluded("bash", true); err != nil {
		t.Fatalf("SetWorkspaceToolExcluded: %v", err)
	}
	src := readTestFile(t, filepath.Join(root, "WORKSPACE.md"))
	if !strings.Contains(src, "exclude:") || !strings.Contains(src, "bash") {
		t.Fatalf("fresh WORKSPACE.md missing exclusion:\n%s", src)
	}
}

// TestSetToolEnabledKeepsMatchingGlob verifies un-excluding a name that is
// still covered by a broader pattern fails loudly instead of appearing to
// succeed.
func TestSetToolEnabledKeepsMatchingGlob(t *testing.T) {
	setupTestXDG(t)
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "WORKSPACE.md"), workspaceManifestWithToolExcludes("web*"))
	ws := openToggleTestWorkspace(t, root)
	if ws.Catalog().IsToolAvailable("web_search") {
		t.Fatal("web* glob should exclude web_search")
	}
	err := ws.SetToolEnabled("web_search", true)
	if !errors.Is(err, ErrNoSafePolicyEdit) {
		t.Fatalf("want ErrNoSafePolicyEdit, got %v", err)
	}
	if !strings.Contains(err.Error(), "web*") {
		t.Fatalf("error should name the matching pattern: %v", err)
	}
}

// TestAvailabilityPolicyPatterns checks the catalog's mirrored policy matcher
// for the identities a WORKSPACE.md exclusion pattern is tried against: short,
// namespace-qualified, and fully qualified names.
func TestAvailabilityPolicyPatterns(t *testing.T) {
	setupTestXDG(t)
	cases := []struct {
		pattern  string
		toolName string
		excluded bool
	}{
		{"web_search", "web_search", true},
		{"web*", "web_search", true},
		{"web_f?tch", "web_fetch", true},
		{"web_fetch", "web_search", false},
		{"system/web_search", "web_search", true},
		{"system/Tool/web_search", "web_search", true},
		{"other*", "web_search", false},
	}
	for _, tc := range cases {
		t.Run(tc.pattern+"~"+tc.toolName, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, filepath.Join(root, "WORKSPACE.md"), workspaceManifestWithToolExcludes(tc.pattern))
			ws := openToggleTestWorkspace(t, root)
			if got := ws.Catalog().IsToolAvailable(tc.toolName); got == tc.excluded {
				t.Fatalf("pattern %q: IsToolAvailable(%q)=%v, want excluded=%v",
					tc.pattern, tc.toolName, got, tc.excluded)
			}
			snap, ok := ws.Catalog().GetToolSnapshot(tc.toolName)
			if !ok {
				t.Fatalf("pattern %q: snapshot for %q missing from the disabled-inclusive listing",
					tc.pattern, tc.toolName)
			}
			if snap.Enabled == tc.excluded {
				t.Fatalf("pattern %q: snapshot Enabled=%v should disagree with excluded=%v",
					tc.pattern, snap.Enabled, tc.excluded)
			}
			if tc.excluded && snap.DisabledBy != api.DisabledByWorkspacePolicy {
				t.Fatalf("pattern %q: disabled by = %q, want %q",
					tc.pattern, snap.DisabledBy, api.DisabledByWorkspacePolicy)
			}
		})
	}
}

// TestToolStatusesClassifiesDisabled verifies manifest-disabled and
// policy-excluded tools surface through ToolStatuses as disabled rather than
// falling through to inactive/denied.
func TestToolStatusesClassifiesDisabled(t *testing.T) {
	setupTestXDG(t)
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "WORKSPACE.md"), workspaceManifestWithToolExcludes("web_search"))
	toolPath := filepath.Join(root, ".agents", "tools", "linter.tool.md")
	if err := os.MkdirAll(filepath.Dir(toolPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeTestFile(t, toolPath, `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: linter
  description: Disabled linter
spec:
  enabled: false
  annotations:
    isReadOnly: true
---
Body.
`)
	ws := openToggleTestWorkspace(t, root)

	statuses := ws.Catalog().ToolStatuses(nil)
	if statuses["linter"] != api.ToolStatusDisabled {
		t.Errorf("manifest-disabled tool status = %q, want %q", statuses["linter"], api.ToolStatusDisabled)
	}
	if statuses["web_search"] != api.ToolStatusDisabled {
		t.Errorf("policy-excluded tool status = %q, want %q", statuses["web_search"], api.ToolStatusDisabled)
	}
	if statuses["view"] != api.ToolStatusEager {
		t.Errorf("available built-in status = %q, want %q", statuses["view"], api.ToolStatusEager)
	}
}

// TestInitializeWritesPolicyExclusions verifies onboarding-time tool and MCP
// choices land in WORKSPACE.md policies rather than permissions.json.
func TestInitializeWritesPolicyExclusions(t *testing.T) {
	setupTestXDG(t)
	root := t.TempDir()
	if err := Initialize(context.Background(), root, InitOptions{
		ProjectName:   "policies",
		DisabledTools: []string{"web_search"},
		DisabledMCPs:  []string{"github"},
	}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	src := readTestFile(t, filepath.Join(root, "WORKSPACE.md"))
	if !strings.Contains(src, "web_search") {
		t.Fatalf("WORKSPACE.md missing tool exclusion:\n%s", src)
	}
	if !strings.Contains(src, "github") {
		t.Fatalf("WORKSPACE.md missing MCP exclusion:\n%s", src)
	}
}

func TestDisabledToolSearchHidesDeferredTools(t *testing.T) {
	setupTestXDG(t)
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "WORKSPACE.md"), workspaceManifestWithToolExcludes("tool_search"))
	ws := openToggleTestWorkspace(t, root)
	cat := ws.Catalog()
	resolved, err := cat.ResolveAgent(cat.DefaultAgentName())
	if err != nil || resolved == nil {
		t.Fatalf("resolve default agent: %v", err)
	}
	part := cat.PartitionAgentTools(resolved)
	if slices.Contains(part.Eager, "tool_search") || len(part.Deferred) != 0 {
		t.Fatalf("disabled tool_search left discovery available: %+v", part)
	}
	if got := cat.BuildDeferredToolsCatalog(resolved); got != nil {
		t.Fatalf("disabled tool_search left a deferred catalog: %+v", got)
	}
}

func TestUserToolToggleIsScopedToWorkspace(t *testing.T) {
	setupTestXDG(t)
	userTools, err := xdg.SubConfigDir("agents", "tools")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(userTools, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(userTools, "linter.tool.md")
	writeTestFile(t, manifest, toggleToolManifest)

	root := t.TempDir()
	ws := openToggleTestWorkspace(t, root)
	if err := ws.SetToolEnabled("linter", false); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, manifest); got != toggleToolManifest {
		t.Fatalf("workspace toggle changed the shared manifest:\n%s", got)
	}
	if err := ws.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}
	if ws.Catalog().IsToolAvailable("linter") {
		t.Fatal("workspace policy did not disable the user tool")
	}
	if other := openToggleTestWorkspace(t, t.TempDir()); !other.Catalog().IsToolAvailable("linter") {
		t.Fatal("workspace policy affected another repository")
	}
}
