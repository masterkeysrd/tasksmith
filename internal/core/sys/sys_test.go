package sys

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireAndReleaseLock(t *testing.T) {
	tmp := t.TempDir()
	lockPath := filepath.Join(tmp, "sub", "test.lock")

	f1, err := AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireLock 1: %v", err)
	}
	defer func() {
		_ = ReleaseLock(f1)
	}()

	f2, err := AcquireLock(lockPath)
	if !errors.Is(err, ErrLocked) {
		if f2 != nil {
			_ = ReleaseLock(f2)
		}
		t.Fatalf("AcquireLock 2 = %v, want ErrLocked", err)
	}

	if err := ReleaseLock(f1); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}

	f3, err := AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireLock 3 after release: %v", err)
	}
	_ = ReleaseLock(f3)
}

func TestListenUnix(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	sockPath := filepath.Join(tmp, "sub", "test.sock")

	ln, err := ListenUnix(ctx, sockPath)
	if err != nil {
		t.Fatalf("ListenUnix: %v", err)
	}
	defer func() {
		_ = ln.Close()
	}()

	fi, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("Stat socket: %v", err)
	}
	if fi.Mode().Type()&os.ModeSocket == 0 {
		t.Fatalf("expected socket file, got %v", fi.Mode())
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("socket permissions = %v, want 0600", fi.Mode().Perm())
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
	if err != nil {
		t.Fatalf("Dial unix socket: %v", err)
	}
	_ = conn.Close()

	_ = ln.Close()

	// Should remove stale socket and re-listen cleanly
	ln2, err := ListenUnix(ctx, sockPath)
	if err != nil {
		t.Fatalf("ListenUnix second time: %v", err)
	}
	_ = ln2.Close()
}

func TestAcquireLockRecordsHolderPID(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "pid.lock")

	f, err := AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	defer func() {
		_ = ReleaseLock(f)
	}()

	pid, ok := LockHolderPID(lockPath)
	if !ok {
		t.Fatalf("LockHolderPID(%q) not found", lockPath)
	}
	if pid != os.Getpid() {
		t.Errorf("LockHolderPID = %d, want %d", pid, os.Getpid())
	}
}

func TestLockHolderPIDInvalid(t *testing.T) {
	tmp := t.TempDir()

	if _, ok := LockHolderPID(filepath.Join(tmp, "missing.lock")); ok {
		t.Error("LockHolderPID on missing file = true, want false")
	}

	garbage := filepath.Join(tmp, "garbage.lock")
	if err := os.WriteFile(garbage, []byte("not-a-pid\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, ok := LockHolderPID(garbage); ok {
		t.Error("LockHolderPID on garbage content = true, want false")
	}
}

func TestAcquireLockWithRetryAcquiresAfterRelease(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "retry.lock")

	held, err := AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}

	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = ReleaseLock(held)
	}()

	f, err := AcquireLockWithRetry(context.Background(), lockPath, 3*time.Second, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("AcquireLockWithRetry: %v", err)
	}
	_ = ReleaseLock(f)
}

func TestAcquireLockWithRetryTimesOut(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "retry-timeout.lock")

	held, err := AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	defer func() {
		_ = ReleaseLock(held)
	}()

	start := time.Now()
	f, err := AcquireLockWithRetry(context.Background(), lockPath, 200*time.Millisecond, 20*time.Millisecond)
	if f != nil {
		_ = ReleaseLock(f)
	}
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("AcquireLockWithRetry = %v, want ErrLocked", err)
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Errorf("AcquireLockWithRetry returned after %v, want >= 200ms", elapsed)
	}
}

func TestWaitForLockRelease(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "wait.lock")

	held, err := AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}

	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = ReleaseLock(held)
	}()

	if err := WaitForLockRelease(context.Background(), lockPath, 3*time.Second, 20*time.Millisecond); err != nil {
		t.Fatalf("WaitForLockRelease: %v", err)
	}
}

func TestWaitForLockReleaseTimesOutWhileHeld(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "wait-timeout.lock")

	held, err := AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	defer func() {
		_ = ReleaseLock(held)
	}()

	if err := WaitForLockRelease(context.Background(), lockPath, 200*time.Millisecond, 20*time.Millisecond); !errors.Is(err, ErrLocked) {
		t.Fatalf("WaitForLockRelease = %v, want ErrLocked", err)
	}
}

func TestWaitForLockReleaseDoesNotClobberHolderPID(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "nopid.lock")

	held, err := AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	defer func() {
		_ = ReleaseLock(held)
	}()

	// Stand in for a different holder's recorded pid; a waiting probe must not
	// overwrite it.
	if err := os.WriteFile(lockPath, []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := WaitForLockRelease(context.Background(), lockPath, 150*time.Millisecond, 20*time.Millisecond); !errors.Is(err, ErrLocked) {
		t.Fatalf("WaitForLockRelease = %v, want ErrLocked", err)
	}

	pid, ok := LockHolderPID(lockPath)
	if !ok || pid != 999999 {
		t.Errorf("LockHolderPID after waiting = (%d, %v), want (999999, true)", pid, ok)
	}
}
