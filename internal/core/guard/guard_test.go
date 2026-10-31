package guard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
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
	ws := "/tmp/workspace"
	home := "/Users/tester"

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
		{"/Users/tester/.ssh/id_rsa", true},
		{"/Users/tester/.aws/credentials", true},
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
		{"~/.ssh/id_rsa", api.ActionRead, "/Users/tester/.ssh/id_rsa", api.SafetyUnsafe},
		{"~", api.ActionRead, "/Users/tester", api.SafetyOutside},
	}

	for _, tc := range evalCases {
		gotPath, gotSafety := EvaluatePath(tc.target, ws, home, tc.action)
		if gotPath != tc.wantPath || gotSafety != tc.wantSafety {
			t.Errorf("EvaluatePath(%q, %s) = (%q, %s), want (%q, %s)",
				tc.target, tc.action, gotPath, gotSafety, tc.wantPath, tc.wantSafety)
		}
	}
}
