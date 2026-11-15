package vcs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func evalTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if eval, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Clean(eval)
	}
	return filepath.Clean(dir)
}

func runTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_TERMINAL_PROMPT=0",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s failed: %v\n%s", args, dir, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func initNormalRepo(t *testing.T, dir string, initialBranch string) {
	t.Helper()
	runTestGit(t, dir, "init", "-b", initialBranch)
	runTestGit(t, dir, "config", "user.name", "Test User")
	runTestGit(t, dir, "config", "user.email", "test@example.com")
}

func commitFile(t *testing.T, dir, relPath, content, msg string) string {
	t.Helper()
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
	runTestGit(t, dir, "add", relPath)
	runTestGit(t, dir, "commit", "-m", msg)
	return runTestGit(t, dir, "rev-parse", "HEAD")
}

func TestInspect_ValidationAndNone(t *testing.T) {
	ctx := context.Background()

	// Empty path
	if _, err := Inspect(ctx, ""); err == nil {
		t.Fatal("expected error for empty directory path")
	}

	// Non-existent path
	base := evalTempDir(t)
	if _, err := Inspect(ctx, filepath.Join(base, "does-not-exist")); err == nil {
		t.Fatal("expected error for non-existent directory")
	}

	// File instead of directory
	filePath := filepath.Join(base, "file.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := Inspect(ctx, filePath); err == nil {
		t.Fatal("expected error when inspecting a regular file")
	}

	// Plain non-git directory
	info, err := Inspect(ctx, base)
	if err != nil {
		t.Fatalf("Inspect plain dir: %v", err)
	}
	if info.Type != RepoTypeNone {
		t.Fatalf("expected type %q, got %q", RepoTypeNone, info.Type)
	}
	if info.RootDir != base {
		t.Fatalf("expected RootDir %q, got %q", base, info.RootDir)
	}
	if info.OriginDir != base {
		t.Fatalf("expected OriginDir %q, got %q", base, info.OriginDir)
	}
	if info.CommonDir != "" {
		t.Fatalf("expected empty CommonDir for RepoTypeNone, got %q", info.CommonDir)
	}
}

func TestInspect_GitUnavailable(t *testing.T) {
	t.Setenv("PATH", "")
	if IsGitAvailable() {
		t.Fatal("expected IsGitAvailable() to be false when PATH is empty")
	}

	base := evalTempDir(t)
	info, err := Inspect(context.Background(), base)
	if err != nil {
		t.Fatalf("Inspect with empty PATH: %v", err)
	}
	if info.Type != RepoTypeNone {
		t.Fatalf("expected RepoTypeNone, got %q", info.Type)
	}
	if info.RootDir != base || info.OriginDir != base {
		t.Fatalf("unexpected RootDir/OriginDir: %+v", info)
	}
}

func TestInspect_NormalRepoAndDetachedHead(t *testing.T) {
	ctx := context.Background()
	repoDir := evalTempDir(t)
	initNormalRepo(t, repoDir, "main")

	// 1. Unborn branch (before first commit)
	unbornInfo, err := Inspect(ctx, repoDir)
	if err != nil {
		t.Fatalf("Inspect unborn repo: %v", err)
	}
	if unbornInfo.Type != RepoTypeNormal {
		t.Fatalf("expected %q, got %q", RepoTypeNormal, unbornInfo.Type)
	}
	if unbornInfo.RootDir != repoDir {
		t.Fatalf("expected RootDir %q, got %q", repoDir, unbornInfo.RootDir)
	}
	if unbornInfo.CommonDir != filepath.Join(repoDir, ".git") {
		t.Fatalf("expected CommonDir %q, got %q", filepath.Join(repoDir, ".git"), unbornInfo.CommonDir)
	}
	if unbornInfo.OriginDir != repoDir {
		t.Fatalf("expected OriginDir %q, got %q", repoDir, unbornInfo.OriginDir)
	}
	if unbornInfo.Branch != "main" {
		t.Fatalf("expected unborn Branch %q, got %q", "main", unbornInfo.Branch)
	}
	if unbornInfo.Commit != "" {
		t.Fatalf("expected empty Commit on unborn branch, got %q", unbornInfo.Commit)
	}
	if unbornInfo.Detached {
		t.Fatal("expected Detached=false on unborn branch")
	}

	// 2. Normal repo after first commit (also test inspecting from a nested subdirectory)
	sha1 := commitFile(t, repoDir, "sub/dir/hello.txt", "hello world\n", "initial commit")
	subDir := filepath.Join(repoDir, "sub", "dir")

	info, err := Inspect(ctx, subDir)
	if err != nil {
		t.Fatalf("Inspect normal repo subdir: %v", err)
	}
	if info.Type != RepoTypeNormal {
		t.Fatalf("expected %q, got %q", RepoTypeNormal, info.Type)
	}
	if info.RootDir != repoDir {
		t.Fatalf("expected RootDir %q, got %q", repoDir, info.RootDir)
	}
	if info.OriginDir != repoDir {
		t.Fatalf("expected OriginDir %q, got %q", repoDir, info.OriginDir)
	}
	if info.Branch != "main" {
		t.Fatalf("expected Branch %q, got %q", "main", info.Branch)
	}
	if info.Commit != sha1 {
		t.Fatalf("expected Commit %q, got %q", sha1, info.Commit)
	}
	if info.Detached {
		t.Fatal("expected Detached=false")
	}

	// 3. Detached HEAD state
	commitFile(t, repoDir, "sub/dir/hello.txt", "second commit\n", "second commit")
	runTestGit(t, repoDir, "checkout", sha1)

	detachedInfo, err := Inspect(ctx, repoDir)
	if err != nil {
		t.Fatalf("Inspect detached repo: %v", err)
	}
	if detachedInfo.Type != RepoTypeNormal {
		t.Fatalf("expected %q, got %q", RepoTypeNormal, detachedInfo.Type)
	}
	if !detachedInfo.Detached {
		t.Fatal("expected Detached=true")
	}
	if detachedInfo.Branch != "" {
		t.Fatalf("expected empty Branch in detached state, got %q", detachedInfo.Branch)
	}
	if detachedInfo.Commit != sha1 {
		t.Fatalf("expected Commit %q, got %q", sha1, detachedInfo.Commit)
	}
}

