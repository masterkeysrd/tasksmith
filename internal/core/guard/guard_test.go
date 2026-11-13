package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

func TestReadDirSymlinkBoundaries(t *testing.T) {
	workspace := t.TempDir()
	readDir := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(readDir, "escape")); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink(readDir, filepath.Join(workspace, "alias")); err != nil {
		t.Skip(err)
	}
	file := filepath.Join(readDir, "resource")
	if err := os.WriteFile(file, []byte("resource"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := PathConfig{WorkspaceDir: workspace, ReadDirs: []string{readDir}}
	if _, safety := EvaluatePathWithConfig(filepath.Join(readDir, "escape"), api.ActionRead, cfg); safety != api.SafetyUnsafe {
		t.Fatalf("symlink escape classified %s", safety)
	}
	if _, safety := EvaluatePathWithConfig(filepath.Join(workspace, "alias", "resource"), api.ActionWrite, cfg); safety != api.SafetyUnsafe {
		t.Fatalf("symlink write classified %s", safety)
	}
}

func TestActionTypeAndSafetyLevel(t *testing.T) {
	actions := []struct {
		action api.ActionType
		str    string
		sev    int
	}{
		{api.ActionRead, "read", 0},
		{api.ActionUnknown, "unknown", 1},
		{api.ActionNet, "net", 2},
		{api.ActionWrite, "write", 3},
		{api.ActionExec, "exec", 4},
		{api.ActionDelete, "delete", 5},
		{api.ActionType("invalid"), "unknown", 1},
	}

	for _, tc := range actions {
		if got := tc.action.String(); got != tc.str {
			t.Errorf("ActionType(%q).String() = %q, want %q", tc.action, got, tc.str)
		}
		if got := tc.action.Severity(); got != tc.sev {
			t.Errorf("ActionType(%q).Severity() = %d, want %d", tc.action, got, tc.sev)
		}
	}

	safetys := []struct {
		safety api.SafetyLevel
		str    string
		sev    int
	}{
		{api.SafetySafe, "safe", 0},
		{api.SafetyOutside, "outside", 1},
		{api.SafetyUnknown, "unknown", 2},
		{api.SafetyUnsafe, "unsafe", 3},
		{api.SafetyLevel("invalid"), "unknown", 2},
	}

	for _, tc := range safetys {
		if got := tc.safety.String(); got != tc.str {
			t.Errorf("SafetyLevel(%q).String() = %q, want %q", tc.safety, got, tc.str)
		}
		if got := tc.safety.Severity(); got != tc.sev {
			t.Errorf("SafetyLevel(%q).Severity() = %d, want %d", tc.safety, got, tc.sev)
		}
	}
}

func TestIsProtectedPath(t *testing.T) {
	ws := "/tmp/workspace"
	cases := []struct {
		path string
		want bool
	}{
		{".git", true},
		{".git/config", true},
		{"sub/.git/HEAD", true},
		{".bare", true},
		{".bare/HEAD", true},
		{".agents", true},
		{".agents/rules.md", true},
		{"/tmp/workspace/.git/config", true},
		{"/tmp/workspace/.agents/skills/a.md", true},
		{".github/workflows/ci.yml", false},
		{".gitignore", false},
		{"internal/main.go", false},
		{"/tmp/workspace/internal/main.go", false},
	}

	for _, tc := range cases {
		if got := IsProtectedPath(tc.path, ws); got != tc.want {
			t.Errorf("IsProtectedPath(%q, %q) = %v, want %v", tc.path, ws, got, tc.want)
		}
	}

	if !IsProtectedPath(".vault/key", ws, ".vault") {
		t.Error("expected .vault/key to be protected with custom protectedDirs")
	}
	if IsProtectedPath(".git/config", ws, ".vault") {
		t.Error("expected .git/config not to be protected when overridden by custom protectedDirs")
	}
}

func TestIsSensitivePathAndEvaluatePath(t *testing.T) {
	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-guard-test")
	t.Cleanup(xdg.ClearCache)

	configDir, err := xdg.Home(xdg.VarTypeConfig)
	if err != nil {
		t.Fatal(err)
	}
	dataDir, err := xdg.Home(xdg.VarTypeData)
	if err != nil {
		t.Fatal(err)
	}

	ws := "/tmp/workspace"
	home := filepath.Join(string(filepath.Separator), "mock", "home", "tester")

	sensitiveCases := []struct {
		path string
		want bool
	}{
		{"/etc/hosts", true},
		{"/etc/passwd", true},
		{"/private/etc/hosts", true},
		{"/proc/self/environ", true},
		{"/dev/sda", true},
		{"~/.ssh/id_rsa", true},
		{filepath.Join(home, ".ssh", "id_rsa"), true},
		{filepath.Join(home, ".aws", "credentials"), true},
		{filepath.Join(home, ".kube", "config"), true},
		{filepath.Join(home, ".docker", "config.json"), true},
		{filepath.Join(home, ".netrc"), true},
		{filepath.Join(configDir, ".env"), true},
		{filepath.Join(dataDir, "credentials", "openai.json"), true},
		{filepath.Join(dataDir, "mcp", "tokens.json"), true},
		{filepath.Join(configDir, "settings.yaml"), false},
		{"/tmp/somefile.txt", false},
		{"/tmp/workspace/file.txt", false},
	}

	for _, tc := range sensitiveCases {
		if got := IsSensitivePath(tc.path, home); got != tc.want {
			t.Errorf("IsSensitivePath(%q, %q) = %v, want %v", tc.path, home, got, tc.want)
		}
	}

	evalCases := []struct {
		target     string
		action     api.ActionType
		wantPath   string
		wantSafety api.SafetyLevel
	}{
		{"file.txt", api.ActionRead, "/tmp/workspace/file.txt", api.SafetySafe},
		{"file.txt", api.ActionWrite, "/tmp/workspace/file.txt", api.SafetySafe},
		{".git/config", api.ActionRead, "/tmp/workspace/.git/config", api.SafetySafe},
		{".git/config", api.ActionWrite, "/tmp/workspace/.git/config", api.SafetyUnsafe},
		{"../somefile.txt", api.ActionRead, "/tmp/somefile.txt", api.SafetyOutside},
		{"../somefile.txt", api.ActionWrite, "/tmp/somefile.txt", api.SafetyUnsafe},
		{"/etc/hosts", api.ActionRead, "/etc/hosts", api.SafetyUnsafe},
		{"~/.ssh/id_rsa", api.ActionRead, filepath.Join(home, ".ssh", "id_rsa"), api.SafetyUnsafe},
		{filepath.Join(configDir, ".env"), api.ActionRead, filepath.Join(configDir, ".env"), api.SafetyUnsafe},
		{filepath.Join(dataDir, "credentials", "token.json"), api.ActionRead, filepath.Join(dataDir, "credentials", "token.json"), api.SafetyUnsafe},
		{"~", api.ActionRead, home, api.SafetyOutside},
	}

	for _, tc := range evalCases {
		gotPath, gotSafety := EvaluatePath(tc.target, ws, home, tc.action)
		if gotPath != tc.wantPath || gotSafety != tc.wantSafety {
			t.Errorf("EvaluatePath(%q, %s) = (%q, %s), want (%q, %s)",
				tc.target, tc.action, gotPath, gotSafety, tc.wantPath, tc.wantSafety)
		}
	}
}

func TestHostCredentialPaths(t *testing.T) {
	xdgRoot := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	xdg.SetTestEnv(xdgRoot, "tasksmith-guard-creds-test")
	t.Cleanup(xdg.ClearCache)

	configDir, err := xdg.Home(xdg.VarTypeConfig)
	if err != nil {
		t.Fatal(err)
	}
	dataDir, err := xdg.Home(xdg.VarTypeData)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(fakeHome, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".env"), []byte("GLOBAL=1"), 0o600); err != nil {
		t.Fatal(err)
	}

	workDir := t.TempDir()
	got := HostCredentialPaths(workDir)

	var want []string
	for _, name := range DefaultSensitiveHomeDirs {
		want = append(want, filepath.Join(fakeHome, name))
	}
	want = append(want,
		filepath.Join(configDir, ".env"),
		filepath.Join(dataDir, "credentials"),
		filepath.Join(dataDir, "mcp"),
	)
	for _, w := range want {
		if !containsPath(got, w) {
			t.Errorf("HostCredentialPaths missing %q, got %v", w, got)
		}
	}
	for _, p := range got {
		if !filepath.IsAbs(p) {
			t.Errorf("denied path must be absolute: %q", p)
		}
	}
}

