package sys

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ErrLocked is returned when a requested lock file is already held by another process.
var ErrLocked = errors.New("sys: lock file is already held")

// AcquireLock opens the file at lockPath (creating its parent directories if necessary)
// and acquires an exclusive, non-blocking lock. It records the current process id in
// the lock file so a second process can report who holds it. It returns [ErrLocked]
// if the lock is already held.
func AcquireLock(lockPath string) (*os.File, error) {
	f, err := tryLock(lockPath)
	if err != nil {
		return nil, err
	}
	if err := writeLockPID(f); err != nil {
		_ = ReleaseLock(f)
		return nil, err
	}
	return f, nil
}

// tryLock opens lockPath and attempts a non-blocking exclusive flock without
// touching the file contents, so probing never clobbers the pid recorded by the
// current holder. On success the caller owns the lock and must release it.
func tryLock(lockPath string) (*os.File, error) {
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

// writeLockPID truncates the held lock file and records the current process id.
func writeLockPID(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("sys: truncate lock file %q: %w", f.Name(), err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("sys: seek lock file %q: %w", f.Name(), err)
	}
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		return fmt.Errorf("sys: write lock file %q: %w", f.Name(), err)
	}
	return nil
}

// retryWhileLocked runs try until it stops reporting [ErrLocked], timeout
// elapses, or ctx is canceled. A zero or negative timeout allows exactly one
// attempt. Every lock-retry loop in this package shares this helper.
func retryWhileLocked(ctx context.Context, timeout, interval time.Duration, try func() error) error {
	if interval <= 0 {
		interval = 50 * time.Millisecond
	}
	if err := try(); !errors.Is(err, ErrLocked) {
		return err
	}
	if timeout <= 0 {
		return ErrLocked
	}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		if err := try(); !errors.Is(err, ErrLocked) {
			return err
		}
		if !time.Now().Before(deadline) {
			return ErrLocked
		}
	}
}

// AcquireLockWithRetry behaves like [AcquireLock] but keeps retrying every interval
// until the lock is acquired, timeout elapses, or ctx is canceled. On timeout it
// returns [ErrLocked]. It gives a daemon that is still tearing down (and holding the
// lock past its socket shutdown) a short window to finish before a replacement gives up.
func AcquireLockWithRetry(ctx context.Context, lockPath string, timeout, interval time.Duration) (*os.File, error) {
	var acquired *os.File
	err := retryWhileLocked(ctx, timeout, interval, func() error {
		f, err := AcquireLock(lockPath)
		if err == nil {
			acquired = f
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return acquired, nil
}

// LockHolderPID reads the process id recorded in the lock file at lockPath. It
// returns false when the file is missing, unreadable, or does not hold a valid pid.
func LockHolderPID(lockPath string) (int, bool) {
	b, err := os.ReadFile(lockPath)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// WaitForLockRelease blocks until the lock at lockPath is free, timeout elapses,
// or ctx is canceled. Probing never writes to the lock file, so a waiter cannot
// clobber the pid recorded by the current holder. It returns [ErrLocked] when the
// timeout elapses with the lock still held, and passes other lock failures through
// unchanged.
func WaitForLockRelease(ctx context.Context, lockPath string, timeout, interval time.Duration) error {
	return retryWhileLocked(ctx, timeout, interval, func() error {
		f, err := tryLock(lockPath)
		if err != nil {
			return err
		}
		return ReleaseLock(f)
	})
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
