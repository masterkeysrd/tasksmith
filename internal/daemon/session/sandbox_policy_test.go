package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
)

var testGitEnv = []string{
	"GIT_CONFIG_NOSYSTEM=1",
	"GIT_CONFIG_GLOBAL=/dev/null",
	"GIT_TERMINAL_PROMPT=0",
	"GIT_CONFIG_COUNT=4",
	"GIT_CONFIG_KEY_0=user.name",
	"GIT_CONFIG_VALUE_0=TaskSmith Test",
	"GIT_CONFIG_KEY_1=user.email",
	"GIT_CONFIG_VALUE_1=test@tasksmith.local",
	"GIT_CONFIG_KEY_2=commit.gpgsign",
	"GIT_CONFIG_VALUE_2=false",
	"GIT_CONFIG_KEY_3=tag.gpgsign",
	"GIT_CONFIG_VALUE_3=false",
}

func runSessionTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), testGitEnv...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func containsExactly(paths []string, want string) bool {
	want = fsutils.ResolveDeepestExisting(want)
	for _, p := range paths {
		if filepath.Clean(p) == want {
			return true
		}
	}
	return false
}

func TestLinkedGitWorktreePaths(t *testing.T) {
	repoDir := t.TempDir()
	runSessionTestGit(t, repoDir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runSessionTestGit(t, repoDir, "add", "README.md")
	runSessionTestGit(t, repoDir, "commit", "-m", "initial")

	// A normal checkout is not a linked worktree.
	if commonDir, denied := linkedGitWorktreePaths(repoDir); commonDir != "" || denied != nil {
		t.Fatalf("normal checkout must not be treated as a linked worktree: commonDir=%q denied=%v", commonDir, denied)
	}

	wtDir := filepath.Join(t.TempDir(), "wt")
	runSessionTestGit(t, repoDir, "worktree", "add", "--detach", wtDir, "HEAD")

	commonDir, denied := linkedGitWorktreePaths(wtDir)
	// macOS temp dirs live behind the /var -> /private/var symlink, so compare
	// canonical forms.
	if resolved := resolveCanonical(t, filepath.Join(repoDir, ".git")); filepath.Clean(commonDir) != resolved {
		t.Fatalf("expected common dir %q (canonical %q), got %q", filepath.Join(repoDir, ".git"), resolved, commonDir)
	}
	if len(denied) != 2 {
		t.Fatalf("expected hooks and config write denies, got %v", denied)
	}
	if !strings.HasSuffix(denied[0], filepath.Join(".git", "hooks")) || !strings.HasSuffix(denied[1], filepath.Join(".git", "config")) {
		t.Fatalf("unexpected denied write paths: %v", denied)
	}
}

func TestLinkedGitWorktreePathsPlainDirectory(t *testing.T) {
	if commonDir, denied := linkedGitWorktreePaths(t.TempDir()); commonDir != "" || denied != nil {
		t.Fatalf("plain directory must yield no worktree paths: commonDir=%q denied=%v", commonDir, denied)
	}
}

func TestSandboxFilesystemPolicyIncludesProtectedPaths(t *testing.T) {
	workDir := t.TempDir()
	policy := sandboxFilesystemPolicy(workDir)

	if len(policy.DeniedReadPaths) == 0 {
		t.Fatalf("expected credential read masks in policy: %+v", policy)
	}
	if !containsExactly(policy.DeniedWritePaths, filepath.Join(workDir, ".agents")) {
		t.Fatalf("policy missing .agents write deny: %v", policy.DeniedWritePaths)
	}
	if !containsExactly(policy.DeniedWritePaths, filepath.Join(workDir, ".git", "hooks")) {
		t.Fatalf("policy missing .git/hooks write deny: %v", policy.DeniedWritePaths)
	}
}

func resolveCanonical(t *testing.T, p string) string {
	t.Helper()
	if eval, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(eval)
	}
	return filepath.Clean(p)
}
