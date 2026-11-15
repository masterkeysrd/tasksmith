package sys

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
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
