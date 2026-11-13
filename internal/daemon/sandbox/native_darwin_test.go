//go:build darwin

package sandbox

import (
	"strings"
	"testing"
)

func TestBuildSeatbeltProfile(t *testing.T) {
	darwinRW := buildSeatbeltProfile("/tmp/work", "/private/tmp/work", "/tmp/temp", "/private/tmp/temp", false, false, nil)
	if !strings.Contains(darwinRW, `(allow file-write* (subpath "/tmp/work") (subpath "/private/tmp/work"))`) {
		t.Fatalf("darwin RW profile missing subpath write allow:\n%s", darwinRW)
	}
	if !strings.Contains(darwinRW, `(allow file-write* (subpath "/tmp/temp") (subpath "/private/tmp/temp"))`) {
		t.Fatalf("darwin RW profile missing temp subpath write allow:\n%s", darwinRW)
	}
	if !strings.Contains(darwinRW, "(allow network*)") {
		t.Fatalf("darwin RW profile missing network allow:\n%s", darwinRW)
	}
	if strings.Contains(darwinRW, "(deny file-read*)") {
		t.Fatalf("darwin profile without denied paths must not deny reads:\n%s", darwinRW)
	}

	darwinRO := buildSeatbeltProfile("/tmp/work", "/private/tmp/work", "/tmp/temp", "/private/tmp/temp", true, true, nil)
	if !strings.Contains(darwinRO, `(deny file-write* (subpath "/tmp/work") (subpath "/private/tmp/work"))`) {
		t.Fatalf("darwin RO profile missing subpath write deny:\n%s", darwinRO)
	}
	if !strings.Contains(darwinRO, `(allow file-write* (subpath "/tmp/temp") (subpath "/private/tmp/temp"))`) {
		t.Fatalf("darwin RO profile missing temp subpath write allow:\n%s", darwinRO)
	}
	if strings.Contains(darwinRO, "(allow network*)") {
		t.Fatalf("darwin network-disabled profile should not allow network:\n%s", darwinRO)
	}

	denied := []string{"/tmp/fake-home/.ssh", "/tmp/fake-home/config/tasksmith/.env", "  "}
	darwinDenied := buildSeatbeltProfile("/tmp/work", "/tmp/work", "/tmp/temp", "/tmp/temp", false, false, denied)
	if !strings.Contains(darwinDenied, `(deny file-read* (subpath "/tmp/fake-home/.ssh"))`) {
		t.Fatalf("darwin profile missing credential read deny:\n%s", darwinDenied)
	}
	if !strings.Contains(darwinDenied, `(deny file-read* (subpath "/tmp/fake-home/config/tasksmith/.env"))`) {
		t.Fatalf("darwin profile missing global .env read deny:\n%s", darwinDenied)
	}
	// Read denies must be emitted after the blanket read allow so Seatbelt's
	// last-match resolution makes them effective.
	allowIdx := strings.Index(darwinDenied, "(allow file-read*)")
	denyIdx := strings.Index(darwinDenied, "(deny file-read*")
	if allowIdx < 0 || denyIdx < 0 || denyIdx < allowIdx {
		t.Fatalf("credential read deny must appear after blanket read allow:\n%s", darwinDenied)
	}
}
