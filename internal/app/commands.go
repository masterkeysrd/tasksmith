package app

import (
	"fmt"

	"github.com/masterkeysrd/tasksmith/internal/tui/command"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// InitializeCommands registers all builtin commands for the application.
func (app *Application) InitializeCommands() {
	command.Register("quit", func(ctx command.CommandContext) error {
		app.Quit()
		return nil
	})

	command.Register("startinsert", func(ctx command.CommandContext) error {
		mode.Set(mode.Insert)
		return nil
	})

	command.Register("stopinsert", func(ctx command.CommandContext) error {
		mode.Set(mode.Normal)
		return nil
	})

	command.Register("colorscheme", func(ctx command.CommandContext) error {
		if len(ctx.Args) == 0 {
			return fmt.Errorf("colorscheme: missing theme name")
		}

		name := ctx.Args[0]
		if err := theme.Set(name); err != nil {
			return fmt.Errorf("colorscheme: %w", err)
		}
		return nil
	})
}
