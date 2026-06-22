//go:build windows

package tools

import (
	"os/exec"
)

func prepareCmd(cmd *exec.Cmd) {
	// Standard Windows process creation
}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
