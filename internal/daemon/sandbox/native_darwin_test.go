//go:build darwin

package sandbox

import (
	"strings"
	"testing"
)

func TestBuildSeatbeltProfile(t *testing.T) {
	base := seatbeltParams{
		WorkDir: "/tmp/work", RealWorkDir: "/private/tmp/work",
		TempDir: "/tmp/temp", RealTempDir: "/private/tmp/temp",
	}

	darwinRW := buildSeatbeltProfile(base)
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

	roParams := base
	roParams.ReadOnly = true
	roParams.NetworkDisabled = true
	darwinRO := buildSeatbeltProfile(roParams)
	if !strings.Contains(darwinRO, `(deny file-write* (subpath "/tmp/work") (subpath "/private/tmp/work"))`) {
		t.Fatalf("darwin RO profile missing subpath write deny:\n%s", darwinRO)
	}
	if !strings.Contains(darwinRO, `(allow file-write* (subpath "/tmp/temp") (subpath "/private/tmp/temp"))`) {
		t.Fatalf("darwin RO profile missing temp subpath write allow:\n%s", darwinRO)
	}
	if strings.Contains(darwinRO, "(allow network*)") {
		t.Fatalf("darwin network-disabled profile should not allow network:\n%s", darwinRO)
	}

	policyParams := base
	policyParams.Filesystem = FilesystemPolicy{
		DeniedReadPaths: []string{"/tmp/fake-home/.ssh", "/tmp/fake-home/config/tasksmith/.env", "  "},
	}
	darwinDenied := buildSeatbeltProfile(policyParams)
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

func TestBuildSeatbeltProfileExtraAndDeniedWritePaths(t *testing.T) {
	params := seatbeltParams{
		WorkDir: "/tmp/work", RealWorkDir: "/tmp/work",
		TempDir: "/tmp/temp", RealTempDir: "/tmp/temp",
		Filesystem: FilesystemPolicy{
			ExtraWritePaths:  []string{"/tmp/main-repo/.git"},
			DeniedWritePaths: []string{"/tmp/work/.agents", "/tmp/work/.git/hooks", "/tmp/main-repo/.git/config"},
		},
	}
	profile := buildSeatbeltProfile(params)

	if !strings.Contains(profile, `(allow file-write* (subpath "/tmp/main-repo/.git"))`) {
		t.Fatalf("darwin profile missing extra write path allow:\n%s", profile)
	}
	for _, denied := range []string{"/tmp/work/.agents", "/tmp/work/.git/hooks", "/tmp/main-repo/.git/config"} {
		if !strings.Contains(profile, `(deny file-write* (subpath "`+denied+`"))`) {
			t.Fatalf("darwin profile missing protected write deny for %s:\n%s", denied, profile)
		}
	}

	// DeniedWritePaths denies must be emitted after every write allow so
	// Seatbelt's last-match resolution keeps protected paths read-only.
	lastAllow := strings.LastIndex(profile, "(allow file-write*")
	firstDeny := strings.Index(profile, "(deny file-write* (subpath \"/tmp/work/.agents\")")
	if lastAllow < 0 || firstDeny < 0 || firstDeny < lastAllow {
		t.Fatalf("protected write deny must appear after all write allows:\n%s", profile)
	}

	// A read-only session must not open ExtraWritePaths at all.
	roParams := params
	roParams.ReadOnly = true
	roProfile := buildSeatbeltProfile(roParams)
	if strings.Contains(roProfile, `(allow file-write* (subpath "/tmp/main-repo/.git"))`) {
		t.Fatalf("read-only profile must not allow extra write paths:\n%s", roProfile)
	}
}

func TestBuildSeatbeltProfileReadRuleOrdering(t *testing.T) {
	params := seatbeltParams{
		WorkDir: "/tmp/work", RealWorkDir: "/tmp/work",
		Filesystem: FilesystemPolicy{
			DeniedReadPaths: []string{"/tmp/fake-home/u/.ssh", "/tmp/fake-home/u/.ssh/id_ed25519"},
			AllowReadPaths:  []string{"/tmp/fake-home/u/.ssh/keys"},
		},
	}
	profile := buildSeatbeltProfile(params)

	idxBroadDeny := strings.Index(profile, `(deny file-read* (subpath "/tmp/fake-home/u/.ssh"))`)
	idxAllow := strings.Index(profile, `(allow file-read* (subpath "/tmp/fake-home/u/.ssh/keys"))`)
	idxNarrowDeny := strings.Index(profile, `(deny file-read* (subpath "/tmp/fake-home/u/.ssh/id_ed25519"))`)
	if idxBroadDeny < 0 || idxAllow < 0 || idxNarrowDeny < 0 {
		t.Fatalf("profile missing breadth-ordered read rules:\n%s", profile)
	}
	// Broadest-to-narrowest ordering so Seatbelt's last-match resolution lets
	// the narrowest rule win: broad deny < re-open allow < narrow re-mask deny.
	if idxBroadDeny >= idxAllow || idxAllow >= idxNarrowDeny {
		t.Fatalf("read rules must be ordered broadest to narrowest (deny, allow, deny):\n%s", profile)
	}
}

func TestBuildSeatbeltProfileDenyWinsExactTie(t *testing.T) {
	params := seatbeltParams{
		WorkDir: "/tmp/work", RealWorkDir: "/tmp/work",
		Filesystem: FilesystemPolicy{
			DeniedReadPaths: []string{"/tmp/fake-home/u/.ssh"},
			AllowReadPaths:  []string{"/tmp/fake-home/u/.ssh"},
		},
	}
	profile := buildSeatbeltProfile(params)
	allowIdx := strings.Index(profile, `(allow file-read* (subpath "/tmp/fake-home/u/.ssh"))`)
	denyIdx := strings.Index(profile, `(deny file-read* (subpath "/tmp/fake-home/u/.ssh"))`)
	if allowIdx < 0 || denyIdx < 0 || denyIdx < allowIdx {
		t.Fatalf("deny must be emitted after allow on an exact-tie path:\n%s", profile)
	}
}

func TestBuildSeatbeltProfileReopensOwnRootsUnderBroadDeny(t *testing.T) {
	params := seatbeltParams{
		WorkDir: "/tmp/fake-home/u/project", RealWorkDir: "/tmp/fake-home/u/project",
		TempDir: "/tmp/fake-home/u/tmp", RealTempDir: "/tmp/fake-home/u/tmp",
		Filesystem: FilesystemPolicy{DeniedReadPaths: []string{"/tmp/fake-home/u"}},
	}
	profile := buildSeatbeltProfile(params)

	maskIdx := strings.Index(profile, `(deny file-read* (subpath "/tmp/fake-home/u"))`)
	workIdx := strings.Index(profile, `(allow file-read* (subpath "/tmp/fake-home/u/project"))`)
	tempIdx := strings.Index(profile, `(allow file-read* (subpath "/tmp/fake-home/u/tmp"))`)
	if maskIdx < 0 || workIdx < 0 || tempIdx < 0 {
		t.Fatalf("profile must re-open its own roots under a broad deny:\n%s", profile)
	}
	if workIdx <= maskIdx || tempIdx <= maskIdx {
		t.Fatalf("root re-opens must be emitted after the broader deny so last-match wins:\n%s", profile)
	}
}
