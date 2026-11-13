package sandboxcfg

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
)

func testManager(t *testing.T, restricted bool) (Manager, string) {
	t.Helper()
	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-sandboxcfg-test")
	t.Cleanup(xdg.ClearCache)

	workDir := t.TempDir()
	mgr, err := New(Config{
		WorkDir:     workDir,
		Restricted:  restricted,
		GlobalPath:  filepath.Join(t.TempDir(), "global", "sandbox.json"),
		ProjectPath: filepath.Join(t.TempDir(), "project", "sandbox.json"),
	})
	if err != nil {
		t.Fatalf("sandboxcfg.New: %v", err)
	}
	return mgr, workDir
}

func containsPath(list []string, want string) bool {
	want = fsutils.ResolveDeepestExisting(want)
	for _, p := range list {
		if strings.EqualFold(filepath.Clean(p), want) {
			return true
		}
	}
	return false
}

func TestGlobalProjectRoundTripAndPermissions(t *testing.T) {
	mgr, _ := testManager(t, false)

	settings := api.SandboxSettings{
		Mode:             api.SandboxLocal,
		ExcludedCommands: []string{"git commit *", "git commit *", " docker compose * "},
		Filesystem: api.SandboxFilesystemSettings{
			AllowWrite: []string{"/tmp/cache", "/tmp/cache"},
			DenyRead:   []string{"~/Secrets"},
		},
		Network: api.SandboxNetworkSettings{
			Mode:           api.SandboxNetworkAllowlist,
			AllowedDomains: []string{"API.Example.com", "api.example.com"},
		},
	}
	for _, scope := range []api.PermissionScope{api.ScopeGlobal, api.ScopeProject} {
		if err := mgr.Set(scope, settings); err != nil {
			t.Fatalf("Set(%s): %v", scope, err)
		}
		path := mgr.(*manager).globalPath
		if scope == api.ScopeProject {
			path = mgr.(*manager).projectPath
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s perms = %o, want 600", scope, perm)
		}
		got, err := mgr.Get(scope)
		if err != nil {
			t.Fatalf("Get(%s): %v", scope, err)
		}
		if got.Schema != 1 {
			t.Errorf("%s schema = %d, want 1", scope, got.Schema)
		}
		if len(got.ExcludedCommands) != 2 {
			t.Errorf("%s excludedCommands = %v, want deduplicated trimmed pair", scope, got.ExcludedCommands)
		}
		if len(got.Filesystem.AllowWrite) != 1 {
			t.Errorf("%s allowWrite = %v, want deduplicated single entry", scope, got.Filesystem.AllowWrite)
		}
		if len(got.Network.AllowedDomains) != 1 || got.Network.AllowedDomains[0] != "api.example.com" {
			t.Errorf("%s allowedDomains = %v, want lowercased dedup [api.example.com]", scope, got.Network.AllowedDomains)
		}
	}
}

func TestGetMissingFileReturnsZeroSettings(t *testing.T) {
	mgr, _ := testManager(t, false)
	for _, scope := range []api.PermissionScope{api.ScopeGlobal, api.ScopeProject} {
		got, err := mgr.Get(scope)
		if err != nil {
			t.Fatalf("Get(%s): %v", scope, err)
		}
		if got.Mode != "" || got.FailIfUnavailable != nil || len(got.ExcludedCommands) != 0 {
			t.Errorf("Get(%s) on missing file = %+v, want zero settings", scope, got)
		}
	}
}

