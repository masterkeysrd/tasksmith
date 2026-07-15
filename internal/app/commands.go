package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/agent/model"
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
	}, command.Complete(func(ctx context.Context, args []string) []command.CompletionItem {
		if len(args) > 1 {
			return nil
		}
		names := theme.List()
		var items []command.CompletionItem
		for _, name := range names {
			items = append(items, command.CompletionItem{
				Label:    name,
				Sublabel: "Theme Preset",
				Badge:    "THEME",
			})
		}
		return items
	}))

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

	command.Register("thinking", func(ctx command.CommandContext) error {
		sessionID := active.GetSessionID()
		if sessionID == "" {
			return fmt.Errorf("thinking: no active session")
		}

		if len(ctx.Args) == 0 {
			return fmt.Errorf("thinking: missing subcommand/argument (try 'toggle', 'effort', 'budget', or 'adaptive')")
		}

		// Fetch the session configuration from backend
		sessionsResp, err := app.api.ListSessions(ctx.Ctx, api.ListSessionsRequest{})
		if err != nil {
			return fmt.Errorf("thinking: failed to list sessions: %w", err)
		}
		var currentSession api.Session
		found := false
		for _, s := range sessionsResp.Sessions {
			if s.ID == sessionID {
				currentSession = s
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("thinking: session %q not found", sessionID)
		}

		newSettings := currentSession.Settings
		if newSettings.Thinking == nil {
			newSettings.Thinking = &model.SessionThinkingSetting{}
		}

		subcommand := strings.ToLower(ctx.Args[0])
		switch subcommand {
		case "toggle":
			var nextEnabled bool
			if newSettings.Thinking.Enabled == nil {
				nextEnabled = true
			} else {
				nextEnabled = !*newSettings.Thinking.Enabled
			}
			newSettings.Thinking.Enabled = &nextEnabled

			_, err = app.api.ConfigureSession(ctx.Ctx, api.ConfigureSessionRequest{
				SessionID:    sessionID,
				ProviderName: currentSession.Settings.ProviderName,
				ModelName:    currentSession.Settings.ModelName,
				AgentName:    currentSession.Settings.AgentName,
				Settings:     &newSettings,
			})
			if err != nil {
				return err
			}

			if active.InvalidateSessionState != nil {
				active.InvalidateSessionState(sessionID)
			}

			if nextEnabled {
				active.SetStatusMessage("Thinking: ENABLED")
			} else {
				active.SetStatusMessage("Thinking: DISABLED")
			}
			return nil

		case "effort":
			var activeModel api.Model
			foundModel := false
			providersResp, err := app.api.ListProviders(ctx.Ctx, api.ListProvidersRequest{})
			if err == nil && providersResp != nil {
				for _, p := range providersResp.Providers {
					if p.Name == currentSession.Settings.ProviderName {
						for _, m := range p.Models {
							if m.ID == currentSession.Settings.ModelName {
								activeModel = m
								foundModel = true
								break
							}
						}
						break
					}
				}
			}

			hasEffort := false
			if foundModel && activeModel.Capabilities.Reasoning {
				for _, opt := range activeModel.Capabilities.ReasoningOptions {
					if opt.Type == "effort" {
						hasEffort = true
						break
					}
				}
			}
			if !hasEffort {
				modelID := "active model"
				if foundModel {
					modelID = activeModel.ID
				}
				return fmt.Errorf("thinking effort: active model %q does not support reasoning effort configuration", modelID)
			}

			// Open the effort picker modal
			active.SetModal("effortpicker")
			return nil

		case "budget":
			if len(ctx.Args) < 2 {
				return fmt.Errorf("thinking budget: missing token budget value (try ':thinking budget 16000' or ':thinking budget off')")
			}
			valStr := strings.ToLower(ctx.Args[1])
			if valStr == "off" || valStr == "none" || valStr == "0" {
				newSettings.Thinking.Budget = nil
			} else {
				budgetVal, err := strconv.Atoi(valStr)
				if err != nil {
					return fmt.Errorf("thinking budget: invalid budget value %q (must be an integer or 'off')", ctx.Args[1])
				}
				newSettings.Thinking.Budget = &budgetVal
				// Make sure thinking is enabled
				enabledVal := true
				newSettings.Thinking.Enabled = &enabledVal
			}

			_, err = app.api.ConfigureSession(ctx.Ctx, api.ConfigureSessionRequest{
				SessionID:    sessionID,
				ProviderName: currentSession.Settings.ProviderName,
				ModelName:    currentSession.Settings.ModelName,
				AgentName:    currentSession.Settings.AgentName,
				Settings:     &newSettings,
			})
			if err != nil {
				return err
			}

			if active.InvalidateSessionState != nil {
				active.InvalidateSessionState(sessionID)
			}

			if newSettings.Thinking.Budget == nil {
				active.SetStatusMessage("Thinking budget: OFF")
			} else {
				active.SetStatusMessage(fmt.Sprintf("Thinking budget set to: %d tokens", *newSettings.Thinking.Budget))
			}
			return nil

		case "adaptive":
			var nextAdaptive bool
			if len(ctx.Args) >= 2 {
				valStr := strings.ToLower(ctx.Args[1])
				switch valStr {
				case "on", "true", "yes":
					nextAdaptive = true
				case "off", "false", "no":
					nextAdaptive = false
				default:
					return fmt.Errorf("thinking adaptive: invalid argument %q (try 'on' or 'off')", ctx.Args[1])
				}
			} else {
				// Toggle
				if newSettings.Thinking.Adaptive == nil {
					nextAdaptive = true
				} else {
					nextAdaptive = !*newSettings.Thinking.Adaptive
				}
			}

			newSettings.Thinking.Adaptive = &nextAdaptive
			// Make sure thinking is enabled
			enabledVal := true
			newSettings.Thinking.Enabled = &enabledVal

			_, err = app.api.ConfigureSession(ctx.Ctx, api.ConfigureSessionRequest{
				SessionID:    sessionID,
				ProviderName: currentSession.Settings.ProviderName,
				ModelName:    currentSession.Settings.ModelName,
				AgentName:    currentSession.Settings.AgentName,
				Settings:     &newSettings,
			})
			if err != nil {
				return err
			}

			if active.InvalidateSessionState != nil {
				active.InvalidateSessionState(sessionID)
			}

			if nextAdaptive {
				active.SetStatusMessage("Adaptive thinking: ENABLED")
			} else {
				active.SetStatusMessage("Adaptive thinking: DISABLED")
			}
			return nil

		default:
			return fmt.Errorf("thinking: unknown subcommand %q (try 'toggle', 'effort', 'budget', or 'adaptive')", ctx.Args[0])
		}
	}, command.Complete(func(ctx context.Context, args []string) []command.CompletionItem {
		if len(args) == 0 {
			return nil
		}
		if len(args) == 1 {
			return []command.CompletionItem{
				{Label: "toggle", Sublabel: "Toggle model reasoning/thinking on/off"},
				{Label: "effort", Sublabel: "Open picker to select reasoning effort"},
				{Label: "budget", Sublabel: "Configure token budget (e.g. 16000 or off)"},
				{Label: "adaptive", Sublabel: "Configure adaptive thinking (on/off)"},
			}
		}
		subcmd := strings.ToLower(args[0])
		switch subcmd {
		case "adaptive":
			if len(args) == 2 {
				return []command.CompletionItem{
					{Label: "on", Sublabel: "Enable adaptive thinking"},
					{Label: "off", Sublabel: "Disable adaptive thinking"},
				}
			}
		case "budget":
			if len(args) == 2 {
				return []command.CompletionItem{
					{Label: "off", Sublabel: "Disable token budget"},
					{Label: "16000", Sublabel: "Default reasoning token budget"},
				}
			}
		}
		return nil
	}))

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
	}, command.Complete(func(ctx context.Context, args []string) []command.CompletionItem {
		if len(args) > 1 {
			return nil
		}
		return []command.CompletionItem{
			{Label: "info", Sublabel: "Show active LSP language client info"},
			{Label: "restart", Sublabel: "Restart LSP language client"},
		}
	}))

	command.Register("permissions", func(ctx command.CommandContext) error {
		if len(ctx.Args) == 0 {
			active.SetModal("permissionsmanager")
			return nil
		}

		arg := ctx.Args[0]
		modeStr := arg
		if arg == "mode" {
			if len(ctx.Args) == 1 {
				active.SetModal("permissionpicker")
				return nil
			}
			modeStr = ctx.Args[1]
		}

		var mode permissions.PermissionMode
		switch modeStr {
		case "auto":
			mode = permissions.ModeAuto
		case "default":
			mode = permissions.ModeDefault
		case "strict":
			mode = permissions.ModeStrict
		default:
			return fmt.Errorf("permissions: unknown subcommand/mode %q (try 'mode', 'auto', 'default', or 'strict')", modeStr)
		}

		sessionID := active.GetSessionID()
		_, err := app.api.SetPermissionMode(ctx.Ctx, api.SetPermissionModeRequest{
			SessionID: sessionID,
			Mode:      mode,
			Scope:     permissions.ScopeSession,
		})
		if err != nil {
			return fmt.Errorf("permissions: failed to set mode: %w", err)
		}

		if active.InvalidateSessionState != nil {
			active.InvalidateSessionState(sessionID)
		}
		return nil
	}, command.Complete(func(ctx context.Context, args []string) []command.CompletionItem {
		if len(args) == 1 {
			return []command.CompletionItem{
				{Label: "mode", Sublabel: "Open interactive permission mode switcher modal"},
				{Label: "auto", Sublabel: "Automatically approve all file/command permissions"},
				{Label: "default", Sublabel: "Approve basic commands, prompt for sensitive actions"},
				{Label: "strict", Sublabel: "Always prompt for approval"},
			}
		}
		if len(args) == 2 && args[0] == "mode" {
			return []command.CompletionItem{
				{Label: "auto", Sublabel: "Set mode to Auto"},
				{Label: "default", Sublabel: "Set mode to Default"},
				{Label: "strict", Sublabel: "Set mode to Strict"},
			}
		}
		return nil
	}))

	command.Register("agent", func(ctx command.CommandContext) error {
		if len(ctx.Args) == 0 {
			active.SetModal("agentpicker")
			return nil
		}

		agentName := ctx.Args[0]
		sessionID := active.GetSessionID()
		if sessionID == "" {
			return fmt.Errorf("agent: no active session")
		}

		// Fetch the session configuration from backend
		sessionsResp, err := app.api.ListSessions(ctx.Ctx, api.ListSessionsRequest{})
		if err != nil {
			return fmt.Errorf("agent: failed to list sessions: %w", err)
		}
		var currentSession api.Session
		found := false
		for _, s := range sessionsResp.Sessions {
			if s.ID == sessionID {
				currentSession = s
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("agent: session %q not found", sessionID)
		}

		_, err = app.api.ConfigureSession(ctx.Ctx, api.ConfigureSessionRequest{
			SessionID:    sessionID,
			ProviderName: currentSession.Settings.ProviderName,
			ModelName:    currentSession.Settings.ModelName,
			AgentName:    agentName,
		})
		if err != nil {
			return fmt.Errorf("agent: failed to configure session: %w", err)
		}

		if active.InvalidateSessionState != nil {
			active.InvalidateSessionState(sessionID)
		}
		active.SetStatusMessage(fmt.Sprintf("Agent set to: %s", agentName))
		return nil
	}, command.Complete(func(ctx context.Context, args []string) []command.CompletionItem {
		if len(args) > 1 {
			return nil
		}
		res, err := app.api.ListAgents(ctx, api.ListAgentsRequest{})
		if err != nil {
			return nil
		}
		var items []command.CompletionItem
		for _, a := range res.Agents {
			items = append(items, command.CompletionItem{
				Label:    a.Name,
				Sublabel: a.Description,
				Badge:    "AGENT",
			})
		}
		return items
	}))

	command.Register("agentpicker", func(ctx command.CommandContext) error {
		active.SetModal("agentpicker")
		return nil
	})
}
