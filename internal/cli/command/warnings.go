package command

import (
	"errors"

	"github.com/masterkeysrd/tasksmith/internal/cli/components"
)

func (r *Registry) registerWarningsCommand() {
	r.Register(Command{
		Name:        "warnings",
		Usage:       "/warnings",
		Description: "Inspect currently unresolved warnings",
		Handler:     handleWarnings,
	})
}

func handleWarnings(args []string) error {
	if len(args) > 0 {
		return errors.New("/warnings does not accept arguments; use /warnings to inspect current warnings")
	}
	components.OpenWarnings()
	return nil
}