func TestHostCredentialPathsSkipsWorkspaceOverlaps(t *testing.T) {
	xdgRoot := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	xdg.SetTestEnv(xdgRoot, "tasksmith-guard-creds-overlap")
	t.Cleanup(xdg.ClearCache)

	sshDir := filepath.Join(fakeHome, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}

	if got := HostCredentialPaths(sshDir); containsPath(got, sshDir) {
		t.Errorf("workspace inside denied dir must be exempt: %v", got)
	}
	if got := HostCredentialPaths(fakeHome); containsPath(got, sshDir) {
		t.Errorf("credential dir inside workspace must be exempt: %v", got)
	}
}

func TestHostCredentialPathsSkipsSymlinkedCredentialIntoWorkspace(t *testing.T) {
	xdgRoot := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	xdg.SetTestEnv(xdgRoot, "tasksmith-guard-creds-symlink")
	t.Cleanup(xdg.ClearCache)

	workDir := t.TempDir()
	realKeys := filepath.Join(workDir, "ssh-keys")
	if err := os.MkdirAll(realKeys, 0o700); err != nil {
		t.Fatal(err)
	}
	sshLink := filepath.Join(fakeHome, ".ssh")
	if err := os.Symlink(realKeys, sshLink); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if got := HostCredentialPaths(workDir); containsPath(got, sshLink) {
		t.Errorf("~/.ssh symlinked into the workspace must stay readable: %v", got)
	}
	other := t.TempDir()
	if got := HostCredentialPaths(other); !containsPath(got, sshLink) {
		t.Errorf("~/.ssh symlink must be masked for unrelated workspaces: %v", got)
	}
}

