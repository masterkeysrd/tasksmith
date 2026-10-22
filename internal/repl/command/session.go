package command

import (
	"fmt"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
)

func (r *Registry) registerSessionCommands() {
	r.Register(Command{
		Name:        "new",
		Usage:       "/new [model]",
		Description: "Create and switch to a new session",
		Handler:     HandleNew,
	})
	r.Register(Command{
		Name:        "sessions",
		Aliases:     []string{"session", "ls"},
		Usage:       "/sessions [session_id]",
		Description: "Browse, switch, or manage workspace sessions",
		Handler:     HandleSessions,
	})
}

// HandleNew switches the REPL to a fresh session, optionally with a model override.
func HandleNew(env Env, args []string) (Event, error) {
	modelOverride := strings.TrimSpace(strings.Join(args, " "))
	if modelOverride == "" {
		modelOverride = strings.TrimSpace(env.DefaultModel)
	}
	return NewSessionMsg{Model: modelOverride}, nil
}

// HandleSessions browses or switches sessions in the active workspace.
func HandleSessions(env Env, args []string) (Event, error) {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		return SwitchSessionMsg{SessionID: strings.TrimSpace(args[0])}, nil
	}

	if env.ListSessions == nil {
		return PrintMsg{Text: "No sessions found."}, nil
	}
	sessions, err := env.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("error listing sessions: %w", err)
	}
	if len(sessions) == 0 {
		return PrintMsg{Text: "No sessions found."}, nil
	}

	var activeID string
	if env.Session != nil {
		activeID = env.Session.ID
	}
	return OpenPickerMsg{Picker: NewSessionPicker(sessions, activeID)}, nil
}

// NewSessionPicker constructs an interactive picker for selecting or deleting sessions.
func NewSessionPicker(sessions []api.Session, activeID string) ui.PickerProps {
	opts := make([]ui.Option, len(sessions))
	for i, s := range sessions {
		title := strings.TrimSpace(s.Title)
		if title == "" {
			title = "Untitled"
		}
		model := strings.TrimSpace(s.Model)
		if model == "" {
			model = "-"
		}
		relTime := humanize.Time(s.UpdatedAt)
		opts[i] = ui.Option{
			Value:   s.ID,
			Label:   title,
			Columns: []string{title, s.ID, model, relTime},
			Current: activeID != "" && s.ID == activeID,
		}
	}

	picker := ui.NewPicker("Select session:", opts, func(opt ui.Option) any {
		return SwitchSessionMsg{SessionID: opt.Value}
	})
	picker.Filterable = true
	picker.StatusBar = "↑/↓ to select • enter to switch • f2 to rename • f4 to delete • esc to cancel"

	picker.InputActions = map[string]ui.InputAction{
		"f2": {
			Prompt: "Rename session: ",
			InitialValue: func(opt ui.Option) string {
				return opt.Label
			},
			OnSubmit: func(opt ui.Option, val string) any {
				return RenameSessionMsg{SessionID: opt.Value, Title: val}
			},
		},
	}
	picker.ConfirmActions = map[string]ui.ConfirmAction{
		"f4": {
			Prompt: func(opt ui.Option) string {
				return fmt.Sprintf("Delete session %q?", opt.Label)
			},
			OnConfirm: func(opt ui.Option) any {
				return DeleteSessionMsg{SessionID: opt.Value}
			},
		},
	}
	return picker
}
