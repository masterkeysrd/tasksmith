package sandbox

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// sandboxFS implements the FS interface backed by Go's os.Root.
type sandboxFS struct {
	s *session
}

func newSandboxFS(s *session) *sandboxFS {
	return &sandboxFS{s: s}
}

// resolvePath validates and converts a caller-supplied path (relative or
// absolute within WorkDir) into a clean path relative to WorkDir suitable for
// os.Root methods. Any traversal outside WorkDir returns ErrPathEscape.
func (f *sandboxFS) resolvePath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || trimmed == "." {
		return ".", nil
	}

	if filepath.IsAbs(trimmed) {
		clean := filepath.Clean(trimmed)
		if rel, ok := relWithin(f.s.workDir, clean); ok {
			return rel, nil
		}
		if f.s.realWorkDir != "" && f.s.realWorkDir != f.s.workDir {
			if rel, ok := relWithin(f.s.realWorkDir, clean); ok {
				return rel, nil
			}
		}
		return "", fmt.Errorf("%w: %q is outside sandbox root %q", ErrPathEscape, path, f.s.workDir)
	}

	clean := filepath.Clean(trimmed)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q escapes sandbox root", ErrPathEscape, path)
	}
	return clean, nil
}

func relWithin(base, target string) (string, bool) {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// wrapRootError translates os.Root escape errors into ErrPathEscape so callers
// can reliably check errors.Is(err, ErrPathEscape).
func wrapRootError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "path escapes from parent") || strings.Contains(msg, "escapes") {
		return fmt.Errorf("%w: %v", ErrPathEscape, err)
	}
	return err
}

func (f *sandboxFS) checkPreconditions(ctx context.Context, writeOp bool) error {
	if f.s.isClosed() {
		return ErrSandboxClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if writeOp && f.s.cfg.ReadOnly {
		return ErrReadOnly
	}
	return nil
}

// Open implements io/fs.FS for read-only standard library compatibility.
func (f *sandboxFS) Open(name string) (fs.File, error) {
	if err := f.checkPreconditions(context.Background(), false); err != nil {
		return nil, err
	}
	rel, err := f.resolvePath(name)
	if err != nil {
		return nil, err
	}
	file, err := f.s.root.Open(rel)
	if err != nil {
		return nil, wrapRootError(err)
	}
	return &sandboxFile{File: file, readOnly: true}, nil
}

func (f *sandboxFS) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if err := f.checkPreconditions(ctx, false); err != nil {
		return nil, err
	}
	rel, err := f.resolvePath(path)
	if err != nil {
		return nil, err
	}
	data, err := f.s.root.ReadFile(rel)
	if err != nil {
		return nil, wrapRootError(err)
	}
	return data, nil
}

func (f *sandboxFS) WriteFile(ctx context.Context, path string, data []byte, perm fs.FileMode) error {
	if err := f.checkPreconditions(ctx, true); err != nil {
		return err
	}
	rel, err := f.resolvePath(path)
	if err != nil {
		return err
	}
	return wrapRootError(f.s.root.WriteFile(rel, data, perm))
}

func (f *sandboxFS) OpenFile(ctx context.Context, path string, flag int, perm fs.FileMode) (File, error) {
	isWrite := flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0
	if err := f.checkPreconditions(ctx, isWrite); err != nil {
		return nil, err
	}
	rel, err := f.resolvePath(path)
	if err != nil {
		return nil, err
	}
	file, err := f.s.root.OpenFile(rel, flag, perm)
	if err != nil {
		return nil, wrapRootError(err)
	}
	return &sandboxFile{File: file, readOnly: f.s.cfg.ReadOnly || !isWrite}, nil
}

func (f *sandboxFS) Stat(ctx context.Context, path string) (fs.FileInfo, error) {
	if err := f.checkPreconditions(ctx, false); err != nil {
		return nil, err
	}
	rel, err := f.resolvePath(path)
	if err != nil {
		return nil, err
	}
	info, err := f.s.root.Stat(rel)
	if err != nil {
		return nil, wrapRootError(err)
	}
	return info, nil
}

func (f *sandboxFS) Lstat(ctx context.Context, path string) (fs.FileInfo, error) {
	if err := f.checkPreconditions(ctx, false); err != nil {
		return nil, err
	}
	rel, err := f.resolvePath(path)
	if err != nil {
		return nil, err
	}
	info, err := f.s.root.Lstat(rel)
	if err != nil {
		return nil, wrapRootError(err)
	}
	return info, nil
}

func (f *sandboxFS) ReadDir(ctx context.Context, path string) ([]fs.DirEntry, error) {
	if err := f.checkPreconditions(ctx, false); err != nil {
		return nil, err
	}
	rel, err := f.resolvePath(path)
	if err != nil {
		return nil, err
	}
	dir, err := f.s.root.Open(rel)
	if err != nil {
		return nil, wrapRootError(err)
	}
	defer dir.Close()

	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, wrapRootError(err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	return entries, nil
}

func (f *sandboxFS) MkdirAll(ctx context.Context, path string, perm fs.FileMode) error {
	if err := f.checkPreconditions(ctx, true); err != nil {
		return err
	}
	rel, err := f.resolvePath(path)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	return wrapRootError(f.s.root.MkdirAll(rel, perm))
}

func (f *sandboxFS) Remove(ctx context.Context, path string) error {
	if err := f.checkPreconditions(ctx, true); err != nil {
		return err
	}
	rel, err := f.resolvePath(path)
	if err != nil {
		return err
	}
	if rel == "." {
		return fmt.Errorf("%w: cannot remove sandbox root directory", ErrPathEscape)
	}
	return wrapRootError(f.s.root.Remove(rel))
}

func (f *sandboxFS) RemoveAll(ctx context.Context, path string) error {
	if err := f.checkPreconditions(ctx, true); err != nil {
		return err
	}
	rel, err := f.resolvePath(path)
	if err != nil {
		return err
	}
	if rel == "." {
		return fmt.Errorf("%w: cannot remove sandbox root directory", ErrPathEscape)
	}
	return wrapRootError(f.s.root.RemoveAll(rel))
}

func (f *sandboxFS) Rename(ctx context.Context, oldpath, newpath string) error {
	if err := f.checkPreconditions(ctx, true); err != nil {
		return err
	}
	oldRel, err := f.resolvePath(oldpath)
	if err != nil {
		return err
	}
	newRel, err := f.resolvePath(newpath)
	if err != nil {
		return err
	}
	return wrapRootError(f.s.root.Rename(oldRel, newRel))
}

func (f *sandboxFS) Chmod(ctx context.Context, path string, perm fs.FileMode) error {
	if err := f.checkPreconditions(ctx, true); err != nil {
		return err
	}
	rel, err := f.resolvePath(path)
	if err != nil {
		return err
	}
	return wrapRootError(f.s.root.Chmod(rel, perm))
}

// sandboxFile wraps *os.File to enforce read-only protections on the handle if opened read-only.
type sandboxFile struct {
	*os.File
	readOnly bool
}

func (sf *sandboxFile) Write(b []byte) (int, error) {
	if sf.readOnly {
		return 0, ErrReadOnly
	}
	return sf.File.Write(b)
}
