package command

import (
	"context"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func (r *Registry) registerSetupCommands() {
	r.Register(Command{
		Name:        "setup",
		Usage:       "/setup",
		Description: "Configure workspace trust, providers, permissions, and tools",
		Handler:     handleSetup,
	})
}

func handleSetup(args []string) error {
	c := store.Client()
	if c == nil {
		store.AppendError("Error", "Client unavailable.")
		return nil
	}

	wsDir := store.GetApp().Workspace
	if wsDir == "" {
		wsDir = c.CWD()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := c.GetWorkspaceStatus(ctx)
	if err != nil {
		store.AppendError("Error", "Failed to retrieve workspace status: "+err.Error())
		return nil
	}

	store.StartOnboarding(wsDir, *status, true)
	return nil
}
