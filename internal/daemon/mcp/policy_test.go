package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/warp"
)

// writeManifest writes an MCP manifest into dir and returns the definition the
// workspace loader would produce for it, so tests exercise the same parsing the
// daemon does.
func writeManifest(t *testing.T, name, content string) *warp.MCP {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := warp.Parse(path, content)
	if err != nil {
		t.Fatalf("warp.Parse(%s): %v", name, err)
	}
	def, ok := result.Resource.(*warp.MCP)
	if !ok {
		t.Fatalf("parsed resource is %T, want *warp.MCP", result.Resource)
	}
	def.Path = path
	def.Directory = dir
	return def
}

const toolsExcludeManifest = `---
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: github
spec:
  type: stdio
  command: ["server"]
  policies:
    # Blocking destructive operations.
    tools:
      exclude:
        - delete_issue
        - close_*
`

func TestAllowPolicyItemRemovesExactExclude(t *testing.T) {
	def := writeManifest(t, "github.mcp.yaml", toolsExcludeManifest)

	change, err := AllowPolicyItem(def, "tool", "delete_issue")
	if err != nil {
		t.Fatalf("AllowPolicyItem: %v", err)
	}
	if change.RemovedExclude != "delete_issue" {
		t.Errorf("change = %+v, want removal of exclude delete_issue", change)
	}
	if change.Included {
		t.Errorf("value must not also be added to include when no include list is active: %+v", change)
	}

	updated := readFile(t, def.GetPath())
	if strings.Contains(updated, "delete_issue") {
		t.Errorf("edited manifest still mentions delete_issue:\n%s", updated)
	}
	if !strings.Contains(updated, "close_*") {
		t.Errorf("edited manifest must keep unrelated excludes:\n%s", updated)
	}
	if !strings.Contains(updated, "Blocking destructive operations.") {
		t.Errorf("edited manifest must preserve comments:\n%s", updated)
	}

	reloaded, err := parseManifest(def.GetPath(), updated)
	if err != nil {
		t.Fatalf("re-parse edited manifest: %v", err)
	}
	if !reloaded.AllowsTool("delete_issue") {
		t.Error("delete_issue must be allowed after the edit")
	}
	if reloaded.AllowsTool("close_pull_request") {
		t.Error("close_* must keep blocking close_pull_request")
	}
}

const toolsIncludeManifest = `---
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: github
spec:
  type: stdio
  command: ["server"]
  policies:
    tools:
      include:
        - read_issue
`

func TestAllowPolicyItemAddsToActiveIncludeList(t *testing.T) {
	def := writeManifest(t, "github.mcp.yaml", toolsIncludeManifest)

	change, err := AllowPolicyItem(def, "tool", "write_issue")
	if err != nil {
		t.Fatalf("AllowPolicyItem: %v", err)
	}
	if !change.Included {
		t.Errorf("change = %+v, want value appended to include", change)
	}

	updated := readFile(t, def.GetPath())
	if !strings.Contains(updated, "read_issue") {
		t.Errorf("edited manifest must keep the existing include entry:\n%s", updated)
	}

	reloaded, err := parseManifest(def.GetPath(), updated)
	if err != nil {
		t.Fatalf("re-parse edited manifest: %v", err)
	}
	if !reloaded.AllowsTool("write_issue") {
		t.Error("write_issue must be allowed after the edit")
	}
}

func TestAllowPolicyItemRemovesExcludeAndAddsToActiveIncludeList(t *testing.T) {
	const manifest = `---
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: postgres
spec:
  type: stdio
  command: ["server"]
  policies:
    tools:
      include:
        - read_query
        - list_tables
      exclude:
        - write_query
`
	def := writeManifest(t, "postgres.mcp.yaml", manifest)

	change, err := AllowPolicyItem(def, "tool", "write_query")
	if err != nil {
		t.Fatalf("AllowPolicyItem: %v", err)
	}
	if change.RemovedExclude != "write_query" || !change.Included {
		t.Errorf("change = %+v, want RemovedExclude=write_query and Included=true", change)
	}

	reloaded, err := parseManifest(def.GetPath(), readFile(t, def.GetPath()))
	if err != nil {
		t.Fatalf("re-parse edited manifest: %v", err)
	}
	if !reloaded.AllowsTool("write_query") {
		t.Error("write_query must be allowed after removing from exclude and adding to include")
	}
}

// An exclude pattern cannot be lifted by listing the value, because exclusion runs
// after inclusion, and deleting the pattern would expose everything else it covers.
func TestAllowPolicyItemRefusesToWidenWildcardExclude(t *testing.T) {
	const manifest = `---
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: github
spec:
  type: stdio
  command: ["server"]
  policies:
    tools:
      include:
        - read_issue
      exclude:
        - delete_*
`
	def := writeManifest(t, "github.mcp.yaml", manifest)
	before := readFile(t, def.GetPath())

	_, err := AllowPolicyItem(def, "tool", "delete_issue")
	if !errors.Is(err, ErrNoSafePolicyEdit) {
		t.Fatalf("AllowPolicyItem: got %v, want ErrNoSafePolicyEdit", err)
	}
	if !strings.Contains(err.Error(), "delete_*") {
		t.Errorf("error must name the blocking pattern, got: %v", err)
	}
	if got := readFile(t, def.GetPath()); got != before {
		t.Errorf("manifest must be untouched when no safe edit exists:\n%s", got)
	}
}

