package sandbox

import (
	"context"
	"errors"
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

// ResolvePath validates a caller-supplied path (relative or absolute within
// WorkDir) and returns a clean, slash-separated path relative to the sandbox
// root, or ErrPathEscape if the path escapes WorkDir.
func (f *sandboxFS) ResolvePath(path string) (string, error) {
	rel, err := f.resolvePath(path)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
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

type targetPathInfo struct {
	rel   string
	abs   string
	isRel bool
}

func (f *sandboxFS) resolveTarget(path string) (targetPathInfo, error) {
	rel, err := f.resolvePath(path)
	if err == nil {
		return targetPathInfo{rel: rel, isRel: true}, nil
	}
	if !errors.Is(err, ErrPathEscape) {
		return targetPathInfo{}, err
	}
	trimmed := strings.TrimSpace(path)
	if filepath.IsAbs(trimmed) {
		return targetPathInfo{abs: filepath.Clean(trimmed), isRel: false}, nil
	}
	return targetPathInfo{abs: filepath.Clean(filepath.Join(f.s.workDir, trimmed)), isRel: false}, nil
}

// Open implements io/fs.FS for read-only standard library compatibility.
func (f *sandboxFS) Open(name string) (fs.File, error) {
	if err := f.checkPreconditions(context.Background(), false); err != nil {
		return nil, err
	}
	target, err := f.resolveTarget(name)
	if err != nil {
		return nil, err
	}
	if target.isRel {
		file, err := f.s.root.Open(target.rel)
		if err != nil {
			return nil, wrapRootError(err)
		}
		return &sandboxFile{File: file, readOnly: true}, nil
	}
	file, err := os.Open(target.abs)
	if err != nil {
		return nil, err
	}
	return &sandboxFile{File: file, readOnly: true}, nil
}

func (f *sandboxFS) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if err := f.checkPreconditions(ctx, false); err != nil {
		return nil, err
	}
	target, err := f.resolveTarget(path)
	if err != nil {
		return nil, err
	}
	if target.isRel {
		data, err := f.s.root.ReadFile(target.rel)
		if err != nil {
			return nil, wrapRootError(err)
		}
		return data, nil
	}
	return os.ReadFile(target.abs)
}

func (f *sandboxFS) WriteFile(ctx context.Context, path string, data []byte, perm fs.FileMode) error {
	if err := f.checkPreconditions(ctx, true); err != nil {
		return err
	}
	target, err := f.resolveTarget(path)
	if err != nil {
		return err
	}
	if target.isRel {
		return wrapRootError(f.s.root.WriteFile(target.rel, data, perm))
	}
	return os.WriteFile(target.abs, data, perm)
}

func (f *sandboxFS) OpenFile(ctx context.Context, path string, flag int, perm fs.FileMode) (File, error) {
	isWrite := flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0
	if err := f.checkPreconditions(ctx, isWrite); err != nil {
		return nil, err
	}
	target, err := f.resolveTarget(path)
	if err != nil {
		return nil, err
	}
	if target.isRel {
		file, err := f.s.root.OpenFile(target.rel, flag, perm)
		if err != nil {
			return nil, wrapRootError(err)
		}
		return &sandboxFile{File: file, readOnly: f.s.cfg.ReadOnly || !isWrite}, nil
	}
	file, err := os.OpenFile(target.abs, flag, perm)
	if err != nil {
		return nil, err
	}
	return &sandboxFile{File: file, readOnly: f.s.cfg.ReadOnly || !isWrite}, nil
}

func (f *sandboxFS) Stat(ctx context.Context, path string) (fs.FileInfo, error) {
	if err := f.checkPreconditions(ctx, false); err != nil {
		return nil, err
	}
	target, err := f.resolveTarget(path)
	if err != nil {
		return nil, err
	}
	if target.isRel {
		info, err := f.s.root.Stat(target.rel)
		if err != nil {
			return nil, wrapRootError(err)
		}
		return info, nil
	}
	return os.Stat(target.abs)
}

func (f *sandboxFS) Lstat(ctx context.Context, path string) (fs.FileInfo, error) {
	if err := f.checkPreconditions(ctx, false); err != nil {
		return nil, err
	}
	target, err := f.resolveTarget(path)
	if err != nil {
		return nil, err
	}
	if target.isRel {
		info, err := f.s.root.Lstat(target.rel)
		if err != nil {
			return nil, wrapRootError(err)
		}
		return info, nil
	}
	return os.Lstat(target.abs)
}

func (f *sandboxFS) ReadDir(ctx context.Context, path string) ([]fs.DirEntry, error) {
	if err := f.checkPreconditions(ctx, false); err != nil {
		return nil, err
	}
	target, err := f.resolveTarget(path)
	if err != nil {
		return nil, err
	}
	var entries []fs.DirEntry
	if target.isRel {
		dir, err := f.s.root.Open(target.rel)
		if err != nil {
			return nil, wrapRootError(err)
		}
		defer dir.Close()

		entries, err = dir.ReadDir(-1)
		if err != nil {
			return nil, wrapRootError(err)
		}
	} else {
		var err error
		entries, err = os.ReadDir(target.abs)
		if err != nil {
			return nil, err
		}
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
	target, err := f.resolveTarget(path)
	if err != nil {
		return err
	}
	if target.isRel {
		if target.rel == "." {
			return nil
		}
		return wrapRootError(f.s.root.MkdirAll(target.rel, perm))
	}
	if target.abs == "/" || target.abs == f.s.workDir || (f.s.realWorkDir != "" && target.abs == f.s.realWorkDir) {
		return nil
	}
	return os.MkdirAll(target.abs, perm)
}

func (f *sandboxFS) Remove(ctx context.Context, path string) error {
	if err := f.checkPreconditions(ctx, true); err != nil {
		return err
	}
	target, err := f.resolveTarget(path)
	if err != nil {
		return err
	}
	if target.isRel {
		if target.rel == "." {
			return fmt.Errorf("%w: cannot remove sandbox root directory", ErrPathEscape)
		}
		return wrapRootError(f.s.root.Remove(target.rel))
	}
	if target.abs == "/" || target.abs == f.s.workDir || (f.s.realWorkDir != "" && target.abs == f.s.realWorkDir) {
		return fmt.Errorf("%w: cannot remove root directory", ErrPathEscape)
	}
	return os.Remove(target.abs)
}

func (f *sandboxFS) RemoveAll(ctx context.Context, path string) error {
	if err := f.checkPreconditions(ctx, true); err != nil {
		return err
	}
	target, err := f.resolveTarget(path)
	if err != nil {
		return err
	}
	if target.isRel {
		if target.rel == "." {
			return fmt.Errorf("%w: cannot remove sandbox root directory", ErrPathEscape)
		}
		return wrapRootError(f.s.root.RemoveAll(target.rel))
	}
	if target.abs == "/" || target.abs == f.s.workDir || (f.s.realWorkDir != "" && target.abs == f.s.realWorkDir) {
		return fmt.Errorf("%w: cannot remove root directory", ErrPathEscape)
	}
	return os.RemoveAll(target.abs)
}

func (f *sandboxFS) Rename(ctx context.Context, oldpath, newpath string) error {
	if err := f.checkPreconditions(ctx, true); err != nil {
		return err
	}
	targetOld, err := f.resolveTarget(oldpath)
	if err != nil {
		return err
	}
	targetNew, err := f.resolveTarget(newpath)
	if err != nil {
		return err
	}
	if targetOld.isRel && targetNew.isRel {
		return wrapRootError(f.s.root.Rename(targetOld.rel, targetNew.rel))
	}
	oldAbs := targetOld.abs
	if targetOld.isRel {
		oldAbs = filepath.Join(f.s.workDir, targetOld.rel)
	}
	newAbs := targetNew.abs
	if targetNew.isRel {
		newAbs = filepath.Join(f.s.workDir, targetNew.rel)
	}
	return os.Rename(oldAbs, newAbs)
}

func (f *sandboxFS) Chmod(ctx context.Context, path string, perm fs.FileMode) error {
	if err := f.checkPreconditions(ctx, true); err != nil {
		return err
	}
	target, err := f.resolveTarget(path)
	if err != nil {
		return err
	}
	if target.isRel {
		return wrapRootError(f.s.root.Chmod(target.rel, perm))
	}
	return os.Chmod(target.abs, perm)
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