func TestSetRejectsInvalidSettings(t *testing.T) {
	mgr, workDir := testManager(t, false)

	invalid := map[string]api.SandboxSettings{
		"bad mode":             {Mode: "vm"},
		"bad network mode":     {Network: api.SandboxNetworkSettings{Mode: "proxy"}},
		"bad domain":           {Network: api.SandboxNetworkSettings{AllowedDomains: []string{"not a domain"}}},
		"empty exclude":        {ExcludedCommands: []string{"   "}},
		"multiline exclude":    {ExcludedCommands: []string{"git commit\npush"}},
		"relative global path": {Filesystem: api.SandboxFilesystemSettings{AllowWrite: []string{"relative/dir"}}},
		"tildeglub path":       {Filesystem: api.SandboxFilesystemSettings{DenyRead: []string{"~other/dir"}}},
		"empty path":           {Filesystem: api.SandboxFilesystemSettings{DenyWrite: []string{"  "}}},
		"wildcard bare":        {Network: api.SandboxNetworkSettings{DeniedDomains: []string{"*"}}},
		"wildcard no suffix":   {Network: api.SandboxNetworkSettings{DeniedDomains: []string{"*."}}},
	}
	for name, settings := range invalid {
		if err := mgr.Set(api.ScopeGlobal, settings); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("Set(global, %s) error = %v, want ErrInvalidValue", name, err)
		}
	}

	valid := map[string]api.SandboxSettings{
		"workspace-relative project path": {Filesystem: api.SandboxFilesystemSettings{DenyWrite: []string{"build"}}},
		"tilde-expanded global path":      {Filesystem: api.SandboxFilesystemSettings{DenyRead: []string{"~/Documents"}}},
		"absolute project path":           {Filesystem: api.SandboxFilesystemSettings{AllowWrite: []string{workDir}}},
		"wildcard domain":                 {Network: api.SandboxNetworkSettings{Mode: api.SandboxNetworkAllowlist, AllowedDomains: []string{"*.example.com"}}},
	}
	for name, settings := range valid {
		if err := mgr.Set(api.ScopeProject, settings); err != nil {
			t.Errorf("Set(project, %s) = %v, want success", name, err)
		}
	}
	if err := mgr.Set(api.ScopeGlobal, api.SandboxSettings{Filesystem: api.SandboxFilesystemSettings{DenyRead: []string{"~/Documents"}}}); err != nil {
		t.Errorf("Set(global, tilde path) = %v, want success", err)
	}
	if err := mgr.Set(api.PermissionScope("machine"), api.SandboxSettings{}); !errors.Is(err, ErrInvalidScope) {
		t.Errorf("Set(machine scope) error = %v, want ErrInvalidScope", err)
	}
}

