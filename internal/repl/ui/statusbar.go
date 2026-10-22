package ui

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// State represents the interactive lifecycle phase of the REPL.
type State int

const (
	// StateIdle indicates the REPL is waiting for user prompt or slash command input.
	StateIdle State = iota
	// StateStreaming indicates the REPL is streaming an active assistant turn.
	StateStreaming
	// StateApproval indicates the REPL is paused awaiting user resolution of a tool permission request.
	StateApproval
	// StatePicker indicates an interactive command picker modal is active.
	StatePicker
)

// String returns the human-readable name of the [State].
func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateStreaming:
		return "streaming"
	case StateApproval:
		return "approval"
	case StatePicker:
		return "picker"
	default:
		return "unknown"
	}
}

// StatusBarProps defines the properties for the REPL status footer.
type StatusBarProps struct {
	State    State
	Mode     api.PermissionMode
	Model    string
	Thinking string
	Hint     string // Custom hint override, e.g. from active picker
}

func stateHint(state State) string {
	switch state {
	case StateStreaming:
		return "streaming • esc to cancel"
	case StatePicker:
		return "↑/↓ navigate · enter select · esc cancel"
	case StateApproval:
		return "y allow once · a allow session · n deny · r reject with feedback"
	default:
		return "shift+tab cycle mode • /help commands"
	}
}

// StatusBar is a trail functional component rendering the two-column REPL status bar:
// left: shortcut hint
// right: <mode> · <Model> · <Thinking> (or <Model> · <Thinking> when mode is default or empty)
var StatusBar = trail.FC("StatusBar", func(props StatusBarProps) trail.Node {
	leftText := props.Hint
	if leftText == "" {
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
