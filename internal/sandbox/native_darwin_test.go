//go:build darwin

package sandbox

import (
	"strings"
	"testing"
)

func TestBuildSeatbeltProfile(t *testing.T) {
	darwinRW := buildSeatbeltProfile("/tmp/work", "/private/tmp/work", false, false)
	if !strings.Contains(darwinRW, `(allow file-write* (subpath "/tmp/work") (subpath "/private/tmp/work"))`) {
		t.Fatalf("darwin RW profile missing subpath write allow:\n%s", darwinRW)
	}
	if !strings.Contains(darwinRW, "(allow network*)") {
		t.Fatalf("darwin RW profile missing network allow:\n%s", darwinRW)
	}

	darwinRO := buildSeatbeltProfile("/tmp/work", "/private/tmp/work", true, true)
	if !strings.Contains(darwinRO, `(deny file-write* (subpath "/tmp/work") (subpath "/private/tmp/work"))`) {
		t.Fatalf("darwin RO profile missing subpath write deny:\n%s", darwinRO)
	}
	if strings.Contains(darwinRO, "(allow network*)") {
		t.Fatalf("darwin network-disabled profile should not allow network:\n%s", darwinRO)
	}
}
