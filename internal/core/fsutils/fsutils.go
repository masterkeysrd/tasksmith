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
