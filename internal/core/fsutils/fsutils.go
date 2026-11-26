package fsutils

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
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

// ResolveConfigPath expands a leading "~" or "~/" in targetPath using the
// current user's home directory, then resolves the result against baseDir when
// targetPath is relative and baseDir is non-empty. It is meant for paths authored
// in configuration files, where both "~" and a path relative to the declaring
// file are natural to write. Failing to determine the home directory leaves "~"
// untouched rather than failing the caller.
func ResolveConfigPath(targetPath, baseDir string) string {
	p := strings.TrimSpace(targetPath)
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return ExpandAndCleanPath(p, baseDir, home)
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

// HasDanglingSymlinkUnder reports whether any existing path component at or
// below baseDir along targetPath is a symlink that cannot be resolved cleanly
// inside baseDir.
func HasDanglingSymlinkUnder(targetPath, baseDir string) bool {
	rel, err := filepath.Rel(baseDir, targetPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	cur := baseDir
	for part := range strings.SplitSeq(filepath.ToSlash(rel), "/") {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return false
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if _, evalErr := filepath.EvalSymlinks(cur); evalErr != nil {
				return true
			}
		}
	}
	return false
}

// IsConfinedInsideDir reports whether targetPath resides inside baseDir without
// escaping via a dangling symlink, and its canonical (symlink-resolved) form
// stays inside the canonical baseDir. When baseDir does not exist yet, a
// canonical base nested inside the canonical target (a not-yet-created
// intermediate directory) is still considered confined.
func IsConfinedInsideDir(targetPath, baseDir string) bool {
	if baseDir == "" {
		return false
	}
	cleanBase := filepath.Clean(baseDir)
	if !IsInsideDir(targetPath, cleanBase) {
		return false
	}
	if HasDanglingSymlinkUnder(targetPath, cleanBase) {
		return false
	}
	canonBase := ResolveDeepestExisting(cleanBase)
	canonTarget := ResolveDeepestExisting(targetPath)
	if _, err := os.Stat(cleanBase); err == nil {
		return IsInsideDir(canonTarget, canonBase)
	}
	return IsInsideDir(canonTarget, canonBase) || IsInsideDir(canonBase, canonTarget)
}

// PortablePath converts targetPath into a portable representation. If targetPath
// is located within rootDir, it returns the relative path from rootDir. Otherwise,
// if located within homeDir, it returns the path prefixed with "~". If it cannot
// be made relative, the cleaned targetPath is returned.
func PortablePath(targetPath, rootDir, homeDir string) string {
	if strings.TrimSpace(targetPath) == "" {
		return ""
	}
	cleanTarget := filepath.Clean(targetPath)
	if rootDir != "" {
		cleanRoot := filepath.Clean(rootDir)
		if rel, err := filepath.Rel(cleanRoot, cleanTarget); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	if homeDir != "" {
		cleanHome := filepath.Clean(homeDir)
		if rel, err := filepath.Rel(cleanHome, cleanTarget); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
	}
	return cleanTarget
}

// ResolveDeepestExisting canonicalizes p by evaluating symlinks on p if it
// exists, or on its deepest existing ancestor while appending the remaining
// (not-yet-created) path components lexically. This keeps path confinement and
// OS kernel sandbox rules anchored to canonical vnode paths even when a parent
// directory is a symlink (e.g. /var -> /private/var on macOS) and the leaf
// does not exist yet.
func ResolveDeepestExisting(p string) string {
	p = filepath.Clean(strings.TrimSpace(p))
	if p == "" {
		return p
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(resolved)
	}
	var rest []string
	cur := p
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Clean(filepath.Join(append([]string{resolved}, rest...)...))
		}
	}
}

// WriteFileAtomic writes data to dstPath atomically via a sibling temporary file
// and rename, setting file permissions to perm. The parent directory must exist.
func WriteFileAtomic(dstPath string, data []byte, perm fs.FileMode) error {
	root, err := os.OpenRoot(filepath.Dir(dstPath))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	return WriteFileAtomicInRoot(root, filepath.Base(dstPath), data, perm)
}

func WriteFileAtomicInRoot(root *os.Root, name string, data []byte, perm fs.FileMode) error {
	tmpName := ".tmp-write-" + rand.Text()
	tmp, err := root.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("fsutils: create temp file: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = tmp.Close()
			_ = root.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("fsutils: write temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		return fmt.Errorf("fsutils: chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("fsutils: close temp file: %w", err)
	}
	if err := root.Rename(tmpName, name); err != nil {
		return fmt.Errorf("fsutils: rename temp file: %w", err)
	}
	cleanup = false
	return nil
}

// CopyFile copies srcPath to dstPath atomically via a sibling temporary file
// and rename, setting destination permissions to perm.
func CopyFile(srcPath, dstPath string, perm fs.FileMode) error {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = in.Close()
	}()

	tmp, err := os.CreateTemp(filepath.Dir(dstPath), ".tmp-copy-*")
	if err != nil {
		return fmt.Errorf("fsutils: create temp copy: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := io.Copy(tmp, in); err != nil {
		return fmt.Errorf("fsutils: copy data: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		return fmt.Errorf("fsutils: chmod temp copy: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("fsutils: close temp copy: %w", err)
	}
	if err := os.Rename(tmpName, dstPath); err != nil {
		return fmt.Errorf("fsutils: rename temp copy: %w", err)
	}
	cleanup = false
	return nil
}

// LinkOrCopy hardlinks srcPath to dstPath in O(1), falling back to [CopyFile]
// when hardlinking fails (such as across filesystems).
func LinkOrCopy(srcPath, dstPath string, perm fs.FileMode) error {
	if err := os.Link(srcPath, dstPath); err == nil {
		return nil
	}
	return CopyFile(srcPath, dstPath, perm)
}

func ReadRegularFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %s", path)
	}
	return os.ReadFile(path)
}

func WriteJSONAtomic(path string, v any, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("fsutils: marshal %q: %w", path, err)
	}
	return WriteFileAtomic(path, append(data, '\n'), perm)
}
