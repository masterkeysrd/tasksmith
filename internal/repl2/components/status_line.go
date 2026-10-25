package components

import (
	"cmp"
	"fmt"
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// Tone is a semantic color role resolved against the active theme.
type Tone int

const (
	// ToneMuted is the default, low-emphasis status text.
	ToneMuted Tone = iota
	ToneInfo
	ToneSuccess
	TonePurple
	ToneMagenta
	ToneError
)

func (t Tone) color(scheme *theme.Scheme) color.Color {
	if scheme == nil {
		return nil
	}
	switch t {
	case ToneInfo:
		return scheme.Color.Surface.Info
	case ToneSuccess:
		return scheme.Color.Surface.Success
	case TonePurple:
		return scheme.Color.Text.Purple
	case ToneMagenta:
		return scheme.Color.Text.Magenta
	case ToneError:
		return scheme.Color.Text.Error
	default:
		return scheme.Color.Text.Secondary
	}
}

// SegmentProps defines the properties shared by status segments.
type SegmentProps struct {
	// Separator is rendered after the segment, only when the segment itself renders.
	Separator string
}

// StatusLineProps defines the properties for the StatusLine layout.
type StatusLineProps struct {
	Children []trail.Node
}

// statusText renders text in the given tone.
func statusText(text string, tone Tone, bold bool) trail.Node {
	spanStyle := style.S().WhiteSpace(style.WhiteSpacePre)
	if fg := tone.color(theme.UseTheme()); fg != nil {
		spanStyle = spanStyle.Foreground(fg)
	}
	if bold {
		spanStyle = spanStyle.Bold(true)
	}
	return trail.Span(trail.SpanProps{Style: spanStyle}, trail.Text(text))
}

// segment appends the separator to a rendered segment and hides empty ones.
func segment(props SegmentProps, content trail.Node, text string) trail.Node {
	if text == "" {
		return nil
	}
	if props.Separator == "" {
		return content
	}
	return trail.Box(trail.BoxProps{},
		content,
		statusText(props.Separator, ToneMuted, false),
	)
}

func modeTone(mode api.PermissionMode) Tone {
	switch mode {
	case api.ModeAcceptEdits:
		return ToneSuccess
	case api.ModePlan:
		return TonePurple
	case api.ModeAsk:
		return ToneMagenta
	case api.ModeAuto:
		return ToneError
	default:
		return ToneInfo
	}
}

// HintSegment shows the shortcut hint or Ctrl+C exit confirmation from store.App.
var HintSegment = trail.FC("HintSegment", func(props SegmentProps) trail.Node {
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
	return segment(props, statusText(text, ToneMuted, false), text)
})

// ModeSegment shows a colored badge for non-default permission modes.
var ModeSegment = trail.FC("ModeSegment", func(props SegmentProps) trail.Node {
	mode := store.UseMode()
	if mode == "" || mode == api.ModeDefault {
		return nil
	}
	text := string(mode)
	return segment(props, statusText(text, modeTone(mode), true), text)
})

// ModelSegment shows "<model> · <thinking>" once session model info is ready.
var ModelSegment = trail.FC("ModelSegment", func(props SegmentProps) trail.Node {
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
	return segment(props, statusText(text, ToneMuted, false), text)
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
		ModelSegment(SegmentProps{}),
	)
})
