package session

import (
	"context"
	"path/filepath"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/guard"
	"github.com/masterkeysrd/tasksmith/internal/core/vcs"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
)

// sandboxFilesystemPolicy assembles the native kernel path policy for a
// session work directory: the read-masking floor over host credential stores,
// the non-overridable protected write paths (workspace agent resources, Git
// hooks/config, shell startup dotfiles, and TaskSmith's own XDG stores), and —
// when the work directory is a linked git worktree — the shared common git
// directory so `git add`/`git commit` work inside the worktree while its hooks
// and repository config stay tamper-proof.
func sandboxFilesystemPolicy(workDir string) sandbox.FilesystemPolicy {
	policy := sandbox.FilesystemPolicy{
		DeniedReadPaths:  guard.HostCredentialPaths(workDir),
		DeniedWritePaths: guard.ProtectedWorkspaceWritePaths(workDir),
	}
	commonDir, extraDeniedWrites := linkedGitWorktreePaths(workDir)
	if commonDir != "" {
		policy.ExtraWritePaths = append(policy.ExtraWritePaths, commonDir)
		policy.DeniedWritePaths = append(policy.DeniedWritePaths, extraDeniedWrites...)
	}
	return policy
}

// linkedGitWorktreePaths detects whether workDir is a linked git worktree
// (where <workDir>/.git is a regular file whose gitdir points into
// <main-repo>/.git/worktrees/<name>) and returns the repository's common git
// directory plus the write-denied subpaths inside it. Outside a linked
// worktree — a plain directory, a normal checkout, or a bare hub — it returns
// empty values, since those topologies keep their Git state inside the sandbox
// root (or outside it entirely).
func linkedGitWorktreePaths(workDir string) (commonDir string, deniedWritePaths []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, err := vcs.Inspect(ctx, workDir)
	if err != nil || info == nil || info.Type != vcs.RepoTypeWorktree {
		return "", nil
	}
	commonDir = info.CommonDir
	if commonDir == "" {
		return "", nil
	}
	return commonDir, []string{
		filepath.Join(commonDir, "hooks"),
		filepath.Join(commonDir, "config"),
	}
}