func TestAllowPolicyItemAlreadyAllowedIsUnchanged(t *testing.T) {
	def := writeManifest(t, "github.mcp.yaml", toolsExcludeManifest)
	before := readFile(t, def.GetPath())

	change, err := AllowPolicyItem(def, "tool", "read_issue")
	if err != nil {
		t.Fatalf("AllowPolicyItem: %v", err)
	}
	if !change.Unchanged {
		t.Errorf("change = %+v, want Unchanged", change)
	}
	if got := readFile(t, def.GetPath()); got != before {
		t.Errorf("manifest must not be rewritten when the value is already allowed:\n%s", got)
	}
}

func TestAllowPolicyItemMarkdownFrontMatter(t *testing.T) {
	content := `---
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: docs
spec:
  type: stdio
  command: ["server"]
  policies:
    prompts:
      exclude: ["danger"]
---

# Docs server

Read the instructions below.
`
	def := writeManifest(t, "docs.mcp.md", content)

	if _, err := AllowPolicyItem(def, "prompt", "danger"); err != nil {
		t.Fatalf("AllowPolicyItem: %v", err)
	}

	updated := readFile(t, def.GetPath())
	if strings.Contains(updated, "danger") {
		t.Errorf("edited manifest still blocks the prompt:\n%s", updated)
	}
	if !strings.HasPrefix(updated, "---\n") {
		t.Errorf("edited manifest must keep its front-matter fence:\n%s", updated)
	}
	if !strings.Contains(updated, "Read the instructions below.") {
		t.Errorf("edited manifest must keep the markdown body:\n%s", updated)
	}

	reloaded, err := parseManifest(def.GetPath(), updated)
	if err != nil {
		t.Fatalf("re-parse edited manifest: %v", err)
	}
	if !reloaded.AllowsPrompt("danger") {
		t.Error("danger prompt must be allowed after the edit")
	}
}

func TestAllowPolicyItemRejectsReadOnlyManifests(t *testing.T) {
	def := writeManifest(t, "github.mcp.yaml", toolsExcludeManifest)
	def.Namespace = warp.NamespaceSystem
	if _, err := AllowPolicyItem(def, "tool", "delete_issue"); !errors.Is(err, ErrManifestReadOnly) {
		t.Errorf("built-in namespace: got %v, want ErrManifestReadOnly", err)
	}

	def = writeManifest(t, "github.mcp.yaml", toolsExcludeManifest)
	def.Path = ""
	if _, err := AllowPolicyItem(def, "tool", "delete_issue"); !errors.Is(err, ErrManifestReadOnly) {
		t.Errorf("missing path: got %v, want ErrManifestReadOnly", err)
	}

	def = writeManifest(t, "github.mcp.yaml", toolsExcludeManifest)
	def.Path = "relative/github.mcp.yaml"
	if _, err := AllowPolicyItem(def, "tool", "delete_issue"); !errors.Is(err, ErrManifestReadOnly) {
		t.Errorf("relative path: got %v, want ErrManifestReadOnly", err)
	}

	def = writeManifest(t, "github.mcp.yaml", toolsExcludeManifest)
	def.Namespace = ""
	def.Path = filepath.Join(filepath.Dir(def.GetPath()), "server.txt")
	if err := os.WriteFile(def.Path, []byte("not a manifest"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AllowPolicyItem(def, "tool", "delete_issue"); !errors.Is(err, ErrManifestReadOnly) {
		t.Errorf("unsupported extension: got %v, want ErrManifestReadOnly", err)
	}
}

func TestAllowPolicyItemRejectsUnwritableFile(t *testing.T) {
	def := writeManifest(t, "github.mcp.yaml", toolsExcludeManifest)
	if err := os.Chmod(def.GetPath(), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(def.GetPath(), 0o644) })

	if _, err := AllowPolicyItem(def, "tool", "delete_issue"); !errors.Is(err, ErrManifestReadOnly) {
		t.Errorf("read-only file: got %v, want ErrManifestReadOnly", err)
	}
}

func TestAllowPolicyItemRejectsUnknownKind(t *testing.T) {
	def := writeManifest(t, "github.mcp.yaml", toolsExcludeManifest)
	if _, err := AllowPolicyItem(def, "widget", "x"); err == nil {
		t.Fatal("expected an error for an unknown policy kind")
	}
}

func TestAllowPolicyItemPreservesFileMode(t *testing.T) {
	def := writeManifest(t, "github.mcp.yaml", toolsExcludeManifest)
	if err := os.Chmod(def.GetPath(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AllowPolicyItem(def, "tool", "delete_issue"); err != nil {
		t.Fatalf("AllowPolicyItem: %v", err)
	}
	info, err := os.Stat(def.GetPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("manifest mode = %o, want 600", info.Mode().Perm())
	}
}

func TestPolicySectionNames(t *testing.T) {
	for kind, want := range map[string]string{
		"tool":     "tools",
		"prompt":   "prompts",
		"resource": "resources",
	} {
		got, err := policySection(kind)
		if err != nil {
			t.Errorf("policySection(%q): %v", kind, err)
			continue
		}
		if got != want {
			t.Errorf("policySection(%q) = %q, want %q", kind, got, want)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
