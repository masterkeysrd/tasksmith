//go:build darwin

package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const darwinSandboxExecBin = "/usr/bin/sandbox-exec"

// nativeBackendName identifies the macOS kernel sandbox backend.
func nativeBackendName() string { return "seatbelt" }

// nativeAvailabilityRemediation returns guidance for a failed Seatbelt probe.
// sandbox-exec ships with macOS, so a failing probe means the daemon itself
// runs inside a restricted context (a sandboxed parent, IDE, or CI runner)
// that offers no user-side fix; sessions simply degrade to local isolation.
func nativeAvailabilityRemediation(_ error) string { return "" }

// checkNativeAvailable verifies that the macOS Seatbelt binary exists and can
// actually apply a sandbox profile in this environment. A parent process that
// is itself Seatbelt-sandboxed (IDE/CI runners) or otherwise restricted keeps
// sandbox-exec on disk yet fails every sandbox_apply, so a trivial probe run
// decides availability instead of a bare stat.
func checkNativeAvailable() error {
	info, err := os.Stat(darwinSandboxExecBin)
	if err != nil || info.IsDir() {
		return fmt.Errorf("%w: %s not found on darwin host", ErrNativeUnavailable, darwinSandboxExecBin)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	probe := exec.CommandContext(ctx, darwinSandboxExecBin, "-p", "(version 1)\n(allow default)\n", "/usr/bin/true")
	if out, err := probe.CombinedOutput(); err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("%w: %s cannot apply a sandbox profile: %s", ErrNativeUnavailable, darwinSandboxExecBin, detail)
	}
	return nil
}

// wrapNativeCommand wraps a resolved command invocation in macOS Seatbelt (sandbox-exec).
func wrapNativeCommand(s *session, _ string, resolvedBin string, args []string, cmdReadOnly bool) (string, []string, error) {
	p := s.fsPolicy.Load()
	policy := FilesystemPolicy{}
	if p != nil {
		policy = *p
	}
	network, proxyPort := s.networkPosture()
	if network.Allowlist && !network.Disabled && proxyPort == 0 {
		return "", nil, fmt.Errorf("%w: network allowlist is active but the sandbox network proxy is not running", ErrProxyUnavailable)
	}
	profile := buildSeatbeltProfile(seatbeltParams{
		WorkDir:          s.workDir,
		RealWorkDir:      s.realWorkDir,
		TempDir:          s.tempDir,
		RealTempDir:      s.realTempDir,
		ReadOnly:         s.readOnly.Load() || cmdReadOnly,
		NetworkDisabled:  network.Disabled,
		NetworkAllowlist: network.Allowlist && !network.Disabled,
		ProxyPort:        proxyPort,
		Filesystem:       policy,
	})
	wrappedArgs := make([]string, 0, 4+len(args))
	wrappedArgs = append(wrappedArgs, "-p", profile, "--", resolvedBin)
	wrappedArgs = append(wrappedArgs, args...)
	return darwinSandboxExecBin, wrappedArgs, nil
}

// seatbeltParams carries the resolved confinement inputs for
// [buildSeatbeltProfile].
type seatbeltParams struct {
	WorkDir     string
	RealWorkDir string
	TempDir     string
	RealTempDir string
	ReadOnly    bool
	// NetworkDisabled drops all network rules from the profile.
	NetworkDisabled bool
	// NetworkAllowlist confines outbound traffic to the daemon-side
	// allowlist proxy. Without an active ProxyPort the profile stays
	// fail-closed (no network rules at all).
	NetworkAllowlist bool
	// ProxyPort is the loopback port of the active allowlist proxy.
	ProxyPort  int
	Filesystem FilesystemPolicy
}

