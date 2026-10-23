//go:build linux

package sandbox

import (
	"strings"
	"testing"
)

func TestBuildBwrapArgs(t *testing.T) {
	linuxArgs := buildBwrapArgs("/work", "/work", "/tmp/temp", "/tmp/temp", "/work/sub", false, true, "/bin/sh", []string{"-c", "id"})
	joinedLinux := strings.Join(linuxArgs, " ")
	if !strings.Contains(joinedLinux, "--ro-bind / / --bind /work /work --bind /tmp/temp /tmp/temp") || !strings.Contains(joinedLinux, "--unshare-net") {
		t.Fatalf("unexpected linux bwrap args: %s", joinedLinux)
	}
}
