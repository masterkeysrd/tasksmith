package components

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// NormalizeVersion ensures a version string starts with 'v' and defaults to build info or "v0.0.0-dev".
func NormalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		} else {
			return "v0.0.0-dev"
		}
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

// Header is a trail functional component rendering the startup banner:
// Left: 3-line ASCII brand Logo
// Right: 3 session info lines:
// 1. TaskSmith <version>
// 2. <model> with <thinking> effort
// 3. <ShortenHomePath(workspace)>
var Header = trail.FC("Header", func(props store.HeaderData) trail.Node {
	scheme := theme.UseTheme()

	titleStyle := style.S().Bold(true)
	metaStyle := style.S().WhiteSpace(style.WhiteSpacePre)
	dimStyle := style.S()

	if scheme != nil {
		titleStyle = titleStyle.Foreground(scheme.Color.Text.Purple)
		metaStyle = metaStyle.Foreground(scheme.Color.Text.Secondary)
		dimStyle = dimStyle.Foreground(scheme.Color.Text.Secondary)
	}

	version := NormalizeVersion(props.Version)
	model := cmp.Or(strings.TrimSpace(props.Model), "default")
	thinking := cmp.Or(strings.TrimSpace(props.Thinking), "auto")
	workspace := ShortenHomePath(props.Workspace)

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			MarginTop(1).
			MarginBottom(1),
	},
		Logo(),
		trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn),
		},
			// Line 1: TaskSmith (bold purple) + <version> (secondary)
			trail.Box(trail.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow),
			},
				trail.Span(trail.SpanProps{
					Style: titleStyle,
				}, trail.Text("TaskSmith")),
				trail.Span(trail.SpanProps{
					Style: metaStyle,
				}, trail.Text(" "+version)),
			),
			// Line 2: <model> with <thinking> effort (secondary)
			trail.Box(trail.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow),
			},
				trail.Span(trail.SpanProps{
					Style: metaStyle,
				}, trail.Text(fmt.Sprintf("%s with %s effort", model, thinking))),
			),
			// Line 3: <shortened workspace path> (tertiary)
			trail.Box(trail.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow),
			},
				trail.Span(trail.SpanProps{
					Style: dimStyle,
				}, trail.Text(workspace)),
			),
		),
	)
})
