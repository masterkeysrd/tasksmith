package vcs

import (
	"os"
	"os/exec"
	"strings"
)

// IsGitAvailable returns true if the git executable is found in the system PATH.
func IsGitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// IsRepo checks if the specified directory is inside a Git repository work tree.
func IsRepo(dir string) bool {
	out, err := runGit(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

// GetBranch retrieves the active branch name for the repository in the specified directory.
func GetBranch(dir string) (string, error) {
	return runGit(dir, "rev-parse", "--abbrev-ref", "HEAD")
}

// GetCommit retrieves the latest commit hash (SHA-1) for the repository in the specified directory.
func GetCommit(dir string) (string, error) {
	return runGit(dir, "rev-parse", "HEAD")
}

// GetDiff returns the combined staged and unstaged diff relative to HEAD.
// If the repository has no commits yet, it falls back to a simple unstaged diff.
func GetDiff(dir string) (string, error) {
	out, err := runGit(dir, "diff", "HEAD")
	if err != nil {
		// Fallback to simple unstaged changes diff
		return runGit(dir, "diff")
	}
	return out, nil
}

// GetStatus returns the porcelain status listing modified and untracked files cleanly.
func GetStatus(dir string) (string, error) {
	return runGit(dir, "status", "--porcelain")
}

// runGit executes a git command in the target directory with non-interactive env settings.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	// Prevent Git from hanging waiting for user passwords or SSH passphrases
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
