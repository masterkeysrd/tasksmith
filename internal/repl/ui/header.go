package ui

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// HeaderProps defines the properties for the 3-line REPL startup header.
type HeaderProps struct {
	Version  string
	Model    string
	Thinking string
	Path     string
}

// NormalizeVersion ensures a version string starts with 'v' and defaults to "v0.1.0".
func NormalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "v0.1.0"
	}
	if !strings.HasPrefix(version, "v") {
		return "v" + version
	}
	return version
}

// ShortenHomePath replaces a leading user home directory with "~".
func ShortenHomePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	home = filepath.Clean(home)
	cleaned := filepath.Clean(path)
	if cleaned == home {
		return "~"
	}
	if strings.HasPrefix(cleaned, home+string(filepath.Separator)) {
		return "~" + cleaned[len(home):]
	}
	return path
}

// Header is a trail functional component rendering the 3-line startup banner:
// 1. TaskSmith <version>
// 2. <Model> with <Thinking> effort
// 3. <ShortenHomePath(Path)>
var Header = trail.FC("Header", func(props HeaderProps) trail.Node {
	version := NormalizeVersion(props.Version)
	model := cmp.Or(strings.TrimSpace(props.Model), "default")
	thinking := cmp.Or(strings.TrimSpace(props.Thinking), "auto")
	path := ShortenHomePath(props.Path)

	return trail.Box(trail.BoxProps{
		Style: style.S().
			FlexDirection(style.FlexColumn).
			MarginBottom(1),
	},
		// Line 1: TaskSmith (bold primary) + <version> (muted)
		trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		},
			trail.Span(trail.SpanProps{
				Style: theme.BannerTitleStyle,
			}, trail.Text("TaskSmith")),
			trail.Span(trail.SpanProps{
				Style: theme.BannerMetaStyle,
			}, trail.Text(" "+version)),
		),
		// Line 2: <Model> with <Thinking> effort (muted)
		trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		},
			trail.Span(trail.SpanProps{
				Style: theme.BannerMetaStyle,
			}, trail.Text(fmt.Sprintf("%s with %s effort", model, thinking))),
		),
		// Line 3: <shortened workspace path with ~> (dim)
		trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		},
			trail.Span(trail.SpanProps{
				Style: theme.DimmedStyle,
			}, trail.Text(path)),
		),
	)
})
