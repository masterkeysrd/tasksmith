//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var linuxBwrapCandidatePaths = []string{
	"/usr/bin/bwrap",
	"/bin/bwrap",
	"/usr/local/bin/bwrap",
}

// nativeBackendName identifies the Linux kernel sandbox backend.
func nativeBackendName() string { return "bubblewrap" }

// nativeAvailabilityRemediation classifies a failed Bubblewrap probe into
// actionable guidance: a missing binary is an install problem, while a bwrap
// that exists yet cannot create namespaces is almost always Ubuntu 24.04+
// restricting unprivileged user namespaces via AppArmor (or a container
// without userns support).
func nativeAvailabilityRemediation(err error) string {
	if strings.Contains(err.Error(), "binary not found") {
		return "Install bubblewrap, e.g. \"sudo apt-get install bubblewrap\" (Debian/Ubuntu), " +
			"\"sudo dnf install bubblewrap\" (Fedora), or \"bubblewrap\" via your distribution's package manager."
	}
	return "Bubblewrap cannot create user namespaces on this host. On Ubuntu 24.04+ this is " +
		"usually AppArmor restricting unprivileged user namespaces; enable them with " +
		"\"sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0\" (persisted via " +
		"/etc/sysctl.d/), or run TaskSmith inside a container configured with unprivileged " +
		"user namespace support."
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

// bwrapParams carries the resolved confinement inputs for [buildBwrapArgs].
type bwrapParams struct {
	WorkDir         string
	RealWorkDir     string
	TempDir         string
	RealTempDir     string
	TargetDir       string
	ReadOnly        bool
	NetworkDisabled bool
	// NetworkAllowlist keeps the network namespace closed even when a relay
	// is not wired up, so an allowlist posture can never fail open to the
	// host network.
	NetworkAllowlist bool
	// ProxySocket, when set, routes child traffic to the daemon-side
	// allowlist proxy through a relay bound to this Unix socket inside the
	// (bind-mounted) sandbox temp dir.
	ProxySocket string
	Filesystem  FilesystemPolicy
	ResolvedBin string
	Args        []string
}

// wrapNativeCommand wraps a resolved command invocation in Linux Bubblewrap (bwrap).
func wrapNativeCommand(s *session, targetDir, resolvedBin string, args []string, cmdReadOnly bool) (string, []string, error) {
	bwrapBin, err := findLinuxBwrap()
	if err != nil {
		return "", nil, err
	}
	p := s.fsPolicy.Load()
	policy := FilesystemPolicy{}
	if p != nil {
		policy = *p
	}
	network, proxyPort := s.networkPosture()
	allowlist := network.Allowlist && !network.Disabled
	if allowlist && proxyPort == 0 {
		return "", nil, fmt.Errorf("%w: network allowlist is active but the sandbox network proxy is not running", ErrProxyUnavailable)
	}
	bin := bwrapBin
	wrappedBin := resolvedBin
	wrappedArgs := args
	var proxySocket string
	if allowlist && proxyPort > 0 {
		proxySocket = s.netProxySocketPath()
		// Inside the closed namespace 127.0.0.1 is not usable until lo is up,
		// and the proxy is only reachable as a Unix socket on the mounted
		// temp dir. Replace the target with the hidden relay bootstrap, which
		// raises lo, bridges loopback proxy traffic to the socket, then execs
		// the real command in place.
		self, err := os.Executable()
		if err != nil {
			return "", nil, fmt.Errorf("sandbox: locate TaskSmith binary for network relay: %w", err)
		}
		wrappedBin = self
		wrappedArgs = append([]string{
			"sandbox-relay",
			"--socket", proxySocket,
			"--port", strconv.Itoa(proxyPort),
			"--", resolvedBin,
		}, args...)
	}
	bwrapArgs := buildBwrapArgs(bwrapParams{
		WorkDir:          s.workDir,
		RealWorkDir:      s.realWorkDir,
		TempDir:          s.tempDir,
		RealTempDir:      s.realTempDir,
		TargetDir:        targetDir,
		ReadOnly:         s.readOnly.Load() || cmdReadOnly,
		NetworkDisabled:  network.Disabled,
		NetworkAllowlist: allowlist,
		ProxySocket:      proxySocket,
		Filesystem:       policy,
		ResolvedBin:      wrappedBin,
		Args:             wrappedArgs,
	})
	return bin, bwrapArgs, nil
}

// buildBwrapArgs constructs arguments for Linux Bubblewrap (bwrap).
//
// The host filesystem is mounted read-only at /, then a single breadth-sorted
// mount plan (see planMountSteps) is emitted: read masks (an empty tmpfs over
// directories, a /dev/null read-only bind over single files) for
// DeniedReadPaths, read-only re-opens for AllowReadPaths, writable binds for
// the sandbox's own roots (work dir unless readOnly, temp dir, existing
// ExtraWritePaths entries) so a broad denyRead like "~" can never wipe out the
// workspace or temp mounts, and read-only overlays for existing
// DeniedWritePaths entries so protected paths stay read-only inside every
// writable bind that covers them. Because bwrap resolves mounts in argument
// order — later mounts overlay earlier ones — the plan's broadest-first,
// writable-after-allow, deny-write-last ordering makes the narrowest matching
// operation win at every path.
func buildBwrapArgs(params bwrapParams) []string {
	out := []string{
		"--ro-bind", "/", "/",
	}
	for _, step := range planMountSteps(mountPlanInput{
		WorkDir:     params.WorkDir,
		RealWorkDir: params.RealWorkDir,
		TempDir:     params.TempDir,
		RealTempDir: params.RealTempDir,
		ReadOnly:    params.ReadOnly,
		Filesystem:  params.Filesystem,
	}) {
		switch step.Kind {
		case mountAllowRead:
			out = append(out, "--ro-bind", step.Path, step.Path)
		case mountWritable:
			out = append(out, "--bind", step.Path, step.Path)
		case mountDenyRead:
			// bwrap --tmpfs only mounts over directories; mask a single
			// sensitive file by covering it with an empty read-only file.
			// The planner already dropped non-existent paths, but the
			// file/dir decision needs a fresh stat.
			info, err := os.Stat(step.Path)
			if err != nil {
				continue
			}
			if !info.IsDir() {
				out = append(out, "--ro-bind", "/dev/null", step.Path)
				continue
			}
			out = append(out, "--tmpfs", step.Path)
		case mountDenyWrite:
			out = append(out, "--ro-bind", step.Path, step.Path)
		}
	}
	out = append(out,
		"--dev", "/dev",
		"--proc", "/proc",
		"--unshare-pid",
		"--die-with-parent",
	)
	if params.NetworkDisabled || params.NetworkAllowlist || params.ProxySocket != "" {
		// Under the allowlist posture the namespace stays closed as well; the
		// wrapped command is a relay bootstrap that bridges loopback to the
		// proxy socket on the bind-mounted temp dir.
		out = append(out, "--unshare-net")
	}
	out = append(out, "--chdir", params.TargetDir, "--", params.ResolvedBin)
	out = append(out, params.Args...)
	return out
}
