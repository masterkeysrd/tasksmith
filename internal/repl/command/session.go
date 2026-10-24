package command

import (
	"fmt"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
	"github.com/masterkeysrd/tasksmith/internal/ui/components"
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

	if env.BrowseSessions == nil {
		if env.AppendOutput != nil {
			env.AppendOutput(ui.CommandOutputProps{
				Command: "/sessions",
				Output:  "No sessions found.",
			})
		}
		return nil
	}
	env.BrowseSessions()
	return nil
}

// NewSessionPicker constructs an interactive picker for selecting or deleting sessions.
func NewSessionPicker(env Env, sessions []api.Session, activeID string) ui.PickerProps {
	opts := make([]components.Option, len(sessions))
	for i, s := range sessions {
		title := strings.TrimSpace(s.Title)
		if title == "" {
			title = "Untitled"
		}
		model := strings.TrimSpace(s.Config.Model)
		if model == "" {
			model = "-"
		}
		relTime := humanize.Time(s.UpdatedAt)
		opts[i] = components.Option{
			Value:   s.ID,
			Label:   title,
			Cells:   []string{title, model, relTime},
			Current: activeID != "" && s.ID == activeID,
		}
	}

	picker := ui.NewPicker("Sessions", opts, func(opt components.Option) {
		if env.SwitchSession != nil {
			env.SwitchSession(opt.Value)
		}
	})
	picker.Columns = []components.Column{
		{Width: 0},
		{Width: 24, Align: style.TextAlignLeft},
		{Width: 26, Align: style.TextAlignRight},
	}
	picker.Filterable = true
	picker.OnCancel = func() {
		if env.AppendOutput != nil {
			env.AppendOutput(ui.CommandOutputProps{
				Command: "/sessions",
				Output:  "Exited /sessions command",
			})
		}
	}
	picker.Footer = "↑/↓ to select • enter to switch • f2 to rename • f4 to delete • esc to cancel"

	picker.InputActions = map[string]ui.InputAction{
		"f2": {
			Prompt: "Rename session:",
			InitialValue: func(opt components.Option) string {
				return opt.Label
			},
			OnSubmit: func(opt components.Option, val string) {
				if env.RenameSession != nil {
					env.RenameSession(opt.Value, val)
				}
			},
		},
	}
	picker.ConfirmActions = map[string]ui.ConfirmAction{
		"f4": {
			Prompt: func(opt components.Option) string {
				return fmt.Sprintf("Delete session %q?", opt.Label)
			},
			OnConfirm: func(opt components.Option) {
				if env.DeleteSession != nil {
					env.DeleteSession(opt.Value)
				}
			},
		},
	}
	return picker
}
