package sys

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

// ErrLocked is returned when a requested lock file is already held by another process.
var ErrLocked = errors.New("sys: lock file is already held")

// AcquireLock opens the file at lockPath (creating its parent directories if necessary)
// and acquires an exclusive, non-blocking lock. It returns [ErrLocked] if the lock
// is already held.
func AcquireLock(lockPath string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, fmt.Errorf("sys: create lock directory: %w", err)
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("sys: open lock file %q: %w", lockPath, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("%w: %v", ErrLocked, err)
	}

	return f, nil
}

// ReleaseLock unlocks and closes f. It is a no-op if f is nil.
func ReleaseLock(f *os.File) error {
	if f == nil {
		return nil
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return f.Close()
}

// ListenUnix creates a Unix domain socket listener at socketPath with 0600 permissions,
// removing any stale socket file first.
func ListenUnix(ctx context.Context, socketPath string) (net.Listener, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return nil, fmt.Errorf("sys: create socket directory: %w", err)
	}
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("sys: remove stale socket %q: %w", socketPath, err)
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("sys: listen unix %q: %w", socketPath, err)
	}
	_ = os.Chmod(socketPath, 0o600)
	return ln, nil
}
