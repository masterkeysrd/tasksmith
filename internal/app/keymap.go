package app

import (
	"context"

	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	"github.com/masterkeysrd/tasksmith/internal/tui/command"
	"github.com/masterkeysrd/tasksmith/internal/tui/keymap"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
	"github.com/masterkeysrd/tasksmith/internal/tui/views/analytics"
)

// InitializeKeymap registers the default application key bindings.
func (app *Application) InitializeKeymap() {
	// Global Toggle for Token Analytics View
	keymap.Set([]mode.Mode{mode.Normal}, "<C-t>", func(ctx context.Context) {
		if active.GetScreen() == "analytics" {
			active.SetScreen("chat")
		} else {
			active.SetScreen("analytics")
		}
	}, keymap.Description("Toggle Token Analytics View"))

	keymap.Set([]mode.Mode{mode.Normal}, "<C-l>", func(ctx context.Context) {
		active.SetModal("lspinfo")
	}, keymap.Description("Open LSP Panel"))

	keymap.Set([]mode.Mode{mode.Normal}, "<C-p>", func(ctx context.Context) {
		active.SetModal("mcp")
	}, keymap.Description("Open MCP Panel"))

	keymap.Set([]mode.Mode{mode.Normal}, "<C-m>", func(ctx context.Context) {
		active.SetModal("modelpicker")
	}, keymap.Description("Open Model Picker"))

	// Normal Mode Keybindings
	keymap.Set([]mode.Mode{mode.Normal}, "q", func(ctx context.Context) {
		if active.GetScreen() == "analytics" {
			active.SetScreen("chat")
		} else {
			_ = command.Execute(ctx, "quit")
		}
	}, keymap.Description("Quit or Close Analytics"))

	keymap.Set([]mode.Mode{mode.Normal}, "<Esc>", func(ctx context.Context) {
		if active.GetScreen() == "analytics" {
			active.SetScreen("chat")
		}
	}, keymap.Description("Close Analytics"))

	keymap.Set([]mode.Mode{mode.Normal}, "i", func(ctx context.Context) {
		if active.GetScreen() != "analytics" {
			_ = command.Execute(ctx, "startinsert")
		}
	}, keymap.Description("Enter insert mode"))

	keymap.Set([]mode.Mode{mode.Normal}, ":", func(ctx context.Context) {
		if active.GetScreen() != "analytics" {
			mode.Set(mode.Command)
		}
	}, keymap.Description("Enter command mode"))

	// Analytics-specific keybindings (only active when on analytics screen)
	keymap.Set([]mode.Mode{mode.Normal}, "1", func(ctx context.Context) {
		analytics.SetTimeframe("today")
	}, keymap.Description("Analytics: Timeframe Today"), keymap.Screen("analytics"))

	keymap.Set([]mode.Mode{mode.Normal}, "2", func(ctx context.Context) {
		analytics.SetTimeframe("7days")
	}, keymap.Description("Analytics: Timeframe 7 Days"), keymap.Screen("analytics"))

	keymap.Set([]mode.Mode{mode.Normal}, "3", func(ctx context.Context) {
		analytics.SetTimeframe("30days")
	}, keymap.Description("Analytics: Timeframe This Month"), keymap.Screen("analytics"))

	keymap.Set([]mode.Mode{mode.Normal}, "p", func(ctx context.Context) {
		analytics.CycleProviderFilter()
	}, keymap.Description("Analytics: Cycle Provider Filter"), keymap.Screen("analytics"))

	keymap.Set([]mode.Mode{mode.Normal}, "t", func(ctx context.Context) {
		analytics.ToggleMetricUnit()
	}, keymap.Description("Analytics: Toggle Metric Unit"), keymap.Screen("analytics"))

	keymap.Set([]mode.Mode{mode.Normal}, "<Tab>", func(ctx context.Context) {
		analytics.CycleActiveTab()
	}, keymap.Description("Analytics: Cycle Active Tab"), keymap.Screen("analytics"))

	// Insert Mode
	keymap.Set([]mode.Mode{mode.Insert}, "<Esc>", func(ctx context.Context) {
		_ = command.Execute(ctx, "stopinsert")
	}, keymap.Description("Exit insert mode"))

	// Command Mode
	keymap.Set([]mode.Mode{mode.Command}, "<Esc>", func(ctx context.Context) {
		mode.Set(mode.Normal)
	}, keymap.Description("Exit command mode"))

	// Permission Mode cycling keybindings
	cyclePermissionMode := func(ctx context.Context) {
		sessionID := active.GetSessionID()
		if sessionID == "" {
			return
		}

		state, err := app.api.GetSessionState(ctx, api.GetSessionStateRequest{SessionID: sessionID})
		if err != nil {
			log.Error("failed to get session state to cycle permission mode", log.Err(err))
			return
		}

		currMode := state.PermissionMode
		if currMode == "" {
			currMode = permissions.ModeDefault
		}

		var nextMode permissions.PermissionMode
		switch currMode {
		case permissions.ModeDefault:
			nextMode = permissions.ModeStrict
		case permissions.ModeStrict:
			nextMode = permissions.ModeAuto
		case permissions.ModeAuto:
			fallthrough
		default:
			nextMode = permissions.ModeDefault
		}

		_, err = app.api.SetPermissionMode(ctx, api.SetPermissionModeRequest{
			SessionID: sessionID,
			Mode:      nextMode,
			Scope:     permissions.ScopeSession,
		})
		if err != nil {
			log.Error("failed to set permission mode during cycle", log.Err(err))
			return
		}

		if active.InvalidateSessionState != nil {
			active.InvalidateSessionState(sessionID)
		}
	}

	keymap.Set([]mode.Mode{mode.Normal}, "\\pm", cyclePermissionMode, keymap.Description("Cycle Permission Mode"))
	keymap.Set([]mode.Mode{mode.Normal}, " pm", cyclePermissionMode, keymap.Description("Cycle Permission Mode"))
	keymap.Set([]mode.Mode{mode.Normal}, "<Space>pm", cyclePermissionMode, keymap.Description("Cycle Permission Mode"))
}
