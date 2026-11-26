package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/core/sys"
	"github.com/masterkeysrd/tasksmith/internal/core/testutils"
)

func TestWaitForDaemonLockReleaseSkipsMissingLock(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "none.sock")
	start := time.Now()
	if err := waitForDaemonLockRelease(context.Background(), missing); err != nil {
		t.Fatalf("waitForDaemonLockRelease (missing lock): %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("waitForDaemonLockRelease took %v for a missing lock, want immediate return", elapsed)
	}
}

func TestWaitForDaemonLockReleaseWaitsForHolder(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "d.sock")
	lockPath := client.LockPathForSocket(sock)

	held, err := sys.AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = sys.ReleaseLock(held)
	}()

	if err := waitForDaemonLockRelease(context.Background(), sock); err != nil {
		t.Fatalf("waitForDaemonLockRelease: %v", err)
	}
}

func TestWaitForDaemonLockReleaseTimesOut(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "d.sock")
	lockPath := client.LockPathForSocket(sock)

	held, err := sys.AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	defer func() {
		_ = sys.ReleaseLock(held)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err = waitForDaemonLockRelease(ctx, sock)
	if err == nil || !strings.Contains(err.Error(), "release") {
		t.Fatalf("waitForDaemonLockRelease error = %v, want lock release timeout", err)
	}
}

func TestWaitForDaemonReadyReportsEarlyChildExit(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "dead.sock")

	c, err := client.New(sock)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	exited := make(chan struct{})
	close(exited)
	spawned := &client.SpawnedDaemon{PID: 4242, Done: exited}

	start := time.Now()
	err = waitForDaemonReady(context.Background(), c, sock, spawned)
	if err == nil || !strings.Contains(err.Error(), "exited before becoming healthy") {
		t.Fatalf("waitForDaemonReady error = %v, want early-exit message", err)
	}
	if !strings.Contains(err.Error(), "pid=4242") {
		t.Errorf("waitForDaemonReady error = %q, want it to name the child pid", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("waitForDaemonReady took %v after child exit, want immediate failure", elapsed)
	}
}

func TestWaitForDaemonReadyIsBounded(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "silent.sock")

	c, err := client.New(sock)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	prev := client.SpawnWaitTimeout
	client.SpawnWaitTimeout = 400 * time.Millisecond
	t.Cleanup(func() {
		client.SpawnWaitTimeout = prev
	})

	start := time.Now()
	err = waitForDaemonReady(context.Background(), c, sock, nil)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitForDaemonReady error = %v, want bounded deadline error", err)
	}
	if !strings.Contains(err.Error(), sock) {
		t.Errorf("waitForDaemonReady error = %q, want it to name the socket", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("waitForDaemonReady waited %v, want the bounded spawn timeout", elapsed)
	}
}

func TestSocketExplicit(t *testing.T) {
	if (&rootOptions{}).socketExplicit() {
		t.Error("socketExplicit() = true for default options, want false")
	}
	if !(&rootOptions{socket: "/tmp/custom.sock"}).socketExplicit() {
		t.Error("socketExplicit() = false with --socket set, want true")
	}
}

func TestConnectOrStartFailsWhenSpawnedDaemonDies(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "dying.sock")

	// The spawned child's stderr goes to the XDG logs directory; keep it inside
	// the test so nothing is appended to the developer's real daemon-spawn.log.
	testutils.SetupXDG(t, "tasksmith-daemon-lifecycle-test")

	prev := client.SpawnWaitTimeout
	client.SpawnWaitTimeout = 5 * time.Second
	t.Cleanup(func() {
		client.SpawnWaitTimeout = prev
	})

	// Running this test binary with a filter that matches no tests exits
	// immediately without ever serving the socket; the wait must notice the
	// child exit and fail fast instead of hanging until the spawn timeout.
	_, _, err := ConnectOrStart(context.Background(), sock, "-test.run=TestZZZNoSuchSpawnTarget")
	if err == nil || !strings.Contains(err.Error(), "exited before becoming healthy") {
		t.Fatalf("ConnectOrStart error = %v, want early-exit failure", err)
	}
}
