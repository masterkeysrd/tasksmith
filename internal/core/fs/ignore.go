package fs

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// defaultIgnoreNames is the predefined set of file and directory names that are
// always excluded, regardless of gitignore rules.
var defaultIgnoreNames = map[string]struct{}{
	".git":         {},
	".env":         {},
	"node_modules": {},
	"__pycache__":  {},
	"vendor":       {},
	"dist":         {},
	"build":        {},
	"target":       {},
	".next":        {},
	".nuxt":        {},
	".DS_Store":    {},
	".venv":        {},
	"venv":         {},
	"coverage":     {},
}

// Ignorer determines whether a filesystem entry should be excluded.
type Ignorer interface {
	// ShouldIgnore returns true when the entry identified by name, fullPath and
	// isDir must be excluded from directory listings or search results.
	ShouldIgnore(name, fullPath string, isDir bool) bool
}

// NewIgnorer constructs an Ignorer for entries inside dir.
// It applies predefined ignore rules and, when dir is inside a git repository,
// all .gitignore files from the repo root down to dir (full git semantics).
// Errors during gitignore loading are non-fatal: the returned Ignorer still
// enforces the predefined rules.
func NewIgnorer(dir string) (Ignorer, error) {
	repoRoot, err := findGitRoot(dir)
	if err != nil || repoRoot == "" {
		return &ignorer{}, nil
	}

	patterns, err := loadGitignorePatternsForDir(repoRoot, dir)
	if err != nil || len(patterns) == 0 {
		return &ignorer{repoRoot: repoRoot}, nil
	}

	return &ignorer{
		matcher:  gitignore.NewMatcher(patterns),
		repoRoot: repoRoot,
	}, nil
}

// ignorer is the concrete implementation of Ignorer.
type ignorer struct {
	matcher  gitignore.Matcher
	repoRoot string
}

// ShouldIgnore implements Ignorer.
func (ig *ignorer) ShouldIgnore(name, fullPath string, isDir bool) bool {
	if _, ok := defaultIgnoreNames[name]; ok {
		return true
	}

	if ig.matcher == nil || ig.repoRoot == "" {
		return false
	}

	rel, err := filepath.Rel(ig.repoRoot, fullPath)
	if err != nil {
		return false
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")
	return ig.matcher.Match(parts, isDir)
}

// findGitRoot walks upward from dir until it finds a directory containing .git.
// Returns "" (no error) when no git repository is found.
func findGitRoot(dir string) (string, error) {
	current := dir
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", nil
		}
		current = parent
	}
}

// loadGitignorePatternsForDir loads gitignore patterns applicable to entries in
// dir, reading:
//   - .git/info/exclude  (repo-level excludes)
//   - .gitignore at the repo root
//   - .gitignore in every directory on the path from repo root down to dir
func loadGitignorePatternsForDir(repoRoot, dir string) ([]gitignore.Pattern, error) {
	var patterns []gitignore.Pattern

	// Repo-level excludes (.git/info/exclude).
	if pats, err := readGitignoreFile(filepath.Join(repoRoot, ".git", "info", "exclude"), nil); err == nil {
		patterns = append(patterns, pats...)
	}

	// Root-level .gitignore (empty domain — patterns apply from repo root).
	if pats, err := readGitignoreFile(filepath.Join(repoRoot, ".gitignore"), nil); err == nil {
		patterns = append(patterns, pats...)
	}

	rel, err := filepath.Rel(repoRoot, dir)
	if err != nil || rel == "." {
		return patterns, nil
	}

	pathParts := strings.Split(filepath.ToSlash(rel), "/")
	current := repoRoot
	for i, part := range pathParts {
		domain := make([]string, i+1)
		copy(domain, pathParts[:i+1])
		current = filepath.Join(current, part)
		if pats, err := readGitignoreFile(filepath.Join(current, ".gitignore"), domain); err == nil {
			patterns = append(patterns, pats...)
		}
	}

	return patterns, nil
}

// readGitignoreFile parses a .gitignore file into gitignore.Pattern values.
// domain is the slice of path components from the repo root to the directory
// that owns this .gitignore file.
func readGitignoreFile(path string, domain []string) ([]gitignore.Pattern, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var patterns []gitignore.Pattern
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		d := make([]string, len(domain))
		copy(d, domain)
		patterns = append(patterns, gitignore.ParsePattern(trimmed, d))
	}
	return patterns, scanner.Err()
}
