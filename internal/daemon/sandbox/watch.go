package sandbox

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/guard"
)

type sandboxWatcher struct {
	s           *session
	fs          *sandboxFS
	fsw         *fsnotify.Watcher
	eventsCh    chan WatchEvent
	errorsCh    chan error
	closeOnce   sync.Once
	closed      atomic.Bool
	stopCh      chan struct{}
	opts        WatchOptions
	ignoreGlobs []*fsutils.Glob
	baseRel     string
	watchedMu   sync.Mutex
	watched     map[string]struct{} // set of host dir paths
}

var _ Watcher = (*sandboxWatcher)(nil)

func (w *sandboxWatcher) Events() <-chan WatchEvent {
	return w.eventsCh
}

func (w *sandboxWatcher) Errors() <-chan error {
	return w.errorsCh
}

func (w *sandboxWatcher) Close() error {
	var closeErr error
	w.closeOnce.Do(func() {
		w.closed.Store(true)
		close(w.stopCh)
		closeErr = w.fsw.Close()
		w.s.unregisterWatcher(w)
	})
	return closeErr
}

// Watch monitors the directory at path (relative to WorkDir) for filesystem changes.
func (f *sandboxFS) Watch(ctx context.Context, path string, opts WatchOptions) (Watcher, error) {
	if err := f.checkPreconditions(ctx, false); err != nil {
		return nil, err
	}

	rel, err := f.resolvePath(path)
	if err != nil {
		return nil, err
	}

	// Verify that target exists and is a real directory (never follow symlinks!).
	var info fs.FileInfo
	if rel == "." {
		baseDir := f.s.realWorkDir
		if baseDir == "" {
			baseDir = f.s.workDir
		}
		info, err = os.Lstat(baseDir)
	} else {
		info, err = f.s.root.Lstat(rel)
	}
	if err != nil {
		return nil, wrapRootError(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("%w: watch path %q is not a directory or is a symlink", ErrInvalidConfig, path)
	}

	maxDirs := opts.MaxDirs
	if maxDirs <= 0 {
		maxDirs = 1024
	}
	opts.MaxDirs = maxDirs

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("sandbox: failed to create watcher: %w", err)
	}

	w := &sandboxWatcher{
		s:           f.s,
		fs:          f,
		fsw:         fsw,
		eventsCh:    make(chan WatchEvent, 2048),
		errorsCh:    make(chan error, 256),
		stopCh:      make(chan struct{}),
		opts:        opts,
		ignoreGlobs: compileIgnoreGlobs(opts.Ignore),
		baseRel:     rel,
		watched:     make(map[string]struct{}),
	}

	if err := w.addWatchTree(rel, false); err != nil {
		_ = fsw.Close()
		return nil, err
	}

	if !f.s.registerWatcher(w) {
		_ = fsw.Close()
		return nil, ErrSandboxClosed
	}
	go w.loop()

	return w, nil
}

func (w *sandboxWatcher) hostPathForRel(rel string) string {
	baseDir := w.s.realWorkDir
	if baseDir == "" {
		baseDir = w.s.workDir
	}
	if rel == "." || rel == "" {
		return baseDir
	}
	return filepath.Join(baseDir, filepath.FromSlash(rel))
}

func (w *sandboxWatcher) addWatchTree(startRel string, emitSynthetic bool) error {
	queue := []string{startRel}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if (emitSynthetic || curr != startRel) && w.isIgnored(curr) {
			continue
		}

		// Verify directory is real and not a symlink.
		if curr != "." {
			info, err := w.fs.s.root.Lstat(curr)
			if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				continue
			}
		}

		w.watchedMu.Lock()
		if len(w.watched) >= w.opts.MaxDirs {
			w.watchedMu.Unlock()
			break
		}
		hostDir := w.hostPathForRel(curr)
		if _, exists := w.watched[hostDir]; !exists {
			if err := w.fsw.Add(hostDir); err != nil {
				w.watchedMu.Unlock()
				if !emitSynthetic {
					return fmt.Errorf("sandbox: watch add %q: %w", curr, err)
				}
				continue
			}
			w.watched[hostDir] = struct{}{}
		}
		w.watchedMu.Unlock()

		if !w.opts.Recursive {
			continue
		}

		dir, err := w.fs.s.root.Open(curr)
		if err != nil {
			continue
		}
		entries, _ := dir.ReadDir(-1)
		_ = dir.Close()

		for _, entry := range entries {
			childRel := entry.Name()
			if curr != "." {
				childRel = filepath.ToSlash(filepath.Join(curr, entry.Name()))
			}
			if w.isIgnored(childRel) {
				continue
			}
			if entry.IsDir() && entry.Type()&fs.ModeSymlink == 0 {
				queue = append(queue, childRel)
			}
			if emitSynthetic {
				select {
				case w.eventsCh <- WatchEvent{Path: childRel, Op: WatchCreate}:
				case <-w.stopCh:
					return nil
				}
			}
		}
	}

	return nil
}

func (w *sandboxWatcher) loop() {
	defer func() {
		_ = w.fsw.Close()
		close(w.eventsCh)
		close(w.errorsCh)
		w.s.unregisterWatcher(w)
	}()

	for {
		select {
		case <-w.stopCh:
			return
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			select {
			case w.errorsCh <- err:
			case <-w.stopCh:
				return
			}
		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		}
	}
}

func (w *sandboxWatcher) handleEvent(event fsnotify.Event) {
	var op WatchOp
	if event.Has(fsnotify.Create) {
		op |= WatchCreate
	}
	if event.Has(fsnotify.Write) {
		op |= WatchWrite
	}
	if event.Has(fsnotify.Remove) {
		op |= WatchRemove
	}
	if event.Has(fsnotify.Rename) {
		op |= WatchRename
	}
	if event.Has(fsnotify.Chmod) {
		op |= WatchChmod
	}
	if op == 0 {
		return
	}

	relPath, err := w.fs.ResolvePath(event.Name)
	if err != nil || relPath == "" || w.isIgnored(relPath) {
		return
	}

	// Dynamically register new directories when created recursively.
	if w.opts.Recursive && op&WatchCreate != 0 {
		info, err := w.fs.s.root.Lstat(relPath)
		if err == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir() {
			_ = w.addWatchTree(relPath, true)
		}
	}

	select {
	case w.eventsCh <- WatchEvent{Path: relPath, Op: op}:
	case <-w.stopCh:
	}
}

func compileIgnoreGlobs(ignore []string) []*fsutils.Glob {
	var globs []*fsutils.Glob
	for _, ign := range ignore {
		ignClean := strings.TrimSuffix(filepath.ToSlash(filepath.Clean(strings.TrimSpace(ign))), "/")
		if ignClean == "" || ignClean == "." {
			continue
		}
		pat := ignClean
		if !strings.Contains(ignClean, "/") {
			pat = "**/" + ignClean
		}
		if g, err := fsutils.Compile(pat); err == nil {
			globs = append(globs, g)
		}
		if !strings.HasSuffix(pat, "/**") {
			if g, err := fsutils.Compile(pat + "/**"); err == nil {
				globs = append(globs, g)
			}
		}
	}
	return globs
}

func (w *sandboxWatcher) isIgnored(relPath string) bool {
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if clean == "." || clean == "" {
		return false
	}
	if guard.IsProtectedPath(clean, "", ".git", ".bare") {
		return true
	}
	for _, g := range w.ignoreGlobs {
		if g.Match(clean) {
			return true
		}
	}
	return false
}
