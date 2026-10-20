package vcs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepoType classifies the version-control topology of a directory.
type RepoType string

const (
	// RepoTypeNone indicates a plain directory (not a Git repo, or git unavailable).
	RepoTypeNone RepoType = "none"
	// RepoTypeNormal indicates a standard Git working tree.
	RepoTypeNormal RepoType = "normal"
	// RepoTypeWorktree indicates a linked Git worktree.
	RepoTypeWorktree RepoType = "worktree"
	// RepoTypeBare indicates a bare Git repository or worktree hub (e.g. .bare/ or core.bare=true).
	RepoTypeBare RepoType = "bare"
)

// ErrNotGitRepo is returned when a Git operation is invoked on a non-Git directory.
var ErrNotGitRepo = errors.New("vcs: not a git repository")

// RepoInfo describes the Git topology and state of an inspected directory.
type RepoInfo struct {
	Type      RepoType `json:"type"`
	RootDir   string   `json:"root_dir"`   // Working tree root (or directory path for RepoTypeNone / RepoTypeBare)
	CommonDir string   `json:"common_dir"` // Shared Git directory (e.g. /path/to/repo/.git or /path/to/hub/.bare)
	OriginDir string   `json:"origin_dir"` // Canonical repository root (parent of CommonDir when CommonDir is .git or .bare; equals RootDir for RepoTypeNone)
	Branch    string   `json:"branch,omitempty"`
	Commit    string   `json:"commit,omitempty"`
	Detached  bool     `json:"detached,omitempty"`
}

// Worktree represents an entry from `git worktree list --porcelain`.
type Worktree struct {
	Path     string `json:"path"`
	Head     string `json:"head,omitempty"`
	Branch   string `json:"branch,omitempty"` // Short branch name (stripped of "refs/heads/")
	Bare     bool   `json:"bare,omitempty"`
	Detached bool   `json:"detached,omitempty"`
	Locked   bool   `json:"locked,omitempty"`
	Prunable bool   `json:"prunable,omitempty"`
}

