package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/version"
	"github.com/spf13/cobra"
)

func newDaemonCmd(rootOpts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the background TaskSmith Brain Daemon",
	}

	cmd.AddCommand(
		newDaemonServeCmd(rootOpts),
		newDaemonStartCmd(rootOpts),
		newDaemonStopCmd(rootOpts),
		newDaemonStatusCmd(rootOpts),
	)

	return cmd
}

func newDaemonServeCmd(rootOpts *rootOptions) *cobra.Command {
	var lockPath string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the Brain Daemon server in the foreground",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemonServe(cmd, rootOpts, lockPath)
		},
	}

	cmd.Flags().StringVar(&lockPath, "lock", "", "Daemon lockfile path override")
	return cmd
}

func newDaemonStartCmd(rootOpts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Ensure the background Brain Daemon is running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemonStart(cmd, rootOpts)
		},
	}
}

func newDaemonStopCmd(rootOpts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Gracefully stop the running Brain Daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemonStop(cmd, rootOpts)
		},
	}
}

func newDaemonStatusCmd(rootOpts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check the health and open workspaces of the Brain Daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemonStatus(cmd, rootOpts)
		},
	}
}

func runDaemonServe(cmd *cobra.Command, rootOpts *rootOptions, lockOverride string) error {
	logsDir, err := xdg.LogsDir()
	if err != nil {
		return fmt.Errorf("resolve logs directory: %w", err)
	}
	logger, rw, err := log.NewRotating(log.RotatingWriterConfig{
		Dir:    logsDir,
		Stream: "daemon",
	}, log.LevelInfo)
	if err != nil {
		return fmt.Errorf("initialize rotating logger: %w", err)
	}
	defer func() {
		_ = rw.Close()
	}()
	log.SetDefault(logger)

	sockPath, err := rootOpts.resolveSocket()
	if err != nil {
		return err
	}

	lockPath := strings.TrimSpace(lockOverride)
	if lockPath == "" {
		defaultLock, err := daemon.DefaultLockPath()
		if err != nil {
			return fmt.Errorf("resolve lock path: %w", err)
		}
		lockPath = defaultLock
	}

	srv, err := daemon.New(daemon.Config{
		SocketPath:  sockPath,
		LockPath:    lockPath,
		GatewayPool: gateway.NewPool(),
		Logger:      logger,
	})
	if err != nil {
		return fmt.Errorf("initialize daemon server: %w", err)
	}

	return srv.Serve(cmd.Context())
}

func runDaemonStart(cmd *cobra.Command, rootOpts *rootOptions) error {
	sockPath, err := rootOpts.resolveSocket()
	if err != nil {
		return err
	}
	c, notices, err := connectOrStart(cmd.Context(), sockPath)
	if err != nil {
		return err
	}
	for _, notice := range notices {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", notice)
	}
	h, err := c.Health(cmd.Context())
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Daemon running (version=%s pid=%d socket=%s)\n",
		daemonVersionLabel(h), h.PID, c.SocketPath())
	return nil
}