// buildSeatbeltProfile generates a macOS Seatbelt SBPL policy string.
//
// The policy allows reading system files and executing child processes, while
// restricting file writes to WorkDir (and its symlink-resolved path, such as
// /private/var/folders/... on macOS), the dedicated temp directory, and every
// ExtraWritePaths entry when ReadOnly is false, or denying writes to WorkDir
// when ReadOnly is true. Outbound/inbound network access is omitted when
// NetworkDisabled is true, and confined to the loopback proxy port when
// ProxyPort is non-zero (allowlist posture).
//
// Seatbelt resolves file rules by last-match wins, so read rules are emitted
// after the blanket read allow sorted from broadest to narrowest path —
// narrower DeniedReadPaths entries re-mask secrets inside subtrees an
// AllowReadPaths entry re-opened — and every DeniedWritePaths entry (in both
// its logical and symlink-resolved form) is emitted as a trailing
// file-write* deny so protected paths stay read-only even when a broad write
// allow covers them.
func buildSeatbeltProfile(params seatbeltParams) string {
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

	escapedWork := escapeSeatbeltString(params.WorkDir)
	escapedReal := escapeSeatbeltString(params.RealWorkDir)

	if !params.ReadOnly {
		if params.RealWorkDir != "" && params.RealWorkDir != params.WorkDir {
			fmt.Fprintf(&b, "(allow file-write* (subpath \"%s\") (subpath \"%s\"))\n", escapedWork, escapedReal)
		} else {
			fmt.Fprintf(&b, "(allow file-write* (subpath \"%s\"))\n", escapedWork)
		}
	} else {
		if params.RealWorkDir != "" && params.RealWorkDir != params.WorkDir {
			fmt.Fprintf(&b, "(deny file-write* (subpath \"%s\") (subpath \"%s\"))\n", escapedWork, escapedReal)
		} else {
			fmt.Fprintf(&b, "(deny file-write* (subpath \"%s\"))\n", escapedWork)
		}
	}

	if params.TempDir != "" {
		escapedTemp := escapeSeatbeltString(params.TempDir)
		escapedRealTemp := escapeSeatbeltString(params.RealTempDir)
		if params.RealTempDir != "" && params.RealTempDir != params.TempDir {
			fmt.Fprintf(&b, "(allow file-write* (subpath \"%s\") (subpath \"%s\"))\n", escapedTemp, escapedRealTemp)
		} else {
			fmt.Fprintf(&b, "(allow file-write* (subpath \"%s\"))\n", escapedTemp)
		}
	}

	if !params.ReadOnly {
		for _, extra := range withCanonicalVariants(params.Filesystem.ExtraWritePaths) {
			fmt.Fprintf(&b, "(allow file-write* (subpath \"%s\"))\n", escapeSeatbeltString(extra))
		}
	}

	switch {
	case params.NetworkDisabled:
		// No network rules: the implicit deny default keeps children offline.
	case params.NetworkAllowlist:
		// Allowlist posture: children may only reach the daemon-side domain
		// proxy on loopback. Outbound is pinned to the proxy's port; the bind
		// rule lets children open the (ephemeral) client socket itself.
		// If the proxy failed to start (ProxyPort == 0) the profile keeps the
		// implicit deny: an allowlist must never fail open to full access.
		if params.ProxyPort > 0 {
			fmt.Fprintf(&b, "(allow network-outbound (remote ip \"localhost:%d\"))\n", params.ProxyPort)
			b.WriteString("(allow network-bind (local ip \"localhost:*\"))\n")
		}
	default:
		b.WriteString("(allow network*)\n")
	}

	// A broad DeniedReadPaths mask (e.g. denyRead: ["~"]) must never blanket the
	// sandbox's own operational roots: without read access to the work or temp
	// directory no command can run. Roots are re-opened at their own breadth, so
	// a narrower denyRead (e.g. <workDir>/secrets) still wins inside them.
	readRules := mergeReadRules(params.Filesystem.DeniedReadPaths, params.Filesystem.AllowReadPaths,
		operationalPolicyRoots(params.WorkDir, params.RealWorkDir, params.TempDir, params.RealTempDir, params.Filesystem.ExtraWritePaths))
	for _, rule := range readRules {
		fmt.Fprintf(&b, "(%s file-read* (subpath \"%s\"))\n", rule.Op, escapeSeatbeltString(rule.Path))
	}

	for _, denied := range withCanonicalVariants(params.Filesystem.DeniedWritePaths) {
		fmt.Fprintf(&b, "(deny file-write* (subpath \"%s\"))\n", escapeSeatbeltString(denied))
	}

	return b.String()
}

func escapeSeatbeltString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
