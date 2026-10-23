package components

import (
	"image/color"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// Tone is a semantic color role. It is resolved against the active theme when
// a status segment renders, so segments never reference concrete colors.
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
		return scheme.Color.Text.Tertiary
	}
}

// StatusLineProps defines the properties for the StatusLine layout.
type StatusLineProps struct {
	Children []trail.Node
}

// StatusLine lays its children out on one line. Compose it from self-sufficient
// segments and a [trail.Spacer] to push the right-hand ones to the edge:
//
//	StatusLine(StatusLineProps{},
//	    HintSegment(SegmentProps{}),
//	    trail.Spacer(),
//	    ModeSegment(SegmentProps{Separator: " · "}),
//	    ModelSegment(SegmentProps{}),
//	)
var StatusLine = trail.FCC("StatusLine", func(props StatusLineProps) trail.Node {
	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			Width(style.Percent(100)),
	}, props.Children...)
})

// DefaultStatusLine is the stock status line: the state hint on the left and
// the permission mode and model on the right.
var DefaultStatusLine = trail.FC("DefaultStatusLine", func(struct{}) trail.Node {
	return StatusLine(StatusLineProps{},
		HintSegment(SegmentProps{}),
		trail.Spacer(),
		ModeSegment(SegmentProps{Separator: " · "}),
		ModelSegment(SegmentProps{}),
	)
})

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
