package components

import (
	"cmp"
	"fmt"
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// StatusLineProps defines the properties for the StatusLine layout.
type StatusLineProps struct {
	Children []trail.Node
}

// SegmentProps defines the properties shared by status segments.
type SegmentProps struct {
	// Separator is rendered after the segment, only when the segment itself renders.
	Separator string
}

func modeColor(scheme *theme.Scheme, mode api.PermissionMode) color.Color {
	switch mode {
	case api.ModeAcceptEdits:
		return scheme.Color.Mode.AcceptEdits
	case api.ModePlan:
		return scheme.Color.Mode.Plan
	case api.ModeAsk:
		return scheme.Color.Mode.Ask
	case api.ModeAuto:
		return scheme.Color.Mode.Auto
	default:
		return scheme.Color.Mode.Default
	}
}

// statusText renders text in the given color.
func statusText(text string, col color.Color, bold bool) trail.Node {
	spanStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(col)
	if bold {
		spanStyle = spanStyle.Bold(true)
	}
	return trail.Span(trail.SpanProps{Style: spanStyle}, trail.Text(text))
}

// segment appends the separator to a rendered segment and hides empty ones.
func segment(props SegmentProps, content trail.Node, text string, sepColor color.Color) trail.Node {
	if text == "" {
		return nil
	}
	if props.Separator == "" {
		return content
	}
	return trail.Box(trail.BoxProps{},
		content,
		statusText(props.Separator, sepColor, false),
	)
}

// HintSegment shows the shortcut hint or Ctrl+C exit confirmation from store.App.
var HintSegment = trail.FC("HintSegment", func(props SegmentProps) trail.Node {
	scheme := theme.UseTheme()
	ctrlCPending := store.UseApp(func(s store.AppState) bool {
		return s.CtrlCPending
	})
	hint := store.UseApp(func(s store.AppState) string {
		return s.Hint
	})
	hasSlot := store.UseHasActiveSlot()

	if !ctrlCPending && hasSlot {
		return nil
	}

	text := hint
	if ctrlCPending {
		text = store.CtrlCHint
	}
	return segment(props, statusText(text, scheme.Color.Text.Secondary, false), text, scheme.Color.Text.Secondary)
})

// ModeSegment shows a colored badge for non-default permission modes.
var ModeSegment = trail.FC("ModeSegment", func(props SegmentProps) trail.Node {
	scheme := theme.UseTheme()
	mode := store.UseMode()
	if mode == "" || mode == api.ModeDefault {
		return nil
	}
	text := string(mode)
	return segment(props, statusText(text, modeColor(scheme, mode), true), text, scheme.Color.Text.Secondary)
})

// ContextSegment shows compact context usage (e.g. "109.5k/400.0k (27%)") once session context is known.
var ContextSegment = trail.FC("ContextSegment", func(props SegmentProps) trail.Node {
	scheme := theme.UseTheme()
	ctx := store.UseContext()
	if ctx.MaxTokens == 0 {
		return nil
	}

	pct := 0
	if ctx.MaxTokens > 0 {
		pct = min(int(float64(ctx.UsedTokens)/float64(ctx.MaxTokens)*100), 100)
	}
	text := fmt.Sprintf("%s/%s (%d%%)", format.FormatTokens(ctx.UsedTokens), format.FormatTokens(ctx.MaxTokens), pct)

	col := scheme.Color.Text.Secondary
	if ctx.TriggerTokens > 0 && ctx.UsedTokens >= ctx.TriggerTokens {
		col = scheme.Color.Text.Error
	} else if ctx.TriggerTokens > 0 && ctx.UsedTokens >= (ctx.TriggerTokens*3)/4 {
		col = scheme.Color.Text.Warning
	}

	return segment(props, statusText(text, col, false), text, scheme.Color.Text.Secondary)
})

// ModelSegment shows "<model> · <thinking>" once session model info is ready.
var ModelSegment = trail.FC("ModelSegment", func(props SegmentProps) trail.Node {
	scheme := theme.UseTheme()
	ready := store.UseSession(func(s store.SessionState) bool {
		return s.Ready
	})
	modelLabel := store.UseSession(func(s store.SessionState) string {
		return s.ModelLabel
	})
	thinking := store.UseSession(func(s store.SessionState) string {
		return s.Thinking
	})
	if !ready {
		return nil
	}

	model := cmp.Or(strings.TrimSpace(modelLabel), "default")
	effort := cmp.Or(strings.TrimSpace(thinking), "auto")
	text := fmt.Sprintf("%s · %s", model, effort)
	return segment(props, statusText(text, scheme.Color.Text.Secondary, false), text, scheme.Color.Text.Secondary)
})

// StatusLine lays its children out horizontally across the full width.
var StatusLine = trail.FCC("StatusLine", func(props StatusLineProps) trail.Node {
	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			Width(style.Percent(100)),
	}, props.Children...)
})

// DefaultStatusLine renders the default REPL status line: hint on the left,
// mode and model info on the right.
var DefaultStatusLine = trail.SimpleFC("DefaultStatusLine", func() trail.Node {
	return StatusLine(StatusLineProps{},
		HintSegment(SegmentProps{}),
		trail.Spacer(),
		ModeSegment(SegmentProps{Separator: " · "}),
		ContextSegment(SegmentProps{Separator: " · "}),
		ModelSegment(SegmentProps{}),
	)
})
