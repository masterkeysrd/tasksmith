package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/sys"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/version"
)

// Daemon process lifecycle helpers. They live beside the client so Go
// consumers (CI scripts, custom harnesses, external SDK shells) drive the
// whole connect-or-spawn flow through this one public package, matching the
// behavior of the `tasksmith` CLI itself.

// DefaultSocketPath returns the default XDG runtime Unix domain socket path
// ("brain.sock") the daemon serves.
func DefaultSocketPath() (string, error) {
	return xdg.SocketPath("brain.sock")
}

// DefaultLockPath returns the default XDG runtime lockfile path ("brain.lock")
// paired with [DefaultSocketPath].
func DefaultLockPath() (string, error) {
	return xdg.SocketPath("brain.lock")
}

// LockPathForSocket returns the lockfile path paired with a socket path: the
// same directory and base name with a ".lock" extension. A socket that does
// not end in ".sock" gets ".lock" appended.
func LockPathForSocket(socketPath string) string {
	base := filepath.Clean(strings.TrimSpace(socketPath))
	if before, ok := strings.CutSuffix(base, ".sock"); ok {
		return before + ".lock"
	}
	return base + ".lock"
}

// SpawnedDaemon describes a detached daemon child process spawned by
// [SpawnDetached]. The process is reaped asynchronously; Done closes when the
// child exits for any reason, letting callers waiting on the socket notice an
// early startup failure immediately instead of polling until their deadline.
type SpawnedDaemon struct {
	PID  int
	Done <-chan struct{}
}

// SpawnDetached resolves the current executable and spawns a background detached daemon process.
// If spawnArgs is empty, default arguments ["daemon", "serve", "--socket", <socketPath>] are used.
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

// SpawnWaitTimeout bounds how long [ConnectOrStart] waits for a freshly
// spawned daemon to become healthy. It is a var so tests can shorten the bound.
var SpawnWaitTimeout = 15 * time.Second

// socketHealthInterval paces health probes while waiting for a spawned daemon.
const socketHealthInterval = 40 * time.Millisecond

// WaitForSocketHealthy polls a health probe against a freshly spawned daemon
// until it answers, the spawned child exits (surfacing the failure immediately
// with a pointer to the daemon logs), or timeout elapses. health is injected so
// callers own their client plumbing without this package depending on the HTTP
// client; it returns the daemon's health payload when the probe succeeds
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

// healthProbeTimeout bounds a single liveness ping during connect-or-start.
const healthProbeTimeout = 300 * time.Millisecond

// ConnectOrStart connects to an existing Brain Daemon at socketPath, or spawns
// a detached background daemon process (`os.Executable() daemon serve --socket <socketPath>`)
// and polls `/v1/health` until ready. An empty socketPath selects
// [DefaultSocketPath].
//
// When the running daemon was built from a different version than this client,
// ConnectOrStart restarts it only once every session is confirmed idle. In any
// other case — an active session or an unknown session state — the existing
// daemon is kept and the skew is returned as a user-facing notice so callers
// can surface it in their own UI; the user then restarts the daemon when it is
// safe instead of having work interrupted.
func ConnectOrStart(ctx context.Context, socketPath string, spawnArgs ...string) (*Client, []string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		defaultSock, err := DefaultSocketPath()
		if err != nil {
			return nil, nil, fmt.Errorf("resolve default socket path: %w", err)
		}
		socketPath = defaultSock
	}

	c, err := New(socketPath)
	if err != nil {
		return nil, nil, err
	}

	var notices []string
	h, healthy := healthDaemon(ctx, c)
	if healthy {
		if !DaemonVersionSkewed(h) {
			return c, notices, nil
		}
		sessions, err := c.ListSessions(ctx)
		if err != nil || HasActiveSessions(sessions) {
			notices = append(notices, fmt.Sprintf(
				"Daemon version %s differs from TaskSmith %s. Restart the daemon when it is safe to update it: run `tasksmith daemon stop` or press r in /daemon.",
				daemonVersionLabel(h), version.Get()))
			return c, notices, nil
		}
		notices = append(notices, fmt.Sprintf(
			"Daemon version %s differs from TaskSmith %s; restarting the daemon to match.",
			daemonVersionLabel(h), version.Get()))
		if err := restartDaemon(ctx, c); err != nil {
			return nil, notices, err
		}
	}

	spawned, err := SpawnDetached(socketPath, spawnArgs...)
	if err != nil {
		return nil, notices, err
	}

	if _, err := WaitForSocketHealthy(ctx, socketPath, spawned, SpawnWaitTimeout, c.Health); err != nil {
		return nil, notices, err
	}
	return c, notices, nil
}

// DaemonVersionSkewed reports whether a healthy daemon's build version differs
// from this client's. Daemons that predate version reporting are treated as
// skewed so they get upgraded on the next client launch.
func DaemonVersionSkewed(h *api.HealthResponse) bool {
	if h == nil {
		return false
	}
	return strings.TrimSpace(h.Version) != version.Get()
}

// HasActiveSessions reports whether any session on the daemon is doing work
// that a restart would interrupt.
func HasActiveSessions(sessions []api.Session) bool {
	for _, s := range sessions {
		if s.Status != api.StatusIdle {
			return true
		}
	}
	return false
}

// daemonVersionLabel renders a health response version, tolerating daemons
// that predate version reporting.
func daemonVersionLabel(h *api.HealthResponse) string {
	if h == nil || strings.TrimSpace(h.Version) == "" {
		return "unknown"
	}
	return h.Version
}

func healthDaemon(ctx context.Context, c *Client) (*api.HealthResponse, bool) {
	pingCtx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
	defer cancel()
	h, err := c.Health(pingCtx)
	if err != nil {
		return nil, false
	}
	return h, true
}

// restartDaemon asks a healthy daemon to shut down gracefully and waits for it
// to stop answering health checks *and* release brain.lock so the caller can
// spawn a replacement that acquires the lock on its first attempt.
func restartDaemon(ctx context.Context, c *Client) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("stop outdated daemon: %w", err)
	}
	waitCtx, waitCancel := context.WithTimeout(ctx, 10*time.Second)
	defer waitCancel()
	ticker := time.NewTicker(socketHealthInterval)
	defer ticker.Stop()
	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timed out waiting for outdated daemon to exit: %w", waitCtx.Err())
		case <-ticker.C:
			if !IsDaemonHealthy(waitCtx, c) {
				return WaitForSocketLockRelease(ctx, c.SocketPath())
			}
		}
	}
}

// IsDaemonHealthy reports whether the daemon answers a bounded health ping.
func IsDaemonHealthy(ctx context.Context, c *Client) bool {
	pingCtx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
	defer cancel()
	_, err := c.Health(pingCtx)
	return err == nil
}
