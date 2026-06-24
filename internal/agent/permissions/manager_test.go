package permissions

import (
	"context"
	"path/filepath"
	"testing"
)

func TestFSManager_Permissions(t *testing.T) {
	tmpDir := t.TempDir()

	globalPath := filepath.Join(tmpDir, "global_perms.json")
	workspacePath := filepath.Join(tmpDir, "ws_perms.json")
	sessionPath := filepath.Join(tmpDir, "sess_perms.json")

	mgr := NewFSManagerWithPaths(globalPath, workspacePath, sessionPath)

	ctx := context.Background()

	// Initial checks should be empty/default
	if grants := mgr.GetGrants(ctx, "bash"); len(grants) != 0 {
		t.Errorf("expected 0 grants initially, got %d", len(grants))
	}
	if mode := mgr.GetMode(ctx); mode != ModeDefault {
		t.Errorf("expected initial mode %q, got %q", ModeDefault, mode)
	}

	// 1. Save global permission
	globalPerm := Permission{
		Group:       "bash",
		Target:      "npm *",
		MatchMethod: "wildcard",
		Action:      ActionAllow,
	}
	if err := mgr.SavePermission(ctx, ScopeGlobal, globalPerm); err != nil {
		t.Fatalf("failed to save global permission: %v", err)
	}

	// 2. Save workspace permission
	wsPerm := Permission{
		Group:       "bash",
		Target:      "go test",
		MatchMethod: "prefix",
		Action:      ActionAllow,
	}
	if err := mgr.SavePermission(ctx, ScopeWorkspace, wsPerm); err != nil {
		t.Fatalf("failed to save workspace permission: %v", err)
	}

	// 3. Save session permission
	sessPerm := Permission{
		Group:       "bash",
		Target:      "rm -rf tmp",
		MatchMethod: "exact",
		Action:      ActionDeny,
	}
	if err := mgr.SavePermission(ctx, ScopeSession, sessPerm); err != nil {
		t.Fatalf("failed to save session permission: %v", err)
	}

	// Check combined grants for tool "bash"
	grants := mgr.GetGrants(ctx, "bash")
	if len(grants) != 3 {
		t.Fatalf("expected 3 grants, got %d", len(grants))
	}

	// Verify order: Session -> Workspace -> Global
	if grants[0].Target != "rm -rf tmp" {
		t.Errorf("expected first grant to be session level (rm -rf tmp), got %q", grants[0].Target)
	}
	if grants[1].Target != "go test" {
		t.Errorf("expected second grant to be workspace level (go test), got %q", grants[1].Target)
	}
	if grants[2].Target != "npm *" {
		t.Errorf("expected third grant to be global level (npm *), got %q", grants[2].Target)
	}

	// Test duplicate avoidance
	if err := mgr.SavePermission(ctx, ScopeSession, sessPerm); err != nil {
		t.Fatalf("failed to save duplicate session permission: %v", err)
	}
	grants = mgr.GetGrants(ctx, "bash")
	if len(grants) != 3 {
		t.Errorf("expected still 3 grants after duplicate insert, got %d", len(grants))
	}
}

func TestFSManager_Mode(t *testing.T) {
	tmpDir := t.TempDir()

	globalPath := filepath.Join(tmpDir, "global_perms.json")
	workspacePath := filepath.Join(tmpDir, "ws_perms.json")
	sessionPath := filepath.Join(tmpDir, "sess_perms.json")

	mgr := NewFSManagerWithPaths(globalPath, workspacePath, sessionPath)

	ctx := context.Background()

	// Default
	if mode := mgr.GetMode(ctx); mode != ModeDefault {
		t.Errorf("expected default mode %q, got %q", ModeDefault, mode)
	}

	// Set global mode to Auto
	if err := mgr.SaveMode(ctx, ScopeGlobal, ModeAuto); err != nil {
		t.Fatalf("failed to save global mode: %v", err)
	}
	if mode := mgr.GetMode(ctx); mode != ModeAuto {
		t.Errorf("expected mode %q, got %q", ModeAuto, mode)
	}

	// Set workspace mode to Strict (should override global)
	if err := mgr.SaveMode(ctx, ScopeWorkspace, ModeStrict); err != nil {
		t.Fatalf("failed to save workspace mode: %v", err)
	}
	if mode := mgr.GetMode(ctx); mode != ModeStrict {
		t.Errorf("expected mode %q, got %q", ModeStrict, mode)
	}

	// Set session mode to Default (should override workspace)
	if err := mgr.SaveMode(ctx, ScopeSession, ModeDefault); err != nil {
		t.Fatalf("failed to save session mode: %v", err)
	}
	if mode := mgr.GetMode(ctx); mode != ModeDefault {
		t.Errorf("expected mode %q, got %q", ModeDefault, mode)
	}
}

func TestFSManager_NewFSManagerPaths(t *testing.T) {
	// Verify constructor works with empty/populated values without panic
	mgr, err := NewFSManager("test-workspace", "session-123")
	if err != nil {
		t.Fatalf("NewFSManager failed: %v", err)
	}
	if mgr.globalPath == "" {
		t.Error("globalPath should not be empty")
	}
	if mgr.workspacePath == "" {
		t.Error("workspacePath should not be empty")
	}
	if mgr.sessionPath == "" {
		t.Error("sessionPath should not be empty")
	}
}
