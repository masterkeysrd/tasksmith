package version

import (
	"runtime/debug"
	"strings"
)

// Version holds the application version string. It can be overridden at build
// time using -ldflags="-X github.com/masterkeysrd/tasksmith/internal/version.Version=vX.Y.Z".
var Version = "v0.1.0"

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
