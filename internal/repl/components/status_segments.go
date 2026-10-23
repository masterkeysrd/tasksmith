package components

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
)

// StatusContext is the read-only REPL status shared with status segments.
type StatusContext struct {
	State    ui.State
	Mode     api.PermissionMode
	Model    string
	Thinking string
	// Hint overrides the state hint, for example while a picker is open.
	Hint string
}

var statusContext = kitex.CreateContext[StatusContext](StatusContext{})

// StatusProvider supplies the REPL status to the segments below it.
func StatusProvider(status StatusContext, children ...trail.Node) trail.Node {
	return statusContext.Provider(status, children...)
}

// UseStatus returns the status supplied by the nearest [StatusProvider].
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
	status := UseStatus()
	if status.Mode == "" || status.Mode == api.ModeDefault {
		return nil
	}
	text := string(status.Mode)
	return segment(props, statusText(text, modeTone(status.Mode), true), text)
})

// ModelSegment shows "<model> · <thinking>", with defaults for blank values.
var ModelSegment = trail.FC("ModelSegment", func(props SegmentProps) trail.Node {
	status := UseStatus()
	model := strings.TrimSpace(status.Model)
	if model == "" {
		model = "default"
	}
	thinking := strings.TrimSpace(status.Thinking)
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