func TestWorktreeLifecycleAndInspect(t *testing.T) {
	ctx := context.Background()
	parent := evalTempDir(t)
	primaryDir := filepath.Join(parent, "primary")
	if err := os.MkdirAll(primaryDir, 0o755); err != nil {
		t.Fatalf("mkdir primary: %v", err)
	}

	initNormalRepo(t, primaryDir, "main")
	sha := commitFile(t, primaryDir, "README.md", "# Primary\n", "initial")

	wtDir := filepath.Join(parent, "wt-feature")
	if err := AddWorktree(ctx, primaryDir, wtDir, "feature/login", true); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	wtInfo, err := Inspect(ctx, wtDir)
	if err != nil {
		t.Fatalf("Inspect worktree: %v", err)
	}
	if wtInfo.Type != RepoTypeWorktree {
		t.Fatalf("expected %q, got %q", RepoTypeWorktree, wtInfo.Type)
	}
	if wtInfo.RootDir != wtDir {
		t.Fatalf("expected RootDir %q, got %q", wtDir, wtInfo.RootDir)
	}
	if wtInfo.CommonDir != filepath.Join(primaryDir, ".git") {
		t.Fatalf("expected CommonDir %q, got %q", filepath.Join(primaryDir, ".git"), wtInfo.CommonDir)
	}
	if wtInfo.OriginDir != primaryDir {
		t.Fatalf("expected OriginDir %q (primary repo root), got %q", primaryDir, wtInfo.OriginDir)
	}
	if wtInfo.Branch != "feature/login" {
		t.Fatalf("expected Branch %q, got %q", "feature/login", wtInfo.Branch)
	}
	if wtInfo.Commit != sha {
		t.Fatalf("expected Commit %q, got %q", sha, wtInfo.Commit)
	}

	// ListWorktrees
	worktrees, err := ListWorktrees(ctx, primaryDir)
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}
	if len(worktrees) != 2 {
		t.Fatalf("expected 2 worktrees, got %d: %+v", len(worktrees), worktrees)
	}
	if worktrees[0].Path != primaryDir || worktrees[0].Branch != "main" {
		t.Fatalf("unexpected primary worktree entry: %+v", worktrees[0])
	}
	if worktrees[1].Path != wtDir || worktrees[1].Branch != "feature/login" || worktrees[1].Head != sha {
		t.Fatalf("unexpected linked worktree entry: %+v", worktrees[1])
	}

	// ListBranches
	branches, err := ListBranches(ctx, primaryDir)
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if !slices.Contains(branches, "main") || !slices.Contains(branches, "feature/login") {
		t.Fatalf("expected branches to contain main and feature/login, got %v", branches)
	}

	// RemoveWorktree
	if err := RemoveWorktree(ctx, primaryDir, wtDir, false); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}

	worktreesAfter, err := ListWorktrees(ctx, primaryDir)
	if err != nil {
		t.Fatalf("ListWorktrees after remove: %v", err)
	}
	if len(worktreesAfter) != 1 {
		t.Fatalf("expected 1 worktree after removal, got %d", len(worktreesAfter))
	}

	// Re-add worktree using existing branch (createBranch=false)
	wtExistingDir := filepath.Join(parent, "wt-existing")
	if err := AddWorktree(ctx, primaryDir, wtExistingDir, "feature/login", false); err != nil {
		t.Fatalf("AddWorktree existing branch: %v", err)
	}
	wtExistingInfo, err := Inspect(ctx, wtExistingDir)
	if err != nil {
		t.Fatalf("Inspect re-added worktree: %v", err)
	}
	if wtExistingInfo.Type != RepoTypeWorktree || wtExistingInfo.Branch != "feature/login" {
		t.Fatalf("unexpected re-added worktree info: %+v", wtExistingInfo)
	}
}

