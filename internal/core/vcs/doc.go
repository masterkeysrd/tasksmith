// Package vcs provides Git repository topology inspection and worktree, branch,
// diff, and status operations for TaskSmith workspaces.
//
// # Repository Topologies
//
// TaskSmith workspaces can exist in one of four version-control topologies,
// represented by [RepoType] and returned by [Inspect]:
//
//   - [RepoTypeNone] ("none"): A plain directory that is not tracked by Git,
//     or an environment where the git binary is unavailable on PATH.
//   - [RepoTypeNormal] ("normal"): A standard Git working tree with an embedded
//     .git directory.
//   - [RepoTypeWorktree] ("worktree"): A linked Git worktree created via
//     git-worktree(1) whose Git metadata resides in a shared common directory.
//   - [RepoTypeBare] ("bare"): A bare Git repository (core.bare=true) or a
//     worktree hub directory containing a .bare repository subdirectory.
//
// # Canonical Origin Resolution
//
// When multiple linked worktrees belong to the same underlying repository,
// [RepoInfo.CommonDir] identifies the shared Git metadata directory and
// [RepoInfo.OriginDir] resolves the canonical repository root (the parent of
// .git or .bare). This allows TaskSmith to group sibling worktrees under a
// single logical project regardless of which worktree path was opened.
//
// # Non-Interactive Subprocess Execution
//
// All Git commands are executed via os/exec with context cancellation and
// non-interactive environment overrides (GIT_TERMINAL_PROMPT=0 and
// GIT_SSH_COMMAND="ssh -o BatchMode=yes") so daemon operations never block on
// interactive credential prompts.
package vcs
