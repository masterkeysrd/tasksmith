//go:build darwin

package sandbox

import (
	"fmt"
	"os"
	"strings"
)

const darwinSandboxExecBin = "/usr/bin/sandbox-exec"

// checkNativeAvailable verifies that the macOS Seatbelt binary exists on disk.
func checkNativeAvailable() error {
	info, err := os.Stat(darwinSandboxExecBin)
	if err != nil || info.IsDir() {
		return fmt.Errorf("%w: %s not found on darwin host", ErrNativeUnavailable, darwinSandboxExecBin)
	}
	return nil
}

// wrapNativeCommand wraps a resolved command invocation in macOS Seatbelt (sandbox-exec).
func wrapNativeCommand(s *session, _ string, resolvedBin string, args []string) (string, []string, error) {
	profile := buildSeatbeltProfile(s.workDir, s.realWorkDir, s.cfg.ReadOnly, s.cfg.Network.Disabled)
	wrappedArgs := make([]string, 0, 4+len(args))
	wrappedArgs = append(wrappedArgs, "-p", profile, "--", resolvedBin)
	wrappedArgs = append(wrappedArgs, args...)
	return darwinSandboxExecBin, wrappedArgs, nil
}

// buildSeatbeltProfile generates a macOS Seatbelt SBPL policy string.
//
// The policy allows reading system files and executing child processes, while
// restricting file writes exclusively to WorkDir (and its symlink-resolved path,
// such as /private/var/folders/... on macOS) when ReadOnly is false, or denying
// writes to WorkDir when ReadOnly is true. Outbound/inbound network access is
// omitted when networkDisabled is true.
func buildSeatbeltProfile(workDir, realWorkDir string, readOnly, networkDisabled bool) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")
	b.WriteString("(allow process*)\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow mach*)\n")
	b.WriteString("(allow ipc*)\n")
	b.WriteString("(allow signal)\n")
	b.WriteString("(allow file-read*)\n")
	b.WriteString("(allow file-write* (literal \"/dev/null\") (literal \"/dev/tty\") (literal \"/dev/zero\") (literal \"/dev/urandom\") (literal \"/dev/random\") (literal \"/dev/dtracehelper\"))\n")

	escapedWork := escapeSeatbeltString(workDir)
	escapedReal := escapeSeatbeltString(realWorkDir)

	if !readOnly {
		if realWorkDir != "" && realWorkDir != workDir {
			fmt.Fprintf(&b, "(allow file-write* (subpath \"%s\") (subpath \"%s\"))\n", escapedWork, escapedReal)
		} else {
			fmt.Fprintf(&b, "(allow file-write* (subpath \"%s\"))\n", escapedWork)
		}
	} else {
		if realWorkDir != "" && realWorkDir != workDir {
			fmt.Fprintf(&b, "(deny file-write* (subpath \"%s\") (subpath \"%s\"))\n", escapedWork, escapedReal)
		} else {
			fmt.Fprintf(&b, "(deny file-write* (subpath \"%s\"))\n", escapedWork)
		}
	}

	if !networkDisabled {
		b.WriteString("(allow network*)\n")
	}

	return b.String()
}

func escapeSeatbeltString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
