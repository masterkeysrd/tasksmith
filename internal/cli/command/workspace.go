package command

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
)

func (r *Registry) registerWorkspaceCommands() {
	r.Register(Command{
		Name:        "workspace",
		Aliases:     []string{"ws"},
		Usage:       "/workspace [reload]",
		Description: "Reload the active workspace resources from disk",
		Handler:     handleWorkspace,
		Complete:    completeWorkspace,
	})
}

func handleWorkspace(args []string) error {
	sub := "reload"
	if len(args) > 0 {
		sub = strings.ToLower(strings.TrimSpace(args[0]))
	}
	switch sub {
	case "", "reload", "refresh":
		triggerWorkspaceReload()
		return nil
	default:
		return fmt.Errorf("unknown workspace subcommand %q (available: reload)", sub)
	}
}

// triggerWorkspaceReload instructs the daemon to reload workspace resources from
// disk and refreshes the store's cached agents, skills, commands, and warnings.
// The reload runs asynchronously so the REPL stays responsive, and the outcome
// is reported back to the transcript scrollback.
func triggerWorkspaceReload() {
	if store.Client() == nil {
		store.AppendError("Error", "Client unavailable.")
		return
	}

	store.AppendCommandOutput("/workspace reload", "Reloading workspace resources...")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := store.ReloadWorkspace(ctx); err != nil {
			store.AppendError("Error", err.Error())
			return
		}
		store.AppendOutput("Workspace reloaded successfully.")
	}()
}

func completeWorkspace(ctx context.Context, _ autocomplete.Request) ([]autocomplete.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []autocomplete.Candidate{
		{Value: "reload", Label: "reload", Description: "Reload workspace resources from disk"},
		{Value: "refresh", Label: "refresh", Description: "Alias for reload"},
	}, nil
}
