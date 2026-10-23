package components

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
)

// StatusLineProps defines the properties for the REPL status line.
type StatusLineProps struct {
	State    ui.State
	Mode     api.PermissionMode
	Model    string
	Thinking string
	Hint     string // Custom hint override, e.g. from active picker
}

func stateHint(state ui.State) string {
	switch state {
	case ui.StateStreaming:
		return "streaming • esc to cancel"
	case ui.StatePicker:
		return ""
	case ui.StateApproval:
		return "y allow once · a allow session · n deny · r reject with feedback"
	default:
		return "shift+tab cycle mode • /help commands"
	}
}

// StatusLine is a trail functional component rendering the two-column REPL status bar:
// left: shortcut hint
// right: <mode> · <Model> · <Thinking> (or <Model> · <Thinking> when mode is default or empty)
var StatusLine = trail.FC("StatusLine", func(props StatusLineProps) trail.Node {
	leftText := props.Hint
	if leftText == "" && props.State != ui.StatePicker {
		leftText = stateHint(props.State)
	}

	model := cmp.Or(strings.TrimSpace(props.Model), "default")
	thinking := cmp.Or(strings.TrimSpace(props.Thinking), "auto")
	mode := cmp.Or(props.Mode, api.ModeDefault)

	var rightChildren []trail.Node
	if mode != api.ModeDefault && mode != "" {
		rightChildren = append(rightChildren,
			trail.Span(trail.SpanProps{
				Style: theme.ModeBadgeStyle(mode),
			}, trail.Text(string(mode))),
			trail.Span(trail.SpanProps{
				Style: theme.StatusBarStyle,
			}, trail.Text(" · ")),
		)
	}
	rightChildren = append(rightChildren,
		trail.Span(trail.SpanProps{
			Style: theme.StatusBarStyle,
		}, trail.Text(fmt.Sprintf("%s · %s", model, thinking))),
	)

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			Width(style.Percent(100)),
	},
		// Left shortcut hint
		trail.Span(trail.SpanProps{
			Style: theme.StatusBarStyle,
		}, trail.Text(leftText)),
		trail.Spacer(),
		// Right mode · model · thinking indicator (inline spans inside a single box)
		trail.Box(trail.BoxProps{}, rightChildren...),
	)
})