func TestInspect_BareRepoAndDotBareHub(t *testing.T) {
	ctx := context.Background()
	parent := evalTempDir(t)

	// 1. Standard bare repository (`git init --bare`)
	bareDir := filepath.Join(parent, "project.git")
	if err := os.MkdirAll(bareDir, 0o755); err != nil {
		t.Fatalf("mkdir bareDir: %v", err)
	}
	runTestGit(t, bareDir, "init", "--bare", "-b", "main")

	bareInfo, err := Inspect(ctx, bareDir)
	if err != nil {
		t.Fatalf("Inspect bare repo: %v", err)
	}
	if bareInfo.Type != RepoTypeBare {
		t.Fatalf("expected %q, got %q", RepoTypeBare, bareInfo.Type)
	}
	if bareInfo.RootDir != bareDir {
		t.Fatalf("expected RootDir %q, got %q", bareDir, bareInfo.RootDir)
	}
	if bareInfo.CommonDir != bareDir {
		t.Fatalf("expected CommonDir %q, got %q", bareDir, bareInfo.CommonDir)
	}
	if bareInfo.OriginDir != bareDir {
		t.Fatalf("expected OriginDir %q, got %q", bareDir, bareInfo.OriginDir)
	}

	// 2. `.bare` worktree-hub directory (`mkdir hub && git init --bare hub/.bare`)
	hubDir := filepath.Join(parent, "hub")
	dotBareDir := filepath.Join(hubDir, ".bare")
	if err := os.MkdirAll(dotBareDir, 0o755); err != nil {
		t.Fatalf("mkdir dotBareDir: %v", err)
	}
	runTestGit(t, dotBareDir, "init", "--bare", "-b", "main")

	hubInfo, err := Inspect(ctx, hubDir)
	if err != nil {
		t.Fatalf("Inspect .bare hub: %v", err)
	}
	if hubInfo.Type != RepoTypeBare {
		t.Fatalf("expected %q for .bare hub, got %q", RepoTypeBare, hubInfo.Type)
	}
	if hubInfo.RootDir != hubDir {
		t.Fatalf("expected RootDir %q, got %q", hubDir, hubInfo.RootDir)
	}
	if hubInfo.CommonDir != dotBareDir {
		t.Fatalf("expected CommonDir %q, got %q", dotBareDir, hubInfo.CommonDir)
	}
	if hubInfo.OriginDir != hubDir {
		t.Fatalf("expected OriginDir %q, got %q", hubDir, hubInfo.OriginDir)
	}

	// Add a worktree directly from the `.bare` hub directory (even on unborn repository!)
	mainWT := filepath.Join(hubDir, "main")
	if err := AddWorktree(ctx, hubDir, mainWT, "main", true); err != nil {
		t.Fatalf("AddWorktree on .bare hub: %v", err)
	}
	runTestGit(t, mainWT, "config", "user.name", "Test User")
	runTestGit(t, mainWT, "config", "user.email", "test@example.com")
	commitFile(t, mainWT, "main.go", "package main\n", "init main")

	// Inspect the worktree created inside the `.bare` hub
	mainWTInfo, err := Inspect(ctx, mainWT)
	if err != nil {
		t.Fatalf("Inspect hub worktree: %v", err)
	}
	if mainWTInfo.Type != RepoTypeWorktree {
		t.Fatalf("expected %q, got %q", RepoTypeWorktree, mainWTInfo.Type)
	}
	if mainWTInfo.CommonDir != dotBareDir {
		t.Fatalf("expected CommonDir %q, got %q", dotBareDir, mainWTInfo.CommonDir)
	}
	if mainWTInfo.OriginDir != hubDir {
		t.Fatalf("expected OriginDir %q, got %q", hubDir, mainWTInfo.OriginDir)
	}

	// Add a second worktree from the `.bare` hub now that `main` has a commit
	featWT := filepath.Join(hubDir, "feat")
	if err := AddWorktree(ctx, hubDir, featWT, "feat", true); err != nil {
		t.Fatalf("AddWorktree feat on .bare hub: %v", err)
	}

	wts, err := ListWorktrees(ctx, hubDir)
	if err != nil {
		t.Fatalf("ListWorktrees on .bare hub: %v", err)
	}
	if len(wts) != 3 { // .bare + main + feat
		t.Fatalf("expected 3 worktrees on .bare hub, got %d: %+v", len(wts), wts)
	}
	if !wts[0].Bare {
		t.Fatalf("expected first worktree entry to be bare: %+v", wts[0])
	}

	branches, err := ListBranches(ctx, hubDir)
	if err != nil {
		t.Fatalf("ListBranches on .bare hub: %v", err)
	}
	if !slices.Contains(branches, "main") || !slices.Contains(branches, "feat") {
		t.Fatalf("expected branches [main, feat], got %v", branches)
	}

	// Diff and Status on RepoTypeBare should return ("", nil)
	diffOut, err := Diff(ctx, hubDir)
	if err != nil || diffOut != "" {
		t.Fatalf("expected empty Diff on bare hub, got %q, err=%v", diffOut, err)
	}
	statusOut, err := Status(ctx, hubDir)
	if err != nil || statusOut != "" {
		t.Fatalf("expected empty Status on bare hub, got %q, err=%v", statusOut, err)
	}
}

