package command

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
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

// HandleChanges lists all file modifications tracked by the session change journal.
func HandleChanges(ctx context.Context, env *Env, _ []string) (Result, error) {
	if env == nil || env.Session == nil || strings.TrimSpace(env.Session.ID) == "" {
		return Result{Output: "No tracked file changes."}, nil
	}
	if err := requireEnvSession(env, true); err != nil {
		return Result{}, err
	}

	changes, err := env.Client.Changes(ctx, env.Session.ID)
	if err != nil {
		return Result{}, fmt.Errorf("error listing file changes: %w", err)
	}
	if len(changes) == 0 {
		return Result{Output: "No tracked file changes."}, nil
	}

	rows := make([]string, len(changes))
	for i, c := range changes {
		rows[i] = fmt.Sprintf("%s\t%s\t%s\t+%d/-%d", c.ID, c.Kind, c.Path, c.Additions, c.Deletions)
	}
	return Result{Output: strings.Join(rows, "\n")}, nil
}

// HandleDiff fetches and formats the unified diff for a specific change entry.
func HandleDiff(ctx context.Context, env *Env, args []string) (Result, error) {
	if err := requireEnvSession(env, true); err != nil {
		return Result{}, err
	}
	if len(args) < 2 || strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
		return Result{}, errors.New("usage: /diff <path> <entry_id>")
	}

	diffText, err := env.Client.DiffChange(ctx, env.Session.ID, strings.TrimSpace(args[0]), strings.TrimSpace(args[1]))
	if err != nil {
		return Result{}, fmt.Errorf("error fetching diff: %w", err)
	}
	if strings.TrimSpace(diffText) == "" {
		return Result{Output: "No diff changes."}, nil
	}
	return Result{Output: ui.FormatDiff(strings.TrimRight(diffText, "\n"))}, nil
}

// HandleRevert restores modified files to their previous states.
func HandleRevert(ctx context.Context, env *Env, args []string) (Result, error) {
	if err := requireEnvSession(env, true); err != nil {
		return Result{}, err
	}

	var path string
	if len(args) > 0 {
		path = strings.TrimSpace(args[0])
	}
	if err := env.Client.Revert(ctx, env.Session.ID, api.RevertRequest{
		Path:  path,
		Force: true,
	}); err != nil {
		return Result{}, fmt.Errorf("error reverting changes: %w", err)
	}

	if path == "" {
		return Result{Output: "Reverted all session file changes."}, nil
	}
	return Result{Output: fmt.Sprintf("Reverted %s.", path)}, nil
}
