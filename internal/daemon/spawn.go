package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// SpawnDetached resolves the current executable and spawns a background detached daemon process.
// If spawnArgs is empty, default arguments ["daemon", "serve", "--socket", socketPath] are used.
// The child process is detached with Setsid and reaped asynchronously in a goroutine.
func SpawnDetached(socketPath string, spawnArgs ...string) error {
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
