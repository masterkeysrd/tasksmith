package ui

import (
	"fmt"
	"strings"

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
	State   State
	Session *api.Session
	Mode    api.PermissionMode
}

// Render formats the status bar footer string.
func (s StatusBar) Render() string {
	mode := s.Mode
	if mode == "" {
		mode = api.ModeDefault
	}

	modeBadge := theme.ModeBadgeStyle(mode).Render(fmt.Sprintf("mode: %s", mode))

	var sessionInfo string
	if s.Session != nil {
		parts := make([]string, 0, 3)
		if s.Session.ID != "" {
			parts = append(parts, fmt.Sprintf("session: %s", s.Session.ID))
		}
		if s.Session.Agent != "" {
			parts = append(parts, fmt.Sprintf("agent: %s", s.Session.Agent))
		}
		if s.Session.Model != "" {
			parts = append(parts, fmt.Sprintf("model: %s", s.Session.Model))
		}
		sessionInfo = strings.Join(parts, " • ")
	}

	hint := stateHint(s.State)
	segments := []string{modeBadge}
	if sessionInfo != "" {
		segments = append(segments, theme.StatusBarStyle.Render(sessionInfo))
	}
	segments = append(segments, theme.StatusBarStyle.Render(hint))

	return strings.Join(segments, theme.StatusBarStyle.Render(" │ "))
}

func stateHint(state State) string {
	switch state {
	case StateStreaming:
		return "streaming • esc to cancel"
	default:
		return "shift+tab cycle mode • /help commands"
	}
}

// FormatStatusBar renders the contextual status footer for the active REPL state.
func FormatStatusBar(state State, sess *api.Session, mode api.PermissionMode) string {
	return StatusBar{State: state, Session: sess, Mode: mode}.Render()
}
