package ui

import (
	"cmp"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// State represents the interactive lifecycle phase of the REPL model.
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

// StatusBar holds contextual data rendered in the REPL status footer.
type StatusBar struct {
	State    State
	Mode     api.PermissionMode
	Model    string
	Thinking string
	Width    int
}

// Render formats the status bar footer string using a split layout.
func (s StatusBar) Render() string {
	left := theme.StatusBarStyle.Render(stateHint(s.State))

	model := cmp.Or(strings.TrimSpace(s.Model), "default")
	thinking := cmp.Or(strings.TrimSpace(s.Thinking), "auto")
	modelPart := theme.StatusBarStyle.Render(fmt.Sprintf("%s · %s", model, thinking))
	mode := cmp.Or(s.Mode, api.ModeDefault)

	var right string
	if mode == api.ModeDefault {
		right = modelPart
	} else {
		right = theme.ModeBadgeStyle(mode).Render(string(mode)) + theme.StatusBarStyle.Render(" · ") + modelPart
	}

	width := cmp.Or(s.Width, 80)
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	gap := max(width-leftWidth-rightWidth, 2)

	return left + strings.Repeat(" ", gap) + right
}

func stateHint(state State) string {
	switch state {
	case StateStreaming:
		return "streaming • esc to cancel"
	default:
		return "shift+tab cycle mode • /help commands"
	}
}

// FormatStatusBar renders the contextual status footer for the given [StatusBar].
func FormatStatusBar(s StatusBar) string {
	return s.Render()
}
