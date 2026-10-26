package fsutils

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrEmptyPath is returned when a provided path is blank or consists only of whitespace.
var ErrEmptyPath = errors.New("fsutils: path must not be empty")

// CleanPath trims whitespace from dir, returns [ErrEmptyPath] if empty,
// resolves it to an absolute path, evaluates symlinks if possible, and returns
// the canonical cleaned path.
func CleanPath(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", ErrEmptyPath
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("fsutils: resolve path %q: %w", dir, err)
	}
	if eval, evalErr := filepath.EvalSymlinks(absDir); evalErr == nil {
		absDir = eval
	}
	return filepath.Clean(absDir), nil
}

// ExpandAndCleanPath trims targetPath, expands ~ with homeDir (if provided),
// and returns a cleaned path relative to baseDir (if targetPath is relative and baseDir is non-empty),
// or a cleaned absolute path otherwise.
func ExpandAndCleanPath(targetPath, baseDir, homeDir string) string {
	p := strings.TrimSpace(targetPath)
	if homeDir != "" {
		homeClean := filepath.Clean(homeDir)
		if p == "~" {
			p = homeClean
		} else if strings.HasPrefix(p, "~/") {
			p = filepath.Join(homeClean, p[2:])
		}
	}
	if filepath.IsAbs(p) || baseDir == "" {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(baseDir, p))
}

// IsInsideDir reports whether targetPath is equal to or located within dir.
// Both paths are cleaned before checking.
func IsInsideDir(targetPath, dir string) bool {
	dirClean := filepath.Clean(dir)
	resClean := filepath.Clean(targetPath)
	if resClean == dirClean {
		return true
	}
	if dirClean == string(filepath.Separator) {
		return strings.HasPrefix(resClean, string(filepath.Separator))
	}
	return strings.HasPrefix(resClean, dirClean+string(filepath.Separator))
}