func TestProtectedWorkspaceWritePaths(t *testing.T) {
	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-guard-protected-write-test")
	t.Cleanup(xdg.ClearCache)

	configDir, err := xdg.Home(xdg.VarTypeConfig)
	if err != nil {
		t.Fatal(err)
	}
	dataDir, err := xdg.Home(xdg.VarTypeData)
	if err != nil {
		t.Fatal(err)
	}

	workDir := t.TempDir()
	got := ProtectedWorkspaceWritePaths(workDir)

	want := []string{
		filepath.Join(workDir, ".agents"),
		filepath.Join(workDir, "WORKSPACE.md"),
		filepath.Join(workDir, ".git", "hooks"),
		filepath.Join(workDir, ".git", "config"),
		configDir,
		dataDir,
	}
	for _, name := range ProtectedWorkspaceWriteFiles {
		want = append(want, filepath.Join(workDir, name))
	}
	for _, w := range want {
		if !containsPath(got, w) {
			t.Errorf("ProtectedWorkspaceWritePaths missing %q, got %v", w, got)
		}
	}
	for _, p := range got {
		if !filepath.IsAbs(p) {
			t.Errorf("protected write path must be absolute: %q", p)
		}
	}
}

func containsPath(paths []string, want string) bool {
	want = fsutils.ResolveDeepestExisting(want)
	for _, p := range paths {
		if strings.EqualFold(filepath.Clean(p), want) {
			return true
		}
	}
	return false
}

func TestTasksmithConfigPathsSensitiveForWrites(t *testing.T) {
	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-guard-sandbox-cfg")
	t.Cleanup(xdg.ClearCache)

	configDir, err := xdg.Home(xdg.VarTypeConfig)
	if err != nil {
		t.Fatal(err)
	}
	dataDir, err := xdg.Home(xdg.VarTypeData)
	if err != nil {
		t.Fatal(err)
	}

	sandboxJSON := filepath.Join(configDir, "sandbox.json")
	workspaces := filepath.Join(dataDir, "workspaces")
	nested := filepath.Join(dataDir, "workspaces", "abc123", "permissions.json")

	cfgPaths := TasksmithConfigPaths()
	for _, want := range []string{sandboxJSON, workspaces} {
		found := false
		for _, p := range cfgPaths {
			if filepath.Clean(p) == filepath.Clean(want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("TasksmithConfigPaths = %v, want %q present", cfgPaths, want)
		}
	}

	for _, p := range []string{sandboxJSON, workspaces, nested} {
		if !IsSensitiveWritePath(p, "") {
			t.Errorf("IsSensitiveWritePath(%q) = false, want true", p)
		}
		if IsSensitivePath(p, "") {
			t.Errorf("IsSensitivePath(%q) = true, want false (reads must stay permitted)", p)
		}
	}

	ordinary := filepath.Join(t.TempDir(), "main.go")
	if IsSensitiveWritePath(ordinary, "") {
		t.Errorf("IsSensitiveWritePath(%q) = true, want false for ordinary paths", ordinary)
	}

	for _, action := range []api.ActionType{api.ActionWrite, api.ActionDelete} {
		if _, safety := EvaluatePathWithConfig(sandboxJSON, action, PathConfig{}); safety != api.SafetyUnsafe {
			t.Errorf("EvaluatePathWithConfig(sandbox.json, %v) = %v, want SafetyUnsafe", action, safety)
		}
		if _, safety := EvaluatePathWithConfig(nested, action, PathConfig{}); safety != api.SafetyUnsafe {
			t.Errorf("EvaluatePathWithConfig(workspaces nested, %v) = %v, want SafetyUnsafe", action, safety)
		}
	}
	if _, safety := EvaluatePathWithConfig(sandboxJSON, api.ActionRead, PathConfig{}); safety == api.SafetyUnsafe {
		t.Errorf("EvaluatePathWithConfig(sandbox.json, read) = SafetyUnsafe, want reads permitted")
	}
}
