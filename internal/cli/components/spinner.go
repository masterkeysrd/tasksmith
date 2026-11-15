package components

import (
	"cmp"
	"fmt"
	"image/color"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// SpinnerFrames defines the braille spinner glyph animation cycle.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SpinnerProps defines the properties for the animated thinking spinner.
type SpinnerProps struct {
	Text       string
	Status     api.SessionStatus
	ActiveText string
	// StartedAt is when the turn started; when set, the spinner shows the
	// elapsed time. Input and Output are the tokens used by the turn so far
	// and are shown with arrows when either is non-zero.
	StartedAt time.Time
	Input     int64
	Output    int64
}

// Spinner is a trail functional component rendering an animated braille spinner and status text.
var Spinner = trail.FC("Spinner", func(props SpinnerProps) trail.Node {
	scheme := theme.UseTheme()
	frame, setFrame := kitex.UseState(0)

	kitex.UseInterval(func() {
		setFrame((frame() + 1) % len(SpinnerFrames))
	}, 80*time.Millisecond, nil)

	var (
		fgColor  color.Color
		baseVerb string
	)
	switch props.Status {
	case api.StatusCompacting:
		fgColor = scheme.Color.Agent.Thinking
		baseVerb = "Compacting"
	case api.StatusGenerating:
		fgColor = scheme.Color.Agent.Generating
		baseVerb = "Generating"
	case api.StatusExecuting:
		fgColor = scheme.Color.Agent.Executing
		baseVerb = "Executing"
	case api.StatusThinking:
		fgColor = scheme.Color.Agent.Thinking
		baseVerb = "Thinking"
	default:
		fgColor = scheme.Color.Agent.Thinking
		baseVerb = "Thinking"
	}

	var defaultText string
	if props.ActiveText != "" {
		defaultText = fmt.Sprintf("%s (%s)...", baseVerb, props.ActiveText)
	} else {
		defaultText = baseVerb + "..."
	}

	frameGlyph := SpinnerFrames[frame()%len(SpinnerFrames)]
	text := cmp.Or(props.Text, defaultText)

	children := []trail.Node{
		trail.Span(trail.SpanProps{
			Style: style.S().Foreground(fgColor).Bold(true),
		}, trail.Text(frameGlyph+" ")),
		trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Secondary),
		}, trail.Text(text)),
	}
	if stats := SpinnerStats(props.StartedAt, props.Input, props.Output, time.Now()); stats != "" {
		children = append(children, trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Muted),
		}, trail.Text("  "+stats)))
	}
	return trail.Box(trail.BoxProps{}, children...)
})

// SpinnerStats renders the elapsed time and token counts shown next to the
// spinner, for example "(12s · ↑12.4k ↓1.8k)". input and output are the tokens
// sent and received in the turn so far, summed across its calls; they are not
// the context size. It returns "" when there is nothing to show.
func SpinnerStats(startedAt time.Time, input, output int64, now time.Time) string {
	var parts []string
	if !startedAt.IsZero() {
		parts = append(parts, format.Duration(now.Sub(startedAt).Truncate(time.Second)))
	}
	// Once the turn has started the arrows are always shown, starting at zero,
	// so the counts count up instead of appearing after the first call.
	if !startedAt.IsZero() || input > 0 || output > 0 {
		parts = append(parts, fmt.Sprintf("↑%s ↓%s", format.Tokens(int(input)), format.Tokens(int(output))))
	}
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, " · ") + ")"
}
