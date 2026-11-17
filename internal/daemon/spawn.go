package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/sys"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

// SpawnedDaemon describes a detached daemon child process spawned by
// [SpawnDetached]. The process is reaped asynchronously; Done closes when the
// child exits for any reason, letting callers waiting on the socket notice an
// early startup failure immediately instead of polling until their deadline.
type SpawnedDaemon struct {
	PID  int
	Done <-chan struct{}
}

// SpawnDetached resolves the current executable and spawns a background detached daemon process.
// If spawnArgs is empty, default arguments ["daemon", "serve", "--socket", socketPath] are used.
// The child process is detached with Setsid and reaped asynchronously in a goroutine.
func SpawnDetached(socketPath string, spawnArgs ...string) (*SpawnedDaemon, error) {
	return SpawnDetachedWithLog(socketPath, "", spawnArgs...)
}

// SpawnDetachedWithLog behaves like [SpawnDetached] and additionally redirects the
// child's stderr to stderrPath when provided, so startup failures of a detached
// daemon are recoverable instead of silently discarded. If stderrPath is empty, a
// best-effort path inside the XDG logs directory is used; stderr is discarded when
// the log directory cannot be resolved.
func SpawnDetachedWithLog(socketPath, stderrPath string, spawnArgs ...string) (*SpawnedDaemon, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return nil, fmt.Errorf("create socket directory: %w", err)
	}

	args := spawnArgs
	if len(args) == 0 {
		args = []string{"daemon", "serve", "--socket", socketPath}
	}

	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	if f := openSpawnLog(stderrPath); f != nil {
		cmd.Stderr = f
		defer func() {
			_ = f.Close()
		}()
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawn background daemon: %w", err)
	}

	done := make(chan struct{})
	pid := cmd.Process.Pid
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	return &SpawnedDaemon{PID: pid, Done: done}, nil
}

// spawnLogMaxBytes bounds daemon-spawn.log; when a new spawn finds the file
// larger, the previous contents move to a single ".1" backup so a repeatedly
// failing daemon cannot grow the log without limit.
const spawnLogMaxBytes = 256 * 1024

// openSpawnLog opens (creating parents) the file a detached daemon's stderr is
// written to, rotating an oversized previous log to "<name>.1" first. It falls
// back to "<logs>/daemon-spawn.log" and returns nil when no writable location
// can be resolved.
func openSpawnLog(stderrPath string) *os.File {
	path := stderrPath
	if path == "" {
		logsDir, err := xdg.LogsDir()
		if err != nil {
			return nil
		}
		path = filepath.Join(logsDir, "daemon-spawn.log")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil
	}
	rotateSpawnLog(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil
	}
	return f
}

// rotateSpawnLog moves an oversized spawn log to a single ".1" backup.
func rotateSpawnLog(path string) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() < spawnLogMaxBytes {
		return
	}
	_ = os.Rename(path, path+".1")
}

// SpawnLogHint returns the daemon log directory for user-facing error messages,
// falling back to the documented default when it cannot be resolved.
func SpawnLogHint() string {
	logsDir, err := xdg.LogsDir()
	if err != nil {
		return "~/.local/state/tasksmith/logs"
	}
	return logsDir
}

// SpawnWaitTimeout bounds how long ConnectOrStart waits for a freshly spawned
// daemon to become healthy. It is a var so tests can shorten the bound.
var SpawnWaitTimeout = 15 * time.Second

// socketHealthInterval paces health probes while waiting for a spawned daemon.
const socketHealthInterval = 40 * time.Millisecond

// WaitForSocketHealthy polls a health probe against a freshly spawned daemon
// until it answers, the spawned child exits (surfacing the failure immediately
// with a pointer to the daemon logs), or timeout elapses. health is injected so
// callers own their client plumbing without the daemon package depending on the
// HTTP client; it returns the daemon's health payload when the probe succeeds
// (may be nil for callers that only track liveness).
func WaitForSocketHealthy(ctx context.Context, socketPath string, spawned *SpawnedDaemon, timeout time.Duration, health func(context.Context) (*api.HealthResponse, error)) (*api.HealthResponse, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(socketHealthInterval)
	defer ticker.Stop()

	var exited <-chan struct{}
	if spawned != nil {
		exited = spawned.Done
	}

	for {
		select {
		case <-waitCtx.Done():
			return nil, fmt.Errorf("timed out waiting for daemon at %q: %w (see logs in %s)", socketPath, waitCtx.Err(), SpawnLogHint())
		case <-exited:
			return nil, fmt.Errorf("daemon process (pid=%d) exited before becoming healthy (see logs in %s)", spawned.PID, SpawnLogHint())
		case <-ticker.C:
			h, err := health(waitCtx)
			if err == nil {
				return h, nil
			}
		}
	}
}

// socketLockReleaseTimeout bounds how long callers wait for a stopping daemon to
// release its lock. Teardown (workspaces, MCP children, database) happens after
// the socket is already closed, so health failing alone does not mean a respawn
// can acquire the lock.
const socketLockReleaseTimeout = 10 * time.Second

// WaitForSocketLockRelease blocks until the lock paired with socketPath is free
// (the previous daemon finished teardown). It returns nil immediately when no
// lock file exists, and a timeout error when the holder never releases; the
// spawned daemon's own retry window may still recover such a handoff.
func WaitForSocketLockRelease(ctx context.Context, socketPath string) error {
	lockPath := LockPathForSocket(socketPath)
	if _, err := os.Stat(lockPath); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, socketLockReleaseTimeout)
	defer cancel()
	if err := sys.WaitForLockRelease(waitCtx, lockPath, socketLockReleaseTimeout, 50*time.Millisecond); err != nil {
		return fmt.Errorf("timed out waiting for previous daemon to release %q: %w", lockPath, err)
	}
	return nil
}
