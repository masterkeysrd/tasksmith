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
func HandleNew(env Env, args []string) error {
	modelOverride := strings.TrimSpace(strings.Join(args, " "))
	if modelOverride == "" {
		modelOverride = strings.TrimSpace(env.DefaultModel)
	}
	if env.NewSession != nil {
		env.NewSession(modelOverride)
	}
	return nil
}

// HandleSessions browses or switches sessions in the active workspace.
func HandleSessions(env Env, args []string) error {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		if env.SwitchSession != nil {
			env.SwitchSession(strings.TrimSpace(args[0]))
		}
		return nil
	}

	if env.ListSessions == nil {
		if env.AppendNode != nil {
			env.AppendNode(ui.Notice(ui.NoticeProps{Text: "No sessions found."}))
		}
		return nil
	}
	sessions, err := env.ListSessions()
	if err != nil {
		return fmt.Errorf("error listing sessions: %w", err)
	}
	if len(sessions) == 0 {
		if env.AppendNode != nil {
			env.AppendNode(ui.Notice(ui.NoticeProps{Text: "No sessions found."}))
		}
		return nil
	}

	var activeID string
	if env.Session != nil {
		activeID = env.Session.ID
	}
	if env.OpenPicker != nil {
		env.OpenPicker(NewSessionPicker(env, sessions, activeID))
	}
	return nil
}

// NewSessionPicker constructs an interactive picker for selecting or deleting sessions.
func NewSessionPicker(env Env, sessions []api.Session, activeID string) ui.PickerProps {
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

	picker := ui.NewPicker("Sessions", opts, func(opt ui.Option) {
		if env.SwitchSession != nil {
			env.SwitchSession(opt.Value)
		}
	})
	picker.Filterable = true
	picker.Footer = "↑/↓ to select • enter to switch • f2 to rename • f4 to delete • esc to cancel"

	picker.InputActions = map[string]ui.InputAction{
		"f2": {
			Prompt: "Rename session:",
			InitialValue: func(opt ui.Option) string {
				return opt.Label
			},
			OnSubmit: func(opt ui.Option, val string) {
				if env.RenameSession != nil {
					env.RenameSession(opt.Value, val)
				}
			},
		},
	}
	picker.ConfirmActions = map[string]ui.ConfirmAction{
		"f4": {
			Prompt: func(opt ui.Option) string {
				return fmt.Sprintf("Delete session %q?", opt.Label)
			},
			OnConfirm: func(opt ui.Option) {
				if env.DeleteSession != nil {
					env.DeleteSession(opt.Value)
				}
			},
		},
	}
	return picker
}