func TestSwitchBranch_Diff_Status(t *testing.T) {
	ctx := context.Background()
	repoDir := evalTempDir(t)

	// On plain directory, Diff and Status return ("", nil), while git operations return ErrNotGitRepo
	if d, err := Diff(ctx, repoDir); err != nil || d != "" {
		t.Fatalf("expected empty Diff on plain dir, got %q, err=%v", d, err)
	}
	if s, err := Status(ctx, repoDir); err != nil || s != "" {
		t.Fatalf("expected empty Status on plain dir, got %q, err=%v", s, err)
	}
	if _, err := ListWorktrees(ctx, repoDir); !errors.Is(err, ErrNotGitRepo) {
		t.Fatalf("expected ErrNotGitRepo from ListWorktrees, got %v", err)
	}
	if _, err := ListBranches(ctx, repoDir); !errors.Is(err, ErrNotGitRepo) {
		t.Fatalf("expected ErrNotGitRepo from ListBranches, got %v", err)
	}
	if err := SwitchBranch(ctx, repoDir, "main", false); !errors.Is(err, ErrNotGitRepo) {
		t.Fatalf("expected ErrNotGitRepo from SwitchBranch, got %v", err)
	}

	// Initialize normal repo and commit a file
	initNormalRepo(t, repoDir, "main")
	commitFile(t, repoDir, "app.txt", "line 1\n", "initial commit")

	// SwitchBranch (create=true)
	if err := SwitchBranch(ctx, repoDir, "develop", true); err != nil {
		t.Fatalf("SwitchBranch create: %v", err)
	}
	info, err := Inspect(ctx, repoDir)
	if err != nil {
		t.Fatalf("Inspect after SwitchBranch: %v", err)
	}
	if info.Branch != "develop" {
		t.Fatalf("expected branch develop, got %q", info.Branch)
	}

	// SwitchBranch back to main (create=false)
	if err := SwitchBranch(ctx, repoDir, "main", false); err != nil {
		t.Fatalf("SwitchBranch existing: %v", err)
	}
	info, err = Inspect(ctx, repoDir)
	if err != nil {
		t.Fatalf("Inspect after SwitchBranch back: %v", err)
	}
	if info.Branch != "main" {
		t.Fatalf("expected branch main, got %q", info.Branch)
	}

	// Modify tracked file and create untracked file
	if err := os.WriteFile(filepath.Join(repoDir, "app.txt"), []byte("line 1\nline 2\n"), 0o644); err != nil {
		t.Fatalf("write app.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("write untracked.txt: %v", err)
	}

	diffOut, err := Diff(ctx, repoDir)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diffOut, "+line 2") {
		t.Fatalf("expected diff to contain '+line 2', got:\n%s", diffOut)
	}

	statusOut, err := Status(ctx, repoDir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !strings.Contains(statusOut, "M app.txt") || !strings.Contains(statusOut, "?? untracked.txt") {
		t.Fatalf("unexpected Status output:\n%s", statusOut)
	}
}
