package components

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
	tsversion "github.com/masterkeysrd/tasksmith/internal/version"
)

// NormalizeVersion ensures a version string starts with 'v' and defaults to build info or "v0.0.0-dev".
func NormalizeVersion(version string) string {
	return tsversion.Normalize(version)
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

// sandboxBannerSuffix renders the sandbox backend badge shown next to the
// workspace path in the banner. Native isolation is highlighted, an explicit
// local fallback is dimmed, and no badge is shown before a session exists.
func sandboxBannerSuffix(scheme *theme.Scheme, mode api.SandboxMode) (string, style.Style) {
	switch api.SandboxMode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case api.SandboxNative:
		return " · native sandbox", style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary)
	case api.SandboxLocal:
		return " · local sandbox", style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Muted)
	default:
		return "", style.S()
	}
}

// Header is a trail functional component rendering the startup banner:
// Left: 3-line ASCII brand Logo
// Right: 3 session info lines:
// 1. TaskSmith <version>
// 2. <model> with <thinking> effort
// 3. <ShortenHomePath(workspace)>
// Below (optional): workspace warnings
var Header = trail.FC("Header", func(props store.HeaderData) trail.Node {
	scheme := theme.UseTheme()

	titleStyle := style.S().Bold(true).Foreground(scheme.Color.Agent.Brand)
	metaStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary)
	dimStyle := style.S().Foreground(scheme.Color.Text.Secondary)

	version := NormalizeVersion(props.Version)
	model := cmp.Or(strings.TrimSpace(props.Model), "default")
	thinking := cmp.Or(strings.TrimSpace(props.Thinking), store.ThinkingProviderDefault)
	modelThinking := fmt.Sprintf("%s with %s effort", model, thinking)
	if thinking == store.ThinkingProviderDefault || strings.HasPrefix(thinking, "budget ") || strings.HasPrefix(thinking, "thinking ") {
		modelThinking = fmt.Sprintf("%s · %s", model, thinking)
	}
	workspace := ShortenHomePath(props.Workspace)
	sandboxLabel, sandboxStyle := sandboxBannerSuffix(scheme, props.SandboxMode)

	line3 := []trail.Node{
		trail.Span(trail.SpanProps{Style: dimStyle}, trail.Text(workspace)),
	}
	if sandboxLabel != "" {
		line3 = append(line3, trail.Span(trail.SpanProps{Style: sandboxStyle}, trail.Text(sandboxLabel)))
	}

	bannerContent := []trail.Node{
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
				}, trail.Text(modelThinking)),
			),
			// Line 3: <shortened workspace path> (tertiary) + sandbox backend
			trail.Box(trail.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow),
			}, line3...),
		),
	}

	if len(props.Warnings) == 0 {
		return trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				MarginTop(1).
				MarginBottom(1),
		}, bannerContent...)
	}

	warningStyle := style.S().
		Foreground(scheme.Color.Text.Warning).
		WhiteSpace(style.WhiteSpacePre)

	warningNodes := make([]trail.Node, 0, len(props.Warnings))
	for _, w := range props.Warnings {
		warningNodes = append(warningNodes, trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow),
		},
			trail.Span(trail.SpanProps{
				Style: warningStyle,
			}, icon.Warning, trail.Text(" "+w)),
		))
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			MarginTop(1).
			MarginBottom(1),
	},
		trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow),
		}, bannerContent...),
		trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				MarginTop(1),
		}, warningNodes...),
	)
})