func TestOmittedBoolInheritanceAndDefaults(t *testing.T) {
	mgr, _ := testManager(t, false)
	if err := mgr.Set(api.ScopeGlobal, api.SandboxSettings{
		Mode:          api.SandboxNative,
		AutoAllowBash: new(true),
	}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Set(api.ScopeProject, api.SandboxSettings{
		Mode: api.SandboxLocal,
	}); err != nil {
		t.Fatal(err)
	}
	eff, err := mgr.Effective()
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if eff.Mode != api.SandboxLocal {
		t.Errorf("mode = %q, want project override local", eff.Mode)
	}
	if !eff.AutoAllowBash {
		t.Error("autoAllowBash = false, want inherited global true")
	}
	if eff.FailIfUnavailable {
		t.Error("failIfUnavailable = true, want default false")
	}
	if !eff.AllowUnsandboxed {
		t.Error("allowUnsandboxed = false, want trusted-workspace default true")
	}
}

func TestMonotonicLocks(t *testing.T) {
	t.Run("global failIfUnavailable true cannot be relaxed", func(t *testing.T) {
		mgr, _ := testManager(t, false)
		if err := mgr.Set(api.ScopeGlobal, api.SandboxSettings{FailIfUnavailable: new(true)}); err != nil {
			t.Fatal(err)
		}
		if err := mgr.Set(api.ScopeProject, api.SandboxSettings{FailIfUnavailable: new(false)}); err != nil {
			t.Fatal(err)
		}
		eff, err := mgr.Effective()
		if err != nil {
			t.Fatal(err)
		}
		if !eff.FailIfUnavailable {
			t.Error("failIfUnavailable = false, want global true lock to hold")
		}
	})

	t.Run("project-only failIfUnavailable applies", func(t *testing.T) {
		mgr, _ := testManager(t, false)
		if err := mgr.Set(api.ScopeProject, api.SandboxSettings{FailIfUnavailable: new(true)}); err != nil {
			t.Fatal(err)
		}
		eff, err := mgr.Effective()
		if err != nil {
			t.Fatal(err)
		}
		if !eff.FailIfUnavailable {
			t.Error("failIfUnavailable = false, want project enable to stick")
		}
	})

	t.Run("global allowUnsandboxed false cannot be re-opened", func(t *testing.T) {
		mgr, _ := testManager(t, false)
		if err := mgr.Set(api.ScopeGlobal, api.SandboxSettings{AllowUnsandboxed: new(false)}); err != nil {
			t.Fatal(err)
		}
		if err := mgr.Set(api.ScopeProject, api.SandboxSettings{AllowUnsandboxed: new(true)}); err != nil {
			t.Fatal(err)
		}
		eff, err := mgr.Effective()
		if err != nil {
			t.Fatal(err)
		}
		if eff.AllowUnsandboxed {
			t.Error("allowUnsandboxed = true, want global false lock to hold")
		}
	})
}

func TestRestrictedWorkspaceClamp(t *testing.T) {
	mgr, _ := testManager(t, true)
	settings := api.SandboxSettings{
		AllowUnsandboxed: new(true),
		ExcludedCommands: []string{"git commit *"},
	}
	if err := mgr.Set(api.ScopeProject, settings); err != nil {
		t.Fatal(err)
	}
	eff, err := mgr.Effective()
	if err != nil {
		t.Fatal(err)
	}
	if eff.AllowUnsandboxed {
		t.Error("allowUnsandboxed = true in restricted workspace, want forced false")
	}
	if len(eff.ExcludedCommands) != 0 {
		t.Errorf("excludedCommands = %v in restricted workspace, want dropped", eff.ExcludedCommands)
	}

	mgr.SetRestricted(false)
	eff, err = mgr.Effective()
	if err != nil {
		t.Fatal(err)
	}
	if !eff.AllowUnsandboxed {
		t.Error("allowUnsandboxed = false after trust, want true")
	}
	if len(eff.ExcludedCommands) != 1 {
		t.Errorf("excludedCommands = %v after trust, want restored", eff.ExcludedCommands)
	}
}

func TestPathExpansionPerScope(t *testing.T) {
	mgr, workDir := testManager(t, false)

	if err := mgr.Set(api.ScopeProject, api.SandboxSettings{
		Filesystem: api.SandboxFilesystemSettings{
			AllowWrite: []string{"../outside", "sub/dir"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	eff, err := mgr.Effective()
	if err != nil {
		t.Fatal(err)
	}
	if !containsPath(eff.ExtraWritePaths, filepath.Join(workDir, "..", "outside")) {
		t.Errorf("extraWrite = %v, want project relative ../outside resolved against workspace root", eff.ExtraWritePaths)
	}
	if !containsPath(eff.ExtraWritePaths, filepath.Join(workDir, "sub", "dir")) {
		t.Errorf("extraWrite = %v, want project relative sub/dir resolved against workspace root", eff.ExtraWritePaths)
	}

	if err := mgr.Set(api.ScopeGlobal, api.SandboxSettings{
		Filesystem: api.SandboxFilesystemSettings{DenyRead: []string{"~/Secrets"}},
	}); err != nil {
		t.Fatal(err)
	}
	eff, err = mgr.Effective()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if !containsPath(eff.DeniedReadPaths, filepath.Join(home, "Secrets")) {
		t.Errorf("deniedRead = %v, want ~/Secrets expanded to home", eff.DeniedReadPaths)
	}
}

func TestUnionDedupAcrossScopes(t *testing.T) {
	mgr, _ := testManager(t, false)
	if err := mgr.Set(api.ScopeGlobal, api.SandboxSettings{
		ExcludedCommands: []string{"git commit *", "docker compose *"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Set(api.ScopeProject, api.SandboxSettings{
		ExcludedCommands: []string{"docker compose *", "make deploy"},
	}); err != nil {
		t.Fatal(err)
	}
	eff, err := mgr.Effective()
	if err != nil {
		t.Fatal(err)
	}
	if len(eff.ExcludedCommands) != 3 {
		t.Errorf("excludedCommands = %v, want 3-entry union", eff.ExcludedCommands)
	}
}

func TestWriteOverlapDenyWins(t *testing.T) {
	mgr, workDir := testManager(t, false)
	covered := filepath.Join(workDir, "build")
	if err := os.MkdirAll(covered, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Set(api.ScopeProject, api.SandboxSettings{
		Filesystem: api.SandboxFilesystemSettings{
			AllowWrite: []string{covered},
			DenyWrite:  []string{workDir},
		},
	}); err != nil {
		t.Fatal(err)
	}
	eff, err := mgr.Effective()
	if err != nil {
		t.Fatal(err)
	}
	if containsPath(eff.ExtraWritePaths, covered) {
		t.Errorf("extraWrite = %v, want allowWrite under a broader denyWrite dropped", eff.ExtraWritePaths)
	}
	if !containsPath(eff.DeniedWritePaths, workDir) {
		t.Errorf("deniedWrite = %v, want user denyWrite present", eff.DeniedWritePaths)
	}
}

func TestProtectedWritesSurviveBroadAllow(t *testing.T) {
	mgr, _ := testManager(t, false)
	configDir, err := xdg.Home(xdg.VarTypeConfig)
	if err != nil {
		t.Fatal(err)
	}
	// A broad allowWrite covering TaskSmith's own config store must not remove
	// the protected write floor or make sandbox.json writable in the kernel.
	if err := mgr.Set(api.ScopeGlobal, api.SandboxSettings{
		Filesystem: api.SandboxFilesystemSettings{AllowWrite: []string{configDir}},
	}); err != nil {
		t.Fatal(err)
	}
	eff, err := mgr.Effective()
	if err != nil {
		t.Fatal(err)
	}
	if containsPath(eff.ExtraWritePaths, configDir) {
		t.Errorf("extraWrite = %v, want allowWrite over the protected config store dropped", eff.ExtraWritePaths)
	}
	if !containsPath(eff.DeniedWritePaths, configDir) {
		t.Errorf("deniedWrite = %v, want protected config store always present", eff.DeniedWritePaths)
	}
}

func TestReadOverlapAndSecretStores(t *testing.T) {
	mgr, workDir := testManager(t, false)

	sshDir := filepath.Join(workDir, "dotssh")
	reopen := filepath.Join(sshDir, "known_hosts")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reopen, []byte("host key\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Set(api.ScopeProject, api.SandboxSettings{
		Filesystem: api.SandboxFilesystemSettings{
			DenyRead:  []string{sshDir},
			AllowRead: []string{reopen},
		},
	}); err != nil {
		t.Fatal(err)
	}
	eff, err := mgr.Effective()
	if err != nil {
		t.Fatal(err)
	}
	if !containsPath(eff.AllowReadPaths, reopen) {
		t.Errorf("allowReadPaths = %v, want the narrower re-open kept", eff.AllowReadPaths)
	}
	if !containsPath(eff.DeniedReadPaths, sshDir) {
		t.Error("broader denyRead mask disappeared")
	}

	// Exact-tie deny wins: allowlisting the same path that is denied keeps it
	// masked.
	if err := mgr.Set(api.ScopeProject, api.SandboxSettings{
		Filesystem: api.SandboxFilesystemSettings{
			DenyRead:  []string{sshDir},
			AllowRead: []string{sshDir},
		},
	}); err != nil {
		t.Fatal(err)
	}
	eff, err = mgr.Effective()
	if err != nil {
		t.Fatal(err)
	}
	if !containsPath(eff.DeniedReadPaths, sshDir) {
		t.Error("exact-tie allowRead removed the deny mask, want deny to win")
	}
	if containsPath(eff.AllowReadPaths, sshDir) {
		t.Errorf("allowReadPaths = %v, want the exact-tie allow dropped so the deny stands unopposed", eff.AllowReadPaths)
	}
}

func TestTasksmithSecretStoresNeverReopened(t *testing.T) {
	mgr, _ := testManager(t, false)
	configDir, err := xdg.Home(xdg.VarTypeConfig)
	if err != nil {
		t.Fatal(err)
	}
	dataDir, err := xdg.Home(xdg.VarTypeData)
	if err != nil {
		t.Fatal(err)
	}

	credentialChild := filepath.Join(dataDir, "credentials", "oauth.json")
	if err := mgr.Set(api.ScopeGlobal, api.SandboxSettings{
		Filesystem: api.SandboxFilesystemSettings{
			AllowRead: []string{
				filepath.Join(configDir, ".env"),
				dataDir,
				credentialChild,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	eff, err := mgr.Effective()
	if err != nil {
		t.Fatal(err)
	}
	if containsPath(eff.AllowReadPaths, filepath.Join(configDir, ".env")) {
		t.Error("allowRead re-opened $XDG_CONFIG_HOME/tasksmith/.env")
	}
	if containsPath(eff.AllowReadPaths, dataDir) {
		t.Error("allowRead re-opened an ancestor of the TaskSmith data store")
	}
	if containsPath(eff.AllowReadPaths, credentialChild) {
		t.Errorf("allowReadPaths = %v, want descendant file inside a TaskSmith secret store dropped", eff.AllowReadPaths)
	}
	if !containsPath(eff.DeniedReadPaths, filepath.Join(configDir, ".env")) {
		t.Error("credential floor missing the TaskSmith .env store")
	}
}

func TestResolveMode(t *testing.T) {
	mgr, _ := testManager(t, false)

	if err := mgr.Set(api.ScopeGlobal, api.SandboxSettings{Mode: api.SandboxLocal}); err != nil {
		t.Fatal(err)
	}
	eff, err := mgr.Effective()
	if err != nil {
		t.Fatal(err)
	}
	mode, err := eff.ResolveMode("")
	if err != nil {
		t.Fatal(err)
	}
	if mode != api.SandboxLocal {
		t.Errorf("ResolveMode(default) = %q, want configured local", mode)
	}
	mode, err = eff.ResolveMode(api.SandboxNative)
	if err != nil {
		t.Fatal(err)
	}
	if sandbox.NativeAvailable() != nil {
		if mode != api.SandboxLocal {
			t.Errorf("ResolveMode(native on unavailable host) = %q, want local fallback", mode)
		}
	} else if mode != api.SandboxNative {
		t.Errorf("ResolveMode(native on available host) = %q, want native", mode)
	}

	if err := mgr.Set(api.ScopeGlobal, api.SandboxSettings{FailIfUnavailable: new(true)}); err != nil {
		t.Fatal(err)
	}
	eff, err = mgr.Effective()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eff.ResolveMode(api.SandboxNative); sandbox.NativeAvailable() != nil {
		if !errors.Is(err, sandbox.ErrNativeUnavailable) {
			t.Errorf("ResolveMode(native, failIfUnavailable) error = %v, want ErrNativeUnavailable", err)
		}
	}
	if _, err := eff.ResolveMode(api.SandboxLocal); err != nil {
		t.Errorf("ResolveMode(local, failIfUnavailable) = %v, want success", err)
	}
}

func TestOverlayKeepsBuiltInFloor(t *testing.T) {
	mgr, workDir := testManager(t, false)
	userDir := filepath.Join(workDir, "scratch")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Set(api.ScopeProject, api.SandboxSettings{
		Filesystem: api.SandboxFilesystemSettings{
			AllowWrite: []string{userDir},
			DenyWrite:  []string{filepath.Join(workDir, "vendor")},
		},
	}); err != nil {
		t.Fatal(err)
	}
	eff, err := mgr.Effective()
	if err != nil {
		t.Fatal(err)
	}

	base := sandbox.FilesystemPolicy{
		DeniedReadPaths:  []string{"/built-in/credential"},
		ExtraWritePaths:  []string{"/built-in/worktree-git"},
		DeniedWritePaths: []string{"/built-in/protected"},
	}
	p := eff.Overlay(base)

	if !containsPath(p.ExtraWritePaths, "/built-in/worktree-git") {
		t.Error("overlay dropped a built-in extra write path")
	}
	if !containsPath(p.ExtraWritePaths, userDir) {
		t.Error("overlay missing the user allowWrite entry")
	}
	if !containsPath(p.DeniedWritePaths, "/built-in/protected") {
		t.Error("overlay dropped a built-in denied write path")
	}
	if !containsPath(p.DeniedWritePaths, filepath.Join(workDir, "vendor")) {
		t.Error("overlay missing the user denyWrite entry")
	}
	if !containsPath(p.DeniedReadPaths, "/built-in/credential") {
		t.Error("overlay dropped a built-in read mask")
	}
	if base.DeniedReadPaths[0] != "/built-in/credential" || len(base.ExtraWritePaths) != 1 {
		t.Error("overlay mutated the base policy")
	}
}

func TestEffectiveSettingsJSONRoundTrip(t *testing.T) {
	raw := `{"mode":"native","failIfUnavailable":true,"filesystem":{"denyRead":["~/Secrets"],"allowRead":["~/Secrets/pub"]},"network":{"mode":"allowlist","allowedDomains":["*.example.com"]}}`
	var settings api.SandboxSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if settings.Mode != api.SandboxNative || settings.FailIfUnavailable == nil || !*settings.FailIfUnavailable {
		t.Fatalf("unmarshal mismatch: %+v", settings)
	}
	if settings.AllowUnsandboxed != nil {
		t.Error("absent allowUnsandboxed must unmarshal to nil, not false")
	}
	if settings.Network.Mode != api.SandboxNetworkAllowlist || len(settings.Network.AllowedDomains) != 1 {
		t.Errorf("network = %+v, want allowlist with one domain", settings.Network)
	}
}
