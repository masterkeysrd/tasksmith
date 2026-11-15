package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpen_RestrictedMode(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	if realDir, err := filepath.EvalSymlinks(tmpDir); err == nil {
		tmpDir = realDir
	}

	// 1. Create .env with secret
	envFile := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(envFile, []byte("WORKSPACE_SECRET=shh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 2. Create custom tool manifest in .agents/tools/
	toolDir := filepath.Join(tmpDir, ".agents", "tools")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	toolContent := `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: dangerous-tool
  description: dangerous custom tool
spec:
  command: ["rm", "-rf", "/"]
---
`
	if err := os.WriteFile(filepath.Join(toolDir, "dangerous-tool.tool.md"), []byte(toolContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. Open in standard mode
	wsNormal, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open normal mode error = %v", err)
	}
	if wsNormal.IsRestricted() {
		t.Errorf("wsNormal.IsRestricted() = true, want false")
	}
	if wsNormal.Env()["WORKSPACE_SECRET"] != "shh" {
		t.Errorf("expected WORKSPACE_SECRET to be loaded in normal mode")
	}
	if _, ok := wsNormal.Catalog().GetTool("dangerous-tool"); !ok {
		t.Errorf("expected dangerous-tool to be in catalog in normal mode")
	}

	// 4. Open in restricted mode
	wsRestricted, err := Open(ctx, tmpDir, WithRestrictedMode(true))
	if err != nil {
		t.Fatalf("Open restricted mode error = %v", err)
	}
	if !wsRestricted.IsRestricted() {
		t.Errorf("wsRestricted.IsRestricted() = false, want true")
	}
	if _, ok := wsRestricted.Env()["WORKSPACE_SECRET"]; ok {
		t.Errorf("expected WORKSPACE_SECRET to NOT be loaded in restricted mode")
	}
	if _, ok := wsRestricted.Catalog().GetTool("dangerous-tool"); ok {
		t.Errorf("expected dangerous-tool to be stripped from catalog in restricted mode")
	}
}
