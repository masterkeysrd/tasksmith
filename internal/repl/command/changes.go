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

// HandleChanges triggers listing tracked file changes in the session.
func HandleChanges(env Env, _ []string) error {
	if env.ListChanges != nil {
		env.ListChanges()
	}
	return nil
}

// HandleDiff parses diff arguments and triggers showing a diff.
func HandleDiff(env Env, args []string) error {
	if len(args) < 2 || strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
		return errors.New("usage: /diff <path> <entry_id>")
	}
	if env.DiffChange != nil {
		env.DiffChange(strings.TrimSpace(args[0]), strings.TrimSpace(args[1]))
	}
	return nil
}

// HandleRevert parses revert arguments and triggers reverting tracked file changes.
func HandleRevert(env Env, args []string) error {
	var path, entryID string
	if len(args) > 0 {
		path = strings.TrimSpace(args[0])
	}
	if len(args) > 1 {
		entryID = strings.TrimSpace(args[1])
	}
	if env.RevertChanges != nil {
		env.RevertChanges(path, entryID)
	}
	return nil
}
