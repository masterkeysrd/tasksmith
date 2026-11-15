package sandbox_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
)

func waitForEvent(t *testing.T, w sandbox.Watcher, timeout time.Duration, predicate func(ev sandbox.WatchEvent) bool) sandbox.WatchEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-w.Events():
			if !ok {
				t.Fatalf("watcher events channel closed unexpectedly")
			}
			if predicate(ev) {
				return ev
			}
		case err, ok := <-w.Errors():
			if ok && err != nil {
				t.Fatalf("unexpected watcher error: %v", err)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for event matching predicate")
		}
	}
}

func TestWatcher_CreateWriteRemoveRename(t *testing.T) {
	dir := t.TempDir()
	sb, err := sandbox.New(sandbox.Config{WorkDir: dir})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	t.Cleanup(func() { _ = sb.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	w, err := sb.FS().Watch(ctx, ".", sandbox.WatchOptions{Recursive: true})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	// 1. Create file
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	waitForEvent(t, w, 3*time.Second, func(ev sandbox.WatchEvent) bool {
		return ev.Path == "test.txt" && ev.Op&sandbox.WatchCreate != 0
	})

	// 2. Modify file
	if err := os.WriteFile(filePath, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("WriteFile modify failed: %v", err)
	}

	waitForEvent(t, w, 3*time.Second, func(ev sandbox.WatchEvent) bool {
		return ev.Path == "test.txt" && ev.Op&sandbox.WatchWrite != 0
	})

	// 3. Rename file
	renamedPath := filepath.Join(dir, "renamed.txt")
	if err := os.Rename(filePath, renamedPath); err != nil {
		t.Fatalf("Rename failed: %v", err)
	}

	waitForEvent(t, w, 3*time.Second, func(ev sandbox.WatchEvent) bool {
		return (ev.Path == "test.txt" || ev.Path == "renamed.txt") && (ev.Op&(sandbox.WatchRename|sandbox.WatchCreate) != 0)
	})

	// 4. Remove file
	if err := os.Remove(renamedPath); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	waitForEvent(t, w, 3*time.Second, func(ev sandbox.WatchEvent) bool {
		return ev.Path == "renamed.txt" && ev.Op&sandbox.WatchRemove != 0
	})
}

func TestWatcher_RecursiveSubdirectory(t *testing.T) {
	dir := t.TempDir()
	sb, err := sandbox.New(sandbox.Config{WorkDir: dir})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	t.Cleanup(func() { _ = sb.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	w, err := sb.FS().Watch(ctx, ".", sandbox.WatchOptions{Recursive: true})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	// Dynamically create a new subdirectory at runtime
	subDir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}

	waitForEvent(t, w, 3*time.Second, func(ev sandbox.WatchEvent) bool {
		return ev.Path == "subdir" && ev.Op&sandbox.WatchCreate != 0
	})

	// Create a file inside that newly created subdirectory
	childFile := filepath.Join(subDir, "child.go")
	if err := os.WriteFile(childFile, []byte("package main"), 0o644); err != nil {
		t.Fatalf("WriteFile in child failed: %v", err)
	}

	waitForEvent(t, w, 3*time.Second, func(ev sandbox.WatchEvent) bool {
		return ev.Path == "subdir/child.go" && ev.Op&sandbox.WatchCreate != 0
	})
}

func TestWatcher_SymlinkEscapeRejection(t *testing.T) {
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	_ = os.WriteFile(outsideFile, []byte("secret"), 0o644)

	dir := t.TempDir()
	sb, err := sandbox.New(sandbox.Config{WorkDir: dir})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	t.Cleanup(func() { _ = sb.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt to symlink an outside directory inside the sandbox
	evilSymlink := filepath.Join(dir, "evil")
	if err := os.Symlink(outsideDir, evilSymlink); err != nil {
		t.Fatalf("Symlink creation failed: %v", err)
	}

	// Attempting to watch the symlinked directory directly must fail
	_, err = sb.FS().Watch(ctx, "evil", sandbox.WatchOptions{Recursive: true})
	if err == nil {
		t.Fatalf("expected Watch on symlinked directory to fail, but succeeded")
	}

	// Recursive watch on root should not watch inside the symlink
	w, err := sb.FS().Watch(ctx, ".", sandbox.WatchOptions{Recursive: true})
	if err != nil {
		t.Fatalf("Watch root failed: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	// Mutating the file in outsideDir must NOT produce an event in w
	_ = os.WriteFile(outsideFile, []byte("secret modified"), 0o644)

	// Small pause to confirm no event is emitted
	time.Sleep(150 * time.Millisecond)

	select {
	case ev := <-w.Events():
		if ev.Path == "evil/secret.txt" {
			t.Fatalf("unexpected event from symlink-escaped directory: %+v", ev)
		}
	default:
		// OK, no event
	}
}

func TestWatcher_IgnoreDirs(t *testing.T) {
	dir := t.TempDir()
	sb, err := sandbox.New(sandbox.Config{WorkDir: dir})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	t.Cleanup(func() { _ = sb.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create ignored dirs beforehand
	_ = os.Mkdir(filepath.Join(dir, ".git"), 0o755)
	_ = os.Mkdir(filepath.Join(dir, ".bare"), 0o755)
	_ = os.Mkdir(filepath.Join(dir, "custom_ignored"), 0o755)

	w, err := sb.FS().Watch(ctx, ".", sandbox.WatchOptions{
		Recursive: true,
		Ignore:    []string{"custom_ignored"},
	})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	// Write to .git, .bare, custom_ignored
	_ = os.WriteFile(filepath.Join(dir, ".git", "index"), []byte("data"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".bare", "head"), []byte("data"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "custom_ignored", "temp.txt"), []byte("data"), 0o644)

	// Also write to an allowed file to test the watcher is functioning
	allowedFile := filepath.Join(dir, "valid.txt")
	_ = os.WriteFile(allowedFile, []byte("valid"), 0o644)

	// We should receive the event for valid.txt
	waitForEvent(t, w, 3*time.Second, func(ev sandbox.WatchEvent) bool {
		return ev.Path == "valid.txt"
	})

	// Confirm no events for .git, .bare, or custom_ignored
	select {
	case ev := <-w.Events():
		if ev.Path == ".git/index" || ev.Path == ".bare/head" || ev.Path == "custom_ignored/temp.txt" {
			t.Fatalf("unexpected event for ignored path: %+v", ev)
		}
	default:
	}
}

func TestWatcher_MaxDirsCap(t *testing.T) {
	dir := t.TempDir()
	sb, err := sandbox.New(sandbox.Config{WorkDir: dir})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	t.Cleanup(func() { _ = sb.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create 5 directories
	for i := 1; i <= 5; i++ {
		_ = os.Mkdir(filepath.Join(dir, filepath.Join("d", string(rune('a'+i)))), 0o755)
	}

	// Watch with MaxDirs = 2 (root + 1 subdir)
	w, err := sb.FS().Watch(ctx, ".", sandbox.WatchOptions{
		Recursive: true,
		MaxDirs:   2,
	})
	if err != nil {
		t.Fatalf("Watch with MaxDirs failed: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
}

func TestWatcher_SandboxCloseCleanup(t *testing.T) {
	dir := t.TempDir()
	sb, err := sandbox.New(sandbox.Config{WorkDir: dir})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	w, err := sb.FS().Watch(ctx, ".", sandbox.WatchOptions{Recursive: true})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}

	// Close the whole sandbox
	if err := sb.Close(); err != nil {
		t.Fatalf("Sandbox.Close failed: %v", err)
	}

	// Watcher events channel must be closed
	select {
	case _, ok := <-w.Events():
		if ok {
			t.Fatalf("expected events channel to be closed after sandbox.Close()")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for events channel to close after sandbox.Close()")
	}

	// Calling Watch after Close must fail with ErrSandboxClosed
	if _, err := sb.FS().Watch(ctx, ".", sandbox.WatchOptions{Recursive: true}); err == nil {
		t.Fatalf("expected Watch after Close to fail")
	}
}

func TestWatcher_ConcurrentWatchAndClose(t *testing.T) {
	for range 10 {
		dir := t.TempDir()
		for i := range 8 {
			_ = os.MkdirAll(filepath.Join(dir, "sub", string(rune('a'+i))), 0o755)
		}
		sb, err := sandbox.New(sandbox.Config{WorkDir: dir})
		if err != nil {
			t.Fatalf("failed to create sandbox: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		done := make(chan struct{})
		go func() {
			defer close(done)
			w, wErr := sb.FS().Watch(ctx, ".", sandbox.WatchOptions{Recursive: true})
			if wErr == nil {
				_ = w.Close()
			}
		}()

		_ = sb.Close()
		<-done
		cancel()
	}
}
