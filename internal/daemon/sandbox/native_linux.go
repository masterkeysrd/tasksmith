//go:build linux

package sandbox

import (
	"fmt"
	"os"
)

var linuxBwrapCandidatePaths = []string{
	"/usr/bin/bwrap",
	"/bin/bwrap",
	"/usr/local/bin/bwrap",
}

// checkNativeAvailable verifies that the Linux Bubblewrap (bwrap) binary exists on disk.
func checkNativeAvailable() error {
	_, err := findLinuxBwrap()
	return err
}

func findLinuxBwrap() (string, error) {
	for _, p := range linuxBwrapCandidatePaths {
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("%w: bubblewrap (bwrap) binary not found in standard system paths", ErrNativeUnavailable)
}

// wrapNativeCommand wraps a resolved command invocation in Linux Bubblewrap (bwrap).
func wrapNativeCommand(s *session, targetDir, resolvedBin string, args []string) (string, []string, error) {
	bwrapBin, err := findLinuxBwrap()
	if err != nil {
		return "", nil, err
	}
	wrappedArgs := buildBwrapArgs(s.workDir, s.realWorkDir, s.tempDir, s.realTempDir, targetDir, s.cfg.ReadOnly, s.cfg.Network.Disabled, resolvedBin, args)
	return bwrapBin, wrappedArgs, nil
}

// buildBwrapArgs constructs arguments for Linux Bubblewrap (bwrap).
func buildBwrapArgs(workDir, realWorkDir, tempDir, realTempDir, targetDir string, readOnly, networkDisabled bool, resolvedBin string, args []string) []string {
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
