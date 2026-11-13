//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildBwrapArgs(t *testing.T) {
	linuxArgs := buildBwrapArgs(bwrapParams{
		WorkDir: "/work", RealWorkDir: "/work",
		TempDir: "/tmp/temp", RealTempDir: "/tmp/temp",
		TargetDir: "/work/sub", NetworkDisabled: true,
		ResolvedBin: "/bin/sh", Args: []string{"-c", "id"},
	})
	joinedLinux := strings.Join(linuxArgs, " ")
	if !strings.Contains(joinedLinux, "--ro-bind / / --bind /work /work --bind /tmp/temp /tmp/temp") || !strings.Contains(joinedLinux, "--unshare-net") {
		t.Fatalf("unexpected linux bwrap args: %s", joinedLinux)
	}
	if strings.Contains(joinedLinux, "--tmpfs") {
		t.Fatalf("bwrap args without denied paths must not mask anything: %s", joinedLinux)
	}
}

func TestBuildBwrapArgsMasksDeniedPaths(t *testing.T) {
	dir := t.TempDir()
	deniedDir := filepath.Join(dir, ".ssh")
	deniedFile := filepath.Join(dir, "global.env")
	if err := os.Mkdir(deniedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deniedFile, []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "does-not-exist")

	args := buildBwrapArgs(bwrapParams{
		WorkDir: "/work", RealWorkDir: "/work",
		TempDir: "/tmp/temp", RealTempDir: "/tmp/temp",
		TargetDir:   "/work",
		Filesystem:  FilesystemPolicy{DeniedReadPaths: []string{deniedDir, deniedFile, missing}},
		ResolvedBin: "/bin/sh", Args: []string{"-c", "id"},
	})
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--tmpfs "+deniedDir) {
		t.Fatalf("bwrap args missing tmpfs mask over denied directory: %s", joined)
	}
	if !strings.Contains(joined, "--ro-bind /dev/null "+deniedFile) {
		t.Fatalf("bwrap args missing /dev/null mask over denied file: %s", joined)
	}
	if strings.Contains(joined, missing) {
		t.Fatalf("bwrap args must not mask non-existent paths: %s", joined)
	}
}

func TestBuildBwrapArgsExtraAndDeniedWritePaths(t *testing.T) {
	dir := t.TempDir()
	extra := filepath.Join(dir, "main-repo", ".git")
	if err := os.MkdirAll(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(dir, "work", ".agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "work", "WORKSPACE.md")

	args := buildBwrapArgs(bwrapParams{
		WorkDir: filepath.Join(dir, "work"), RealWorkDir: filepath.Join(dir, "work"),
		TempDir: "/tmp/temp", RealTempDir: "/tmp/temp",
		TargetDir: filepath.Join(dir, "work"),
		Filesystem: FilesystemPolicy{
			ExtraWritePaths:  []string{extra, filepath.Join(dir, "missing-extra")},
			DeniedWritePaths: []string{agents, missing},
		},
		ResolvedBin: "/bin/sh", Args: []string{"-c", "id"},
	})
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--bind "+extra+" "+extra) {
		t.Fatalf("bwrap args missing writable bind for extra write path: %s", joined)
	}
	if strings.Contains(joined, filepath.Join(dir, "missing-extra")) {
		t.Fatalf("bwrap args must not bind non-existent extra write paths: %s", joined)
	}
	if !strings.Contains(joined, "--ro-bind "+agents+" "+agents) {
		t.Fatalf("bwrap args missing read-only overlay for denied write path: %s", joined)
	}
	if strings.Contains(joined, missing) {
		t.Fatalf("bwrap args must not overlay non-existent denied write paths: %s", joined)
	}

	// Mount order matters: the ro overlay must come after the writable binds
	// so it wins inside the mount namespace.
	workBindIdx := strings.Index(joined, "--bind "+filepath.Join(dir, "work"))
	roOverlayIdx := strings.Index(joined, "--ro-bind "+agents)
	if workBindIdx < 0 || roOverlayIdx < 0 || roOverlayIdx < workBindIdx {
		t.Fatalf("denied write overlay must be mounted after writable binds: %s", joined)
	}

	// A read-only session must not bind extra write paths writable.
	roArgs := buildBwrapArgs(bwrapParams{
		WorkDir: filepath.Join(dir, "work"), RealWorkDir: filepath.Join(dir, "work"),
		TempDir: "/tmp/temp", RealTempDir: "/tmp/temp",
		TargetDir:   filepath.Join(dir, "work"),
		ReadOnly:    true,
		Filesystem:  FilesystemPolicy{ExtraWritePaths: []string{extra}},
		ResolvedBin: "/bin/sh", Args: []string{"-c", "id"},
	})
	if strings.Contains(strings.Join(roArgs, " "), "--bind "+extra) {
		t.Fatalf("read-only bwrap args must not bind extra write paths: %s", strings.Join(roArgs, " "))
	}
}

func TestBuildBwrapArgsReadRuleOrdering(t *testing.T) {
	dir := t.TempDir()
	broad := filepath.Join(dir, ".ssh")
	narrow := filepath.Join(broad, "keys")
	if err := os.MkdirAll(narrow, 0o700); err != nil {
		t.Fatal(err)
	}

	args := buildBwrapArgs(bwrapParams{
		WorkDir: "/work", RealWorkDir: "/work",
		TargetDir:   "/work",
		Filesystem:  FilesystemPolicy{DeniedReadPaths: []string{broad}, AllowReadPaths: []string{narrow}},
		ResolvedBin: "/bin/sh", Args: []string{"-c", "id"},
	})
	joined := strings.Join(args, " ")

	maskIdx := strings.Index(joined, "--tmpfs "+broad)
	reopenIdx := strings.Index(joined, "--ro-bind "+narrow+" "+narrow)
	if maskIdx < 0 || reopenIdx < 0 || reopenIdx < maskIdx {
		t.Fatalf("narrow allow re-open must be overlaid after broad deny mask: %s", joined)
	}
}

func TestBuildBwrapArgsRebindsWorkDirUnderBroadMask(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, "project")
	agents := filepath.Join(work, ".agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}

	args := buildBwrapArgs(bwrapParams{
		WorkDir: work, RealWorkDir: work,
		TempDir: "/tmp/temp", RealTempDir: "/tmp/temp",
		TargetDir:   work,
		Filesystem:  FilesystemPolicy{DeniedReadPaths: []string{home}, DeniedWritePaths: []string{agents}},
		ResolvedBin: "/bin/sh", Args: []string{"-c", "id"},
	})
	joined := strings.Join(args, " ")

	maskIdx := strings.Index(joined, "--tmpfs "+home)
	workIdx := strings.Index(joined, "--bind "+work+" "+work)
	agentsIdx := strings.Index(joined, "--ro-bind "+agents+" "+agents)
	if maskIdx < 0 || workIdx < 0 || agentsIdx < 0 {
		t.Fatalf("broad deny must mask home, re-bind the work dir writable, and overlay protected paths: %s", joined)
	}
	if workIdx <= maskIdx {
		t.Fatalf("work dir must be re-bound after the tmpfs mask that would wipe it: %s", joined)
	}
	if agentsIdx <= workIdx {
		t.Fatalf("protected read-only overlay must land after the writable work bind: %s", joined)
	}
}
