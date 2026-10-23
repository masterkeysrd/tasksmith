package components

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
	uihooks "github.com/masterkeysrd/tasksmith/internal/ui/hooks"
)

// StatusContext is the REPL view state shared with status segments. Anything
// that belongs to the session, such as its mode or model, is read by the
// segments themselves through the session hooks.
type StatusContext struct {
	State ui.State
	// Hint overrides the state hint, for example while a picker is open.
	Hint string
}

var statusContext = kitex.CreateContext[StatusContext](StatusContext{})

// StatusProvider supplies the REPL view state to the segments below it.
func StatusProvider(status StatusContext, children ...trail.Node) trail.Node {
	return statusContext.Provider(status, children...)
}

// UseStatus returns the state supplied by the nearest [StatusProvider].
func UseStatus() StatusContext {
	return kitex.UseContext(statusContext)
}

// SegmentProps defines the properties shared by status segments.
type SegmentProps struct {
	// Separator is rendered after the segment, only when the segment itself
	// renders, so a hidden segment never leaves a dangling separator.
	Separator string
}

// HintSegment shows the shortcut hint for the current state, or the override.
var HintSegment = trail.FC("HintSegment", func(props SegmentProps) trail.Node {
	status := UseStatus()
	text := status.Hint
	if text == "" && status.State != ui.StatePicker {
		text = stateHint(status.State)
	}
	return segment(props, statusText(text, ToneMuted, false), text)
})

// ModeSegment shows a colored badge for a non-default permission mode.
var ModeSegment = trail.FC("ModeSegment", func(props SegmentProps) trail.Node {
	mode := uihooks.UsePermissionMode().Mode
	if mode == "" || mode == api.ModeDefault {
		return nil
	}
	text := string(mode)
	return segment(props, statusText(text, modeTone(mode), true), text)
})

// ModelSegment shows "<model> · <thinking>" once the model is known.
var ModelSegment = trail.FC("ModelSegment", func(props SegmentProps) trail.Node {
	info := uihooks.UseModelInfo()
	if !info.Ready {
		return nil
	}
	model := strings.TrimSpace(info.Label)
	if model == "" {
		model = "default"
	}
	thinking := strings.TrimSpace(info.Thinking)
	if thinking == "" {
		thinking = "auto"
	}
	text := fmt.Sprintf("%s · %s", model, thinking)
	return segment(props, statusText(text, ToneMuted, false), text)
})

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
