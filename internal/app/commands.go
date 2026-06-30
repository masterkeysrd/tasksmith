package app

import (
	"fmt"

	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
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

	command.Register("theme", func(ctx command.CommandContext) error {
		if len(ctx.Args) == 0 {
			return fmt.Errorf("theme: missing theme name")
		}

		name := ctx.Args[0]
		if err := theme.Set(name); err != nil {
			return fmt.Errorf("theme: %w", err)
		}
		return nil
	})

	command.Register("lspinfo", func(ctx command.CommandContext) error {
		active.SetModal("lspinfo")
		return nil
	})

	command.Register("mcp", func(ctx command.CommandContext) error {
		active.SetModal("mcp")
		return nil
	})

	command.Register("model", func(ctx command.CommandContext) error {
		active.SetModal("modelpicker")
		return nil
	})

	command.Register("lsp", func(ctx command.CommandContext) error {
		if len(ctx.Args) == 0 {
			return fmt.Errorf("lsp: missing subcommand (try 'info' or 'restart')")
		}
		switch ctx.Args[0] {
		case "info":
			active.SetModal("lspinfo")
			return nil
		case "restart":
			if app.lspManager != nil {
				go func() {
					_ = app.lspManager.RestartClient(ctx.Ctx, app.opts.CWD)
				}()
			}
			return nil
		default:
			return fmt.Errorf("lsp: unknown subcommand %q", ctx.Args[0])
		}
	})

	command.Register("perm", func(ctx command.CommandContext) error {
		if len(ctx.Args) == 0 {
			active.SetModal("permissionpicker")
			return nil
		}

		modeStr := ctx.Args[0]
		var mode permissions.PermissionMode
		switch modeStr {
		case "auto":
			mode = permissions.ModeAuto
		case "default":
			mode = permissions.ModeDefault
		case "strict":
			mode = permissions.ModeStrict
		default:
			return fmt.Errorf("perm: unknown mode %q (try 'auto', 'default', or 'strict')", modeStr)
		}

		sessionID := active.GetSessionID()
		_, err := app.api.SetPermissionMode(ctx.Ctx, api.SetPermissionModeRequest{
			SessionID: sessionID,
			Mode:      mode,
			Scope:     permissions.ScopeSession,
		})
		if err != nil {
			return fmt.Errorf("perm: failed to set mode: %w", err)
		}

		if active.InvalidateSessionState != nil {
			active.InvalidateSessionState(sessionID)
		}
		return nil
	})
}
