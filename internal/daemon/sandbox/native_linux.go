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

// bwrapParams carries the resolved confinement inputs for [buildBwrapArgs].
type bwrapParams struct {
	WorkDir         string
	RealWorkDir     string
	TempDir         string
	RealTempDir     string
	TargetDir       string
	ReadOnly        bool
	NetworkDisabled bool
	Filesystem      FilesystemPolicy
	ResolvedBin     string
	Args            []string
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
	wrappedArgs := buildBwrapArgs(bwrapParams{
		WorkDir:         s.workDir,
		RealWorkDir:     s.realWorkDir,
		TempDir:         s.tempDir,
		RealTempDir:     s.realTempDir,
		TargetDir:       targetDir,
		ReadOnly:        s.readOnly.Load() || cmdReadOnly,
		NetworkDisabled: s.cfg.Network.Disabled,
		Filesystem:      policy,
		ResolvedBin:     resolvedBin,
		Args:            args,
	})
	return bwrapBin, wrappedArgs, nil
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
	if params.NetworkDisabled {
		out = append(out, "--unshare-net")
	}
	out = append(out, "--chdir", params.TargetDir, "--", params.ResolvedBin)
	out = append(out, params.Args...)
	return out
}