func runDaemonStop(cmd *cobra.Command, rootOpts *rootOptions) error {
	sockPath, err := rootOpts.resolveSocket()
	if err != nil {
		return err
	}
	c, err := client.New(sockPath)
	if err != nil {
		return err
	}
	if err := c.Shutdown(cmd.Context()); err != nil {
		return fmt.Errorf("stop daemon: %w", err)
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Daemon stopped.")
	return nil
}

func runDaemonStatus(cmd *cobra.Command, rootOpts *rootOptions) error {
	sockPath, err := rootOpts.resolveSocket()
	if err != nil {
		return err
	}
	c, err := client.New(sockPath)
	if err != nil {
		return err
	}
	h, err := c.Health(cmd.Context())
	if err != nil {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Daemon is not running (socket=%s)\n", sockPath)
		return nil
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Daemon running (version=%s pid=%d started_at=%s socket=%s workspaces=%d)\n",
		daemonVersionLabel(h), h.PID, h.StartedAt.Format(time.RFC3339), sockPath, len(h.Workspaces))
	for _, ws := range h.Workspaces {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", ws)
	}
	return nil
}

// daemonVersionLabel renders a health response version, tolerating daemons
// that predate version reporting.
func daemonVersionLabel(h *api.HealthResponse) string {
	if h == nil || strings.TrimSpace(h.Version) == "" {
		return "unknown"
	}
	return h.Version
}

// hasActiveSessions reports whether any session on the daemon is doing work
// that a restart would interrupt.
func hasActiveSessions(sessions []api.Session) bool {
	for _, s := range sessions {
		if s.Status != api.StatusIdle {
			return true
		}
	}
	return false
}

// restartDaemon asks a healthy daemon to shut down gracefully and waits for
// it to stop answering health checks so the caller can spawn a replacement.
func restartDaemon(ctx context.Context, c *client.Client) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("stop outdated daemon: %w", err)
	}
	waitCtx, waitCancel := context.WithTimeout(ctx, 10*time.Second)
	defer waitCancel()
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timed out waiting for outdated daemon to exit: %w", waitCtx.Err())
		case <-ticker.C:
			if !isDaemonHealthy(waitCtx, c) {
				return nil
			}
		}
	}
}

// ConnectOrStart connects to an existing Brain Daemon at socketPath, or spawns
// a detached background daemon process (`os.Executable() daemon serve --socket <socketPath>`)
// and polls `/v1/health` until ready.
//
// When the running daemon was built from a different version than the client,
// ConnectOrStart restarts it only once every session is confirmed idle. In any
// other case — an active session or an unknown session state — the existing
// daemon is kept and the skew is returned as a user-facing notice so callers
// can surface it in their own UI; the user then restarts the daemon when it is
// safe instead of having work interrupted.
func ConnectOrStart(ctx context.Context, socketPath string, spawnArgs ...string) (*client.Client, []string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		defaultSock, err := daemon.DefaultSocketPath()
		if err != nil {
			return nil, nil, fmt.Errorf("resolve default socket path: %w", err)
		}
		socketPath = defaultSock
	}

	c, err := client.New(socketPath)
	if err != nil {
		return nil, nil, err
	}

	var notices []string
	h, healthy := healthDaemon(ctx, c)
	if healthy {
		if !daemonVersionSkewed(h) {
			return c, notices, nil
		}
		sessions, err := c.ListSessions(ctx)
		if err != nil || hasActiveSessions(sessions) {
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

	if err := spawnDetachedDaemon(socketPath, spawnArgs); err != nil {
		return nil, notices, err
	}

	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, notices, fmt.Errorf("timed out waiting for daemon at %q: %w", socketPath, ctx.Err())
		case <-ticker.C:
			if isDaemonHealthy(ctx, c) {
				return c, notices, nil
			}
		}
	}
}

// daemonVersionSkewed reports whether a healthy daemon's build version
// differs from this client's. Daemons that predate version reporting are
// treated as skewed so they get upgraded on the next client launch.
func daemonVersionSkewed(h *api.HealthResponse) bool {
	if h == nil {
		return false
	}
	return strings.TrimSpace(h.Version) != version.Get()
}

func healthDaemon(ctx context.Context, c *client.Client) (*api.HealthResponse, bool) {
	pingCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	h, err := c.Health(pingCtx)
	if err != nil {
		return nil, false
	}
	return h, true
}

func connectOrStart(ctx context.Context, socketPath string, spawnArgs ...string) (*client.Client, []string, error) {
	return ConnectOrStart(ctx, socketPath, spawnArgs...)
}

func isDaemonHealthy(ctx context.Context, c *client.Client) bool {
	pingCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	_, err := c.Health(pingCtx)
	return err == nil
}

func spawnDetachedDaemon(socketPath string, spawnArgs []string) error {
	return daemon.SpawnDetached(socketPath, spawnArgs...)
}
