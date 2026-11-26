package workspace

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/testutils"
)

func TestTrustStore_SaveAndLoad(t *testing.T) {
	tmpDir := testutils.SetupXDG(t, "tasksmith-test")

	store, err := LoadTrustStore()
	if err != nil {
		t.Fatalf("LoadTrustStore() error = %v", err)
	}
	if store.Schema != 1 {
		t.Errorf("store.Schema = %d, want 1", store.Schema)
	}
	if len(store.Folders) != 0 {
		t.Errorf("len(store.Folders) = %d, want 0", len(store.Folders))
	}

	testFolder := filepath.Join(tmpDir, "project-a")
	if err := os.MkdirAll(testFolder, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := TrustFolder(testFolder, api.TrustScopeFolder); err != nil {
		t.Fatalf("TrustFolder error = %v", err)
	}

	trusted, fpChanged, err := IsTrusted(testFolder)
	if err != nil {
		t.Fatalf("IsTrusted error = %v", err)
	}
	if !trusted {
		t.Errorf("expected testFolder to be trusted")
	}
	if fpChanged {
		t.Errorf("expected fpChanged to be false")
	}

	// Revoke
	if err := RevokeTrust(testFolder); err != nil {
		t.Fatalf("RevokeTrust error = %v", err)
	}

	trusted, _, err = IsTrusted(testFolder)
	if err != nil {
		t.Fatalf("IsTrusted after revoke error = %v", err)
	}
	if trusted {
		t.Errorf("expected testFolder to NOT be trusted after revoke")
	}
}

func TestTrustRejectsNonRegularWorkspaceManifest(t *testing.T) {
	testutils.SetupXDG(t, "trust-manifest")
	dir := t.TempDir()
	if err := TrustFolder(dir, api.TrustScopeFolder); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(os.DevNull, filepath.Join(dir, "WORKSPACE.md")); err != nil {
		t.Fatal(err)
	}

	if trusted, _, err := IsTrusted(dir); err == nil || trusted {
		t.Errorf("IsTrusted: trusted=%v err=%v, want false and an error", trusted, err)
	}
	if _, err := ScanExecutableConfig(dir); err == nil {
		t.Error("ScanExecutableConfig accepted a non-regular workspace manifest")
	}
}

func TestTrustTracksEffectiveExecutableSet(t *testing.T) {
	testutils.SetupXDG(t, "trust-test")
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: mono\nspec:\n  projects: [\".\", child]\n---\n"
	if err := os.WriteFile(filepath.Join(root, "WORKSPACE.md"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := TrustFolder(child, api.TrustScopeFolder); err != nil {
		t.Fatal(err)
	}
	tools := filepath.Join(root, ".agents", "tools")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tools, "sample.tool.md")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	checkChanged := func() {
		t.Helper()
		if trusted, changed, err := IsTrusted(child); err != nil || !trusted || !changed {
			t.Fatalf("trust: %v changed: %v err: %v", trusted, changed, err)
		}
	}
	checkChanged()
	if err := TrustFolder(child, api.TrustScopeFolder); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	checkChanged()
	if err := os.Symlink("missing", filepath.Join(tools, "broken.tool.md")); err != nil {
		t.Fatal(err)
	}
	if trusted, _, err := IsTrusted(child); err == nil || trusted {
		t.Fatal("incomplete scan accepted")
	}
	if err := TrustFolder(child, api.TrustScopeFolder); err == nil {
		t.Fatal("incomplete baseline recorded")
	}
}

func TestTrustScopeParentAllowsChildExecutablesAndExactFolderPrecedence(t *testing.T) {
	testutils.SetupXDG(t, "trust-parent-precedence")

	parent := t.TempDir()
	child := filepath.Join(parent, "my-app")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, ".env"), []byte("CHILD_KEY=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := TrustFolder(parent, api.TrustScopeParent); err != nil {
		t.Fatalf("TrustFolder(parent): %v", err)
	}

	trusted, changed, err := IsTrusted(child)
	if err != nil || !trusted || changed {
		t.Fatalf("child under trusted parent: got trusted=%v changed=%v err=%v, want true/false/nil", trusted, changed, err)
	}

	// Modifying the trusted parent's own executable set triggers re-approval.
	if err := os.WriteFile(filepath.Join(parent, ".env"), []byte("PARENT_KEY=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	trusted, changed, err = IsTrusted(child)
	if err != nil || !trusted || !changed {
		t.Fatalf("after parent .env added: got trusted=%v changed=%v err=%v, want true/true/nil", trusted, changed, err)
	}

	// Explicitly trusting the child folder takes precedence over the earlier parent entry.
	if err := TrustFolder(child, api.TrustScopeFolder); err != nil {
		t.Fatalf("TrustFolder(child): %v", err)
	}
	trusted, changed, err = IsTrusted(child)
	if err != nil || !trusted || changed {
		t.Fatalf("after explicit child trust: got trusted=%v changed=%v err=%v, want true/false/nil", trusted, changed, err)
	}

	if err := TrustFolder(child, api.TrustScope("bogus")); err == nil {
		t.Fatal("expected TrustFolder to reject unknown trust scope")
	}
}

func TestTrustStore_ParentScope(t *testing.T) {
	tmpDir := testutils.SetupXDG(t, "tasksmith-test")

	parentDir := filepath.Join(tmpDir, "repos")
	childDir := filepath.Join(parentDir, "my-repo")
	if err := os.MkdirAll(childDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Trust parent
	if err := TrustFolder(parentDir, api.TrustScopeParent); err != nil {
		t.Fatalf("TrustFolder(parent) error = %v", err)
	}

	trusted, _, err := IsTrusted(childDir)
	if err != nil {
		t.Fatalf("IsTrusted(childDir) error = %v", err)
	}
	if !trusted {
		t.Errorf("childDir should be trusted via parent scope")
	}

	outsideDir := filepath.Join(tmpDir, "other-repo")
	trusted, _, err = IsTrusted(outsideDir)
	if err != nil {
		t.Fatalf("IsTrusted(outsideDir) error = %v", err)
	}
	if trusted {
		t.Errorf("outsideDir should NOT be trusted")
	}
}

func TestScanExecutableConfigAndFingerprint(t *testing.T) {
	tmpDir := t.TempDir()

	summary, err := ScanExecutableConfig(tmpDir)
	if err != nil {
		t.Fatalf("ScanExecutableConfig error = %v", err)
	}
	if summary.HasExecutables() {
		t.Errorf("expected clean directory to have no executables")
	}

	// Add .env
	_ = os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("FOO=BAR\n"), 0o644)
	// Add an MCP manifest
	mcpsDir := filepath.Join(tmpDir, ".agents", "mcps")
	_ = os.MkdirAll(mcpsDir, 0o755)
	_ = os.WriteFile(filepath.Join(mcpsDir, "gh.mcp.yaml"), []byte("name: gh\n"), 0o644)

	summary, err = ScanExecutableConfig(tmpDir)
	if err != nil {
		t.Fatalf("ScanExecutableConfig error = %v", err)
	}
	if !summary.HasEnv {
		t.Errorf("expected summary.HasEnv = true")
	}
	if len(summary.MCPServers) != 1 {
		t.Errorf("expected 1 MCP server, got %d", len(summary.MCPServers))
	}

	fp1, err := ComputeExecutableFingerprint(tmpDir)
	if err != nil {
		t.Fatalf("ComputeExecutableFingerprint error = %v", err)
	}
	if fp1 == "" {
		t.Errorf("expected non-empty fingerprint")
	}

	// Modify MCP server and verify fingerprint changes
	time.Sleep(10 * time.Millisecond)
	_ = os.WriteFile(filepath.Join(mcpsDir, "gh.mcp.yaml"), []byte("name: gh-modified\n"), 0o644)

	fp2, err := ComputeExecutableFingerprint(tmpDir)
	if err != nil {
		t.Fatalf("ComputeExecutableFingerprint 2 error = %v", err)
	}
	if fp1 == fp2 {
		t.Errorf("fingerprint should have changed when file modified")
	}
}

func TestScanExecutableConfigIncludesDecisions(t *testing.T) {
	tmpDir := t.TempDir()

	decisionsDir := filepath.Join(tmpDir, ".agents", "decisions")
	if err := os.MkdirAll(decisionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "---\napiVersion: warp/v1alpha1\nkind: Decision\nmetadata:\n  name: permission-reviewer\nspec:\n  models: [\"@default\"]\n---\nrubric\n"
	if err := os.WriteFile(filepath.Join(decisionsDir, "permission-reviewer.decision.md"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	summary, err := ScanExecutableConfig(tmpDir)
	if err != nil {
		t.Fatalf("ScanExecutableConfig error = %v", err)
	}
	wantRel := filepath.Join(".agents", "decisions", "permission-reviewer.decision.md")
	if !slices.Contains(summary.Decisions, wantRel) {
		t.Errorf("expected Decisions to list %s, got %v", wantRel, summary.Decisions)
	}
	if !summary.HasExecutables() {
		t.Errorf("expected HasExecutables true with a repo Decision override present")
	}

	fp1, err := ComputeExecutableFingerprint(tmpDir)
	if err != nil {
		t.Fatalf("ComputeExecutableFingerprint error = %v", err)
	}

	// Editing the Decision rubric must change the trust fingerprint so an
	// agent self-modification re-prompts before the next turn executes.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(decisionsDir, "permission-reviewer.decision.md"), []byte(manifest+"tighter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp2, err := ComputeExecutableFingerprint(tmpDir)
	if err != nil {
		t.Fatalf("ComputeExecutableFingerprint 2 error = %v", err)
	}
	if fp1 == fp2 {
		t.Errorf("fingerprint should have changed when Decision modified")
	}
}
