package command

import (
	"errors"
	"strings"
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
func HandleChanges(_ Env, _ []string) (Event, error) {
	return ListChangesMsg{}, nil
}

// HandleDiff parses diff arguments and emits a DiffChangeMsg.
func HandleDiff(_ Env, args []string) (Event, error) {
	if len(args) < 2 || strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
		return nil, errors.New("usage: /diff <path> <entry_id>")
	}
	return DiffChangeMsg{
		Path:    strings.TrimSpace(args[0]),
		EntryID: strings.TrimSpace(args[1]),
	}, nil
}

// HandleRevert parses revert arguments and emits a RevertChangesMsg.
func HandleRevert(_ Env, args []string) (Event, error) {
	var path, entryID string
	if len(args) > 0 {
		path = strings.TrimSpace(args[0])
	}
	if len(args) > 1 {
		entryID = strings.TrimSpace(args[1])
	}
	return RevertChangesMsg{
		Path:    path,
		EntryID: entryID,
	}, nil
}