// IsGitAvailable reports whether the `git` executable is available on PATH.
func IsGitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// Inspect determines the Git topology, canonical directories, and HEAD state for dir.
func Inspect(ctx context.Context, dir string) (*RepoInfo, error) {
	absDir, err := resolveDir(dir)
	if err != nil {
		return nil, err
	}

	if !IsGitAvailable() {
		return &RepoInfo{
			Type:      RepoTypeNone,
			RootDir:   absDir,
			OriginDir: absDir,
		}, nil
	}

	// Check for the .bare worktree-hub convention where <dir>/.bare is a bare Git repository.
	dotBare := filepath.Join(absDir, ".bare")
	if fi, statErr := os.Stat(dotBare); statErr == nil && fi.IsDir() {
		if isBareGitDir(ctx, dotBare) {
			commonDir := resolvePath(absDir, dotBare)
			branch, commit, detached := inspectHead(ctx, commonDir)
			return &RepoInfo{
				Type:      RepoTypeBare,
				RootDir:   absDir,
				CommonDir: commonDir,
				OriginDir: absDir,
				Branch:    branch,
				Commit:    commit,
				Detached:  detached,
			}, nil
		}
	}

	out, err := runGit(ctx, absDir, "rev-parse", "--is-inside-work-tree", "--is-bare-repository", "--git-dir", "--git-common-dir")
	if err != nil {
		return &RepoInfo{
			Type:      RepoTypeNone,
			RootDir:   absDir,
			OriginDir: absDir,
		}, nil
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 4 {
		return &RepoInfo{
			Type:      RepoTypeNone,
			RootDir:   absDir,
			OriginDir: absDir,
		}, nil
	}

	insideWorkTree := strings.TrimSpace(lines[0])
	isBare := strings.TrimSpace(lines[1])
	gitDir := resolvePath(absDir, strings.TrimSpace(lines[2]))
	commonDir := resolvePath(absDir, strings.TrimSpace(lines[3]))

	if isBare == "true" {
		originDir := originFromCommonDir(commonDir)
		branch, commit, detached := inspectHead(ctx, commonDir)
		return &RepoInfo{
			Type:      RepoTypeBare,
			RootDir:   absDir,
			CommonDir: commonDir,
			OriginDir: originDir,
			Branch:    branch,
			Commit:    commit,
			Detached:  detached,
		}, nil
	}

	if insideWorkTree == "true" {
		topOut, topErr := runGit(ctx, absDir, "rev-parse", "--show-toplevel")
		rootDir := absDir
		if topErr == nil && strings.TrimSpace(topOut) != "" {
			rootDir = resolvePath(absDir, strings.TrimSpace(topOut))
		}

		repoType := RepoTypeNormal
		if gitDir != commonDir {
			repoType = RepoTypeWorktree
		}

		originDir := originFromCommonDir(commonDir)
		branch, commit, detached := inspectHead(ctx, absDir)

		return &RepoInfo{
			Type:      repoType,
			RootDir:   rootDir,
			CommonDir: commonDir,
			OriginDir: originDir,
			Branch:    branch,
			Commit:    commit,
			Detached:  detached,
		}, nil
	}

	return &RepoInfo{
		Type:      RepoTypeNone,
		RootDir:   absDir,
		OriginDir: absDir,
	}, nil
}

// ListWorktrees returns all worktrees registered with the repository at dir.
func ListWorktrees(ctx context.Context, dir string) ([]Worktree, error) {
	execDir, _, err := gitExecDir(ctx, dir)
	if err != nil {
		return nil, err
	}

	out, err := runGit(ctx, execDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	return parseWorktreePorcelain(out), nil
}

// ListBranches returns the short names of all local branches (refs/heads/) in the repository at dir.
func ListBranches(ctx context.Context, dir string) ([]string, error) {
	execDir, _, err := gitExecDir(ctx, dir)
	if err != nil {
		return nil, err
	}

	out, err := runGit(ctx, execDir, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return []string{}, nil
	}

	rawLines := strings.Split(trimmed, "\n")
	branches := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		b := strings.TrimSpace(line)
		if b != "" {
			branches = append(branches, b)
		}
	}
	return branches, nil
}

// AddWorktree creates a new linked worktree at worktreePath for the repository at dir.
// If createBranch is true, a new branch with the given name is created (`git worktree add -b <branch> <worktreePath>`).
// If createBranch is false, the existing branch is checked out (`git worktree add <worktreePath> <branch>`).
func AddWorktree(ctx context.Context, dir, worktreePath, branch string, createBranch bool) error {
	if strings.TrimSpace(worktreePath) == "" {
		return errors.New("vcs: worktree path must not be empty")
	}
	if strings.TrimSpace(branch) == "" {
		return errors.New("vcs: branch name must not be empty")
	}

	execDir, _, err := gitExecDir(ctx, dir)
	if err != nil {
		return err
	}

	absWorktreePath, err := filepath.Abs(worktreePath)
	if err != nil {
		return fmt.Errorf("vcs: resolve worktree path: %w", err)
	}

	var args []string
	if createBranch {
		// Check whether HEAD resolves to a valid commit. If not (e.g. unborn HEAD
		// or a bare hub whose HEAD points to an unborn ref), check if another
		// local branch exists as a base, or fall back to --orphan.
		if _, headErr := runGit(ctx, execDir, "rev-parse", "--verify", "--quiet", "HEAD"); headErr != nil {
			branches, _ := ListBranches(ctx, dir)
			if len(branches) > 0 {
				args = []string{"worktree", "add", "-b", branch, absWorktreePath, branches[0]}
			} else {
				args = []string{"worktree", "add", "--orphan", "-b", branch, absWorktreePath}
			}
		} else {
			args = []string{"worktree", "add", "-b", branch, absWorktreePath}
		}
	} else {
		args = []string{"worktree", "add", absWorktreePath, branch}
	}

	_, err = runGit(ctx, execDir, args...)
	return err
}

// RemoveWorktree removes a linked worktree at worktreePath from the repository at dir.
func RemoveWorktree(ctx context.Context, dir, worktreePath string, force bool) error {
	if strings.TrimSpace(worktreePath) == "" {
		return errors.New("vcs: worktree path must not be empty")
	}

	execDir, _, err := gitExecDir(ctx, dir)
	if err != nil {
		return err
	}

	absWorktreePath, err := filepath.Abs(worktreePath)
	if err != nil {
		return fmt.Errorf("vcs: resolve worktree path: %w", err)
	}
	if eval, evalErr := filepath.EvalSymlinks(absWorktreePath); evalErr == nil {
		absWorktreePath = eval
	}

	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, absWorktreePath)

	_, err = runGit(ctx, execDir, args...)
	return err
}

// SwitchBranch switches the working tree at dir to branch, creating it if create is true.
func SwitchBranch(ctx context.Context, dir, branch string, create bool) error {
	if strings.TrimSpace(branch) == "" {
		return errors.New("vcs: branch name must not be empty")
	}

	info, err := Inspect(ctx, dir)
	if err != nil {
		return err
	}
	if info.Type == RepoTypeNone {
		return fmt.Errorf("%w: %s", ErrNotGitRepo, dir)
	}
	if info.Type == RepoTypeBare {
		return fmt.Errorf("vcs: cannot switch branch in bare repository %s", dir)
	}

	var args []string
	if create {
		args = []string{"checkout", "-b", branch}
	} else {
		args = []string{"checkout", branch}
	}

	_, err = runGit(ctx, info.RootDir, args...)
	return err
}

