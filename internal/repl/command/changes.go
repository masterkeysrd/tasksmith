package command

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (r *Registry) registerChangesCommands() {
	r.Register(Command{
		Name:        "changes",
		Usage:       "/changes",
		Description: "List tracked file changes in the session",
		Handler:     HandleChanges,
	})
	r.Register(Command{
		Name:        "diff",
		Usage:       "/diff <path> <entry_id>",
		Description: "Show unified diff for a tracked file change",
		Handler:     HandleDiff,
	})
	r.Register(Command{
		Name:        "revert",
		Usage:       "/revert [path]",
		Description: "Revert tracked changes for a file or the entire session",
		Handler:     HandleRevert,
	})
}

// HandleChanges emits a directive to list tracked file changes in the session.
func HandleChanges(_ Env, _ []string) (tea.Msg, error) {
	return ListChangesMsg{}, nil
}

// HandleDiff parses diff arguments and emits a ShowDiffMsg.
func HandleDiff(_ Env, args []string) (tea.Msg, error) {
	if len(args) < 2 || strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
		return nil, errors.New("usage: /diff <path> <entry_id>")
	}
	return ShowDiffMsg{
		Path:    strings.TrimSpace(args[0]),
		EntryID: strings.TrimSpace(args[1]),
	}, nil
}

// HandleRevert parses revert arguments and emits a RevertMsg.
func HandleRevert(_ Env, args []string) (tea.Msg, error) {
	var path, entryID string
	if len(args) > 0 {
		path = strings.TrimSpace(args[0])
	}
	if len(args) > 1 {
		entryID = strings.TrimSpace(args[1])
	}
	return RevertMsg{
		Path:    path,
		EntryID: entryID,
	}, nil
}
