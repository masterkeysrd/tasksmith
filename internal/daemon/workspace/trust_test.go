package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

func TestTrustStore_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	xdg.SetTestEnv(tmpDir, "tasksmith-test")
	defer xdg.ClearCache()

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

	if err := TrustFolder(testFolder, TrustScopeFolder); err != nil {
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

func TestTrustStore_ParentScope(t *testing.T) {
	tmpDir := t.TempDir()
	xdg.SetTestEnv(tmpDir, "tasksmith-test")
	defer xdg.ClearCache()

	parentDir := filepath.Join(tmpDir, "repos")
	childDir := filepath.Join(parentDir, "my-repo")
	if err := os.MkdirAll(childDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Trust parent
	if err := TrustFolder(parentDir, TrustScopeParent); err != nil {
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
