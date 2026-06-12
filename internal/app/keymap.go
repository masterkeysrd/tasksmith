package app

import (
	"context"

	"github.com/masterkeysrd/tasksmith/internal/tui/command"
	"github.com/masterkeysrd/tasksmith/internal/tui/keymap"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
)

// InitializeKeymap registers the default application key bindings.
func (app *Application) InitializeKeymap() {
	// Normal Mode
	keymap.Set([]mode.Mode{mode.Normal}, "q", func(ctx context.Context) {
		_ = command.Execute(ctx, "quit")
	}, keymap.Description("Quit application"))

	keymap.Set([]mode.Mode{mode.Normal}, "i", func(ctx context.Context) {
		_ = command.Execute(ctx, "startinsert")
	}, keymap.Description("Enter insert mode"))

	keymap.Set([]mode.Mode{mode.Normal}, ":", func(ctx context.Context) {
		mode.Set(mode.Command)
	}, keymap.Description("Enter command mode"))

	// Insert Mode
	keymap.Set([]mode.Mode{mode.Insert}, "<Esc>", func(ctx context.Context) {
		_ = command.Execute(ctx, "stopinsert")
	}, keymap.Description("Exit insert mode"))

	// Command Mode
	keymap.Set([]mode.Mode{mode.Command}, "<Esc>", func(ctx context.Context) {
		mode.Set(mode.Normal)
	}, keymap.Description("Exit command mode"))
}
