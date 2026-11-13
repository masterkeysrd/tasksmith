//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildBwrapArgs(t *testing.T) {
	linuxArgs := buildBwrapArgs("/work", "/work", "/tmp/temp", "/tmp/temp", "/work/sub", false, true, nil, "/bin/sh", []string{"-c", "id"})
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

	args := buildBwrapArgs("/work", "/work", "/tmp/temp", "/tmp/temp", "/work", false, false,
		[]string{deniedDir, deniedFile, missing}, "/bin/sh", []string{"-c", "id"})
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