// Diff returns the unstaged and staged diff against HEAD for the working tree at dir.
// If dir is RepoTypeNone or RepoTypeBare, Diff returns ("", nil).
func Diff(ctx context.Context, dir string) (string, error) {
	info, err := Inspect(ctx, dir)
	if err != nil {
		return "", err
	}
	if info.Type == RepoTypeNone || info.Type == RepoTypeBare {
		return "", nil
	}

	// If HEAD exists, diff against HEAD (includes both staged and unstaged changes).
	// If HEAD is unborn, fall back to `git diff`.
	if info.Commit != "" {
		out, diffErr := runGit(ctx, info.RootDir, "diff", "HEAD")
		if diffErr == nil {
			return out, nil
		}
	}

	out, err := runGit(ctx, info.RootDir, "diff")
	if err != nil {
		return "", err
	}
	return out, nil
}

// Status returns the porcelain working-tree status (`git status --porcelain`) for dir.
// If dir is RepoTypeNone or RepoTypeBare, Status returns ("", nil).
func Status(ctx context.Context, dir string) (string, error) {
	info, err := Inspect(ctx, dir)
	if err != nil {
		return "", err
	}
	if info.Type == RepoTypeNone || info.Type == RepoTypeBare {
		return "", nil
	}

	out, err := runGit(ctx, info.RootDir, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	return out, nil
}

func resolveDir(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("vcs: directory path must not be empty")
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("vcs: resolve path %q: %w", dir, err)
	}
	fi, err := os.Stat(absDir)
	if err != nil {
		return "", fmt.Errorf("vcs: stat %q: %w", dir, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("vcs: %q is not a directory", dir)
	}
	if eval, evalErr := filepath.EvalSymlinks(absDir); evalErr == nil {
		absDir = eval
	}
	return filepath.Clean(absDir), nil
}

func resolvePath(baseDir, p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(baseDir, p)
	}
	p = filepath.Clean(p)
	if eval, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(eval)
	}
	return p
}

func originFromCommonDir(commonDir string) string {
	base := filepath.Base(commonDir)
	if base == ".git" || base == ".bare" {
		return filepath.Dir(commonDir)
	}
	return commonDir
}

func isBareGitDir(ctx context.Context, gitDir string) bool {
	out, err := runGit(ctx, gitDir, "rev-parse", "--is-bare-repository")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "true"
}

func inspectHead(ctx context.Context, execDir string) (branch, commit string, detached bool) {
	symOut, symErr := runGit(ctx, execDir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if symErr == nil {
		branch = strings.TrimSpace(symOut)
	}

	revOut, revErr := runGit(ctx, execDir, "rev-parse", "--verify", "--quiet", "HEAD")
	if revErr == nil {
		commit = strings.TrimSpace(revOut)
	}

	if symErr != nil && commit != "" {
		detached = true
	}
	return branch, commit, detached
}

func gitExecDir(ctx context.Context, dir string) (string, *RepoInfo, error) {
	info, err := Inspect(ctx, dir)
	if err != nil {
		return "", nil, err
	}
	switch info.Type {
	case RepoTypeNone:
		return "", nil, fmt.Errorf("%w: %s", ErrNotGitRepo, dir)
	case RepoTypeBare:
		return info.CommonDir, info, nil
	default:
		return info.RootDir, info, nil
	}
}

func parseWorktreePorcelain(out string) []Worktree {
	var worktrees []Worktree
	blocks := strings.SplitSeq(strings.TrimSpace(out), "\n\n")
	for block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var wt Worktree
		for line := range strings.SplitSeq(block, "\n") {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "worktree "):
				p := strings.TrimPrefix(line, "worktree ")
				if eval, err := filepath.EvalSymlinks(p); err == nil {
					p = eval
				}
				wt.Path = filepath.Clean(p)
			case strings.HasPrefix(line, "HEAD "):
				wt.Head = strings.TrimPrefix(line, "HEAD ")
			case strings.HasPrefix(line, "branch "):
				ref := strings.TrimPrefix(line, "branch ")
				wt.Branch = strings.TrimPrefix(ref, "refs/heads/")
			case line == "bare":
				wt.Bare = true
			case line == "detached":
				wt.Detached = true
			case line == "locked" || strings.HasPrefix(line, "locked "):
				wt.Locked = true
			case line == "prunable" || strings.HasPrefix(line, "prunable "):
				wt.Prunable = true
			}
		}
		if wt.Path != "" {
			worktrees = append(worktrees, wt)
		}
	}
	return worktrees
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmdArgs := make([]string, 0, len(args)+2)
	if dir != "" {
		cmdArgs = append(cmdArgs, "-C", dir)
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = strings.TrimSpace(stdout.String())
		}
		if errMsg != "" {
			return "", fmt.Errorf("vcs: git %s: %w: %s", strings.Join(args, " "), err, errMsg)
		}
		return "", fmt.Errorf("vcs: git %s: %w", strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}
