package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
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
	c, err := connectOrStart(cmd.Context(), sockPath)
	if err != nil {
		return err
	}
	h, err := c.Health(cmd.Context())
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Daemon running (pid=%d socket=%s)\n", h.PID, c.SocketPath())
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
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Daemon running (pid=%d started_at=%s socket=%s workspaces=%d)\n",
		h.PID, h.StartedAt.Format(time.RFC3339), sockPath, len(h.Workspaces))
	for _, ws := range h.Workspaces {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", ws)
	}
	return nil
}

// connectOrStart connects to an existing Brain Daemon at socketPath, or spawns
// a detached background daemon process (`os.Executable() daemon serve --socket <socketPath>`)
// and polls `/v1/health` until ready.
func connectOrStart(ctx context.Context, socketPath string, spawnArgs ...string) (*client.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		defaultSock, err := daemon.DefaultSocketPath()
		if err != nil {
			return nil, fmt.Errorf("resolve default socket path: %w", err)
		}
		socketPath = defaultSock
	}

	c, err := client.New(socketPath)
	if err != nil {
		return nil, err
	}

	if isDaemonHealthy(ctx, c) {
		return c, nil
	}

	if err := spawnDetachedDaemon(socketPath, spawnArgs); err != nil {
		return nil, err
	}

	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for daemon at %q: %w", socketPath, ctx.Err())
		case <-ticker.C:
			if isDaemonHealthy(ctx, c) {
				return c, nil
			}
		}
	}
}

func isDaemonHealthy(ctx context.Context, c *client.Client) bool {
	pingCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	_, err := c.Health(pingCtx)
	return err == nil
}

func spawnDetachedDaemon(socketPath string, spawnArgs []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}

	args := spawnArgs
	if len(args) == 0 {
		args = []string{"daemon", "serve", "--socket", socketPath}
	}

	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn background daemon: %w", err)
	}

	go func() {
		_ = cmd.Wait()
	}()
	return nil
}
