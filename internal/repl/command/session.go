package command

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
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
		Aliases:     []string{"ls"},
		Usage:       "/sessions",
		Description: "List sessions in the current workspace",
		Handler:     HandleSessions,
	})
	r.Register(Command{
		Name:        "switch",
		Usage:       "/switch <session_id>",
		Description: "Switch to an existing session by ID",
		Handler:     HandleSwitch,
	})
	r.Register(Command{
		Name:        "history",
		Usage:       "/history",
		Description: "Show conversation history for the active session",
		Handler:     HandleHistory,
	})
}

// HandleNew switches the REPL to a fresh session, optionally with a model override.
func HandleNew(env Env, args []string) (tea.Msg, error) {
	modelOverride := strings.TrimSpace(strings.Join(args, " "))
	if modelOverride == "" {
		modelOverride = strings.TrimSpace(env.DefaultModel)
	}
	return NewSessionMsg{Model: modelOverride}, nil
}

// HandleSessions lists all sessions associated with the active workspace.
func HandleSessions(_ Env, _ []string) (tea.Msg, error) {
	return ListSessionsMsg{}, nil
}

// HandleSwitch changes the active session to targetID.
func HandleSwitch(_ Env, args []string) (tea.Msg, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return nil, errors.New("usage: /switch <session_id>")
	}
	return SwitchSessionMsg{SessionID: strings.TrimSpace(args[0])}, nil
}

// HandleHistory renders the message history of the current session.
func HandleHistory(_ Env, _ []string) (tea.Msg, error) {
	return ShowHistoryMsg{}, nil
}
