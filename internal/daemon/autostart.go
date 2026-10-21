package daemon

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
)

// ConnectOrStart connects to an existing Brain Daemon at socketPath, or spawns
// a detached background daemon process (`os.Executable()` with `daemon serve`
// or the provided spawnArgs) and polls `/v1/health` until ready.
func ConnectOrStart(ctx context.Context, socketPath string, spawnArgs ...string) (*client.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		defaultSock, err := DefaultSocketPath()
		if err != nil {
			return nil, fmt.Errorf("daemon: resolve default socket path: %w", err)
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
			return nil, fmt.Errorf("daemon: timed out waiting for daemon at %q: %w", socketPath, ctx.Err())
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
		return fmt.Errorf("daemon: resolve executable path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return fmt.Errorf("daemon: create socket directory: %w", err)
	}

	args := spawnArgs
	if len(args) == 0 {
		args = []string{"daemon", "serve"}
	}

	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("daemon: spawn background daemon: %w", err)
	}

	go func() {
		_ = cmd.Wait()
	}()
	return nil
}
