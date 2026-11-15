package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandboxcfg"
)

func policyContains(paths []string, want string) bool {
	want = filepath.Clean(want)
	for _, p := range paths {
		if strings.EqualFold(filepath.Clean(p), want) {
			return true
		}
	}
	return false
}

// newSandboxSettingsSession creates a top-level session whose work directory
// is the workspace root, wired with the given sandboxcfg manager.
func newSandboxSettingsSession(t *testing.T, mgr sandboxcfg.Manager) *Session {
	t.Helper()
	db, ws, gw, pub, _ := setupTestEnv(t, nil)

	permMgr, err := permissions.New(permissions.Config{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := models.Build(ws.Catalog(), gw)
	if err != nil {
		t.Fatal(err)
	}

	sess, err := New(context.Background(), Config{
		Title:           "Sandbox Settings",
		Session:         api.SessionConfig{Agent: "main", Model: "fake/fake-default-model"},
		DB:              db,
		Workspace:       ws,
		Gateway:         func() gateway.Gateway { return gw },
		Catalog:         func() *models.Catalog { return catalog },
		Permissions:     permMgr,
		SandboxSettings: mgr,
		Publisher:       pub,
		Logger:          log.Default(),
	})
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// settingsManager returns a sandboxcfg manager anchored on workDir with both
// persisted scopes isolated under temp directories.
func settingsManager(t *testing.T, workDir string) sandboxcfg.Manager {
	t.Helper()
	mgr, err := sandboxcfg.New(sandboxcfg.Config{
		WorkDir:     workDir,
		GlobalPath:  filepath.Join(t.TempDir(), "global-sandbox.json"),
		ProjectPath: filepath.Join(t.TempDir(), "project-sandbox.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return mgr
}

func TestSessionDefaultModeFromSandboxSettings(t *testing.T) {
	sessProbe := newSandboxSettingsSession(t, nil)
	workDir := sessProbe.WorkDir
	if err := sessProbe.Close(); err != nil {
		t.Fatalf("probe session close: %v", err)
	}

	mgr := settingsManager(t, workDir)
	if err := mgr.Set(api.ScopeGlobal, api.SandboxSettings{Mode: api.SandboxLocal}); err != nil {
		t.Fatal(err)
	}

	sess := newSandboxSettingsSession(t, mgr)
	if sess.Config.Sandbox != api.SandboxLocal {
		t.Errorf("session sandbox = %q, want inherited configured default local", sess.Config.Sandbox)
	}
	if sess.Sandbox.Mode() != sandbox.ModeLocal {
		t.Errorf("enforced backend = %q, want local", sess.Sandbox.Mode())
	}
}

func TestSessionFailIfUnavailableBlocksCreation(t *testing.T) {
	if sandbox.NativeAvailable() == nil {
		t.Skip("kernel sandboxing is available on this host; hard-failure path cannot be exercised")
	}

	mgr := settingsManager(t, t.TempDir())
	if err := mgr.Set(api.ScopeGlobal, api.SandboxSettings{FailIfUnavailable: new(true)}); err != nil {
		t.Fatal(err)
	}

	db, ws, gw, pub, _ := setupTestEnv(t, nil)
	permMgr, err := permissions.New(permissions.Config{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := models.Build(ws.Catalog(), gw)
	if err != nil {
		t.Fatal(err)
	}

	_, err = New(context.Background(), Config{
		Title:           "Must fail",
		Session:         api.SessionConfig{Agent: "main", Model: "fake/fake-default-model"},
		DB:              db,
		Workspace:       ws,
		Gateway:         func() gateway.Gateway { return gw },
		Catalog:         func() *models.Catalog { return catalog },
		Permissions:     permMgr,
		SandboxSettings: mgr,
		Publisher:       pub,
		Logger:          log.Default(),
	})
	if !errors.Is(err, sandbox.ErrNativeUnavailable) {
		t.Fatalf("session.New with failIfUnavailable on unavailable host error = %v, want ErrNativeUnavailable", err)
	}
}

func TestSessionRefreshAppliesSandboxSettingsLive(t *testing.T) {
	sess := newSandboxSettingsSession(t, nil)
	workDir := sess.WorkDir

	denyDir := filepath.Join(workDir, "vendor")
	allowDir := filepath.Join(workDir, "scratch")
	for _, d := range []string{denyDir, allowDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	mgr := settingsManager(t, workDir)
	if err := mgr.Set(api.ScopeProject, api.SandboxSettings{
		Filesystem: api.SandboxFilesystemSettings{
			DenyWrite:  []string{denyDir},
			AllowWrite: []string{allowDir},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// Re-point the live session at the manager so Refresh picks the settings
	// up, mirroring a workspace runtime that gained sandbox.json support.
	sess.Lock()
	sess.sandboxCfg = mgr
	sess.Unlock()

	before := sess.Sandbox.Filesystem()
	if policyContains(before.DeniedWritePaths, denyDir) {
		t.Fatalf("precondition: vendor deny present before the settings were wired")
	}

	sess.Refresh(context.Background())

	after := sess.Sandbox.Filesystem()
	if !policyContains(after.DeniedWritePaths, denyDir) {
		t.Errorf("deniedWrite after refresh = %v, want live-applied %q", after.DeniedWritePaths, denyDir)
	}
	if !policyContains(after.ExtraWritePaths, allowDir) {
		t.Errorf("extraWrite after refresh = %v, want live-applied %q", after.ExtraWritePaths, allowDir)
	}

	// The built-in protected floor must survive the live refresh too.
	agentsDir := filepath.Join(workDir, ".agents")
	if !policyContains(after.DeniedWritePaths, agentsDir) {
		t.Errorf("deniedWrite after refresh = %v, want built-in protected %q", after.DeniedWritePaths, agentsDir)
	}
}
