package command

import (
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func (r *Registry) registerCompactCommand() {
	r.Register(Command{
		Name:        "compact",
		Usage:       "/compact [focus]",
		Description: "Compact conversation history to free context",
		Handler:     handleCompact,
	})
}

func handleCompact(args []string) error {
	focus := strings.TrimSpace(strings.Join(args, " "))
	store.SendCompact(focus)
	return nil
}
