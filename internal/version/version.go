package version

import (
	"runtime/debug"
	"strings"
)

// Version holds the application version string. Release builds stamp it via
// -ldflags="-X github.com/masterkeysrd/tasksmith/internal/version.Version=vX.Y.Z"
// (see .goreleaser.yaml); the default marks unreleased development builds.
var Version = "v0.0.3-dev"

// Get returns the running version of TaskSmith, normalized to include the 'v' prefix.
func Get() string {
	return Normalize(Version)
}

// Normalize ensures a version string starts with 'v' and defaults to build info or "v0.0.0-dev".
func Normalize(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		} else {
			return "v0.0.0-dev"
		}
	}
	if !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}
