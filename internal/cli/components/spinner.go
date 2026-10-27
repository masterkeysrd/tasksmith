package components

import (
	"cmp"
	"image/color"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// SpinnerFrames defines the braille spinner glyph animation cycle.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SpinnerProps defines the properties for the animated thinking spinner.
type SpinnerProps struct {
	Text   string
	Status api.SessionStatus
}

// Spinner is a trail functional component rendering an animated braille spinner and status text.
var Spinner = trail.FC("Spinner", func(props SpinnerProps) trail.Node {
	scheme := theme.UseTheme()
	frame, setFrame := kitex.UseState(0)

	kitex.UseInterval(func() {
		setFrame((frame() + 1) % len(SpinnerFrames))
	}, 80*time.Millisecond, nil)

	var (
		fgColor     color.Color
		defaultText string
	)
	switch props.Status {
	case api.StatusGenerating:
		fgColor = scheme.Color.Text.Magenta
		defaultText = "Generating..."
	case api.StatusExecuting:
		fgColor = scheme.Color.Surface.Info
		defaultText = "Executing..."
	case api.StatusThinking:
		fgColor = scheme.Color.Text.Purple
		defaultText = "Thinking..."
	default:
		fgColor = scheme.Color.Text.Purple
		defaultText = "Thinking..."
	}

	frameGlyph := SpinnerFrames[frame()%len(SpinnerFrames)]
	text := cmp.Or(props.Text, defaultText)

	return trail.Box(trail.BoxProps{},
		trail.Span(trail.SpanProps{
			Style: style.S().Foreground(fgColor).Bold(true),
		}, trail.Text(frameGlyph+" ")),
		trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Secondary),
		}, trail.Text(text)),
	)
})
