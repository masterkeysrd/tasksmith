//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var linuxBwrapCandidatePaths = []string{
	"/usr/bin/bwrap",
	"/bin/bwrap",
	"/usr/local/bin/bwrap",
}

// checkNativeAvailable verifies that the Linux Bubblewrap (bwrap) binary
// exists and can actually create a sandbox here. Unprivileged containers
// without user namespaces and AppArmor-restricted hosts (Ubuntu 24.04+) keep
// bwrap on disk yet fail every unshare, so a trivial namespace-creation probe
// decides availability instead of a bare stat.
func checkNativeAvailable() error {
	bwrapBin, err := findLinuxBwrap()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	probe := exec.CommandContext(ctx, bwrapBin,
		"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--unshare-pid",
		"--", "/bin/sh", "-c", ":")
	if out, err := probe.CombinedOutput(); err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("%w: %s cannot create a sandbox: %s", ErrNativeUnavailable, bwrapBin, detail)
	}
	return nil
}

func findLinuxBwrap() (string, error) {
	for _, p := range linuxBwrapCandidatePaths {
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	// Non-FHS distributions (NixOS, Guix, Linuxbrew) install bwrap outside the
	// standard system paths; honor PATH while requiring an absolute executable.
	if found, err := exec.LookPath("bwrap"); err == nil && filepath.IsAbs(found) {
		return found, nil
	}
	return "", fmt.Errorf("%w: bubblewrap (bwrap) binary not found in standard system paths or PATH", ErrNativeUnavailable)
}

// wrapNativeCommand wraps a resolved command invocation in Linux Bubblewrap (bwrap).
func wrapNativeCommand(s *session, targetDir, resolvedBin string, args []string, cmdReadOnly bool) (string, []string, error) {
	bwrapBin, err := findLinuxBwrap()
	if err != nil {
		return "", nil, err
	}
	wrappedArgs := buildBwrapArgs(s.workDir, s.realWorkDir, s.tempDir, s.realTempDir, targetDir,
		s.readOnly.Load() || cmdReadOnly, s.cfg.Network.Disabled, s.deniedPaths, resolvedBin, args)
	return bwrapBin, wrappedArgs, nil
}

// buildBwrapArgs constructs arguments for Linux Bubblewrap (bwrap).
//
// The host filesystem is mounted read-only at / with writable binds for
// WorkDir (unless readOnly) and the sandbox temp directory. Each path in
// deniedPaths (credential files and directories outside WorkDir) is masked
// with an empty tmpfs so child processes can neither read nor write it.
func buildBwrapArgs(workDir, realWorkDir, tempDir, realTempDir, targetDir string, readOnly, networkDisabled bool, deniedPaths []string, resolvedBin string, args []string) []string {
	out := []string{
		"--ro-bind", "/", "/",
	}
	if !readOnly {
		out = append(out, "--bind", workDir, workDir)
		if realWorkDir != "" && realWorkDir != workDir {
			out = append(out, "--bind", realWorkDir, realWorkDir)
		}
	}
	if tempDir != "" {
		out = append(out, "--bind", tempDir, tempDir)
		if realTempDir != "" && realTempDir != tempDir {
			out = append(out, "--bind", realTempDir, realTempDir)
		}
	}
	for _, denied := range deniedPaths {
		denied = strings.TrimSpace(denied)
		if denied == "" {
			continue
		}
		denied = filepath.Clean(denied)
		info, err := os.Stat(denied)
		if err != nil {
			continue // nothing to mask when the path does not exist on this host
		}
		if !info.IsDir() {
			// bwrap --tmpfs only mounts over directories; mask a single
			// sensitive file by covering it with an empty read-only file.
			out = append(out, "--ro-bind", "/dev/null", denied)
			continue
		}
		out = append(out, "--tmpfs", denied)
	}
	out = append(out,
		"--dev", "/dev",
		"--proc", "/proc",
		"--unshare-pid",
		"--die-with-parent",
	)
	if networkDisabled {
		out = append(out, "--unshare-net")
	}
	out = append(out, "--chdir", targetDir, "--", resolvedBin)
	out = append(out, args...)
	return out
}
