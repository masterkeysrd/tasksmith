package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
)

func (r *Registry) registerSystemCommands() {
	r.Register(Command{
		Name:        "help",
		Aliases:     []string{"h", "?"},
		Usage:       "/help",
		Description: "Show available slash commands",
		Handler:     r.handleHelp,
	})
	r.Register(Command{
		Name:        "mode",
		Usage:       "/mode [default|accept_edits|plan|ask|auto]",
		Description: "Read or update the session permission mode",
		Handler:     HandleMode,
	})
	r.Register(Command{
		Name:        "clear",
		Usage:       "/clear",
		Description: "Clear the terminal screen",
		Handler:     HandleClear,
	})
	r.Register(Command{
		Name:        "exit",
		Aliases:     []string{"quit", "q"},
		Usage:       "/exit",
		Description: "Exit the REPL",
		Handler:     HandleExit,
	})
}

func (r *Registry) handleHelp(_ context.Context, _ *Env, _ []string) (Result, error) {
	cmds := r.List()
	var rows []string
	rows = append(rows, "Available commands:")
	for _, cmd := range cmds {
		usage := cmd.Usage
		if len(cmd.Aliases) > 0 {
			aliases := make([]string, len(cmd.Aliases))
			for i, a := range cmd.Aliases {
				aliases[i] = "/" + a
			}
			usage = fmt.Sprintf("%s (%s)", usage, strings.Join(aliases, ", "))
		}
		rows = append(rows, fmt.Sprintf("  %-44s %s", usage, cmd.Description))
	}
	return Result{Output: strings.Join(rows, "\n")}, nil
}

// HandleMode displays or alters the active permission mode.
func HandleMode(ctx context.Context, env *Env, args []string) (Result, error) {
	if err := requireEnvSession(env, false); err != nil {
		return Result{}, err
	}

	hasSession := env.Session != nil && strings.TrimSpace(env.Session.ID) != ""
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		if !hasSession {
			mode := env.Mode
			if mode == "" {
				mode = api.ModeDefault
			}
			return Result{
				Output:     fmt.Sprintf("Permission mode: %s", mode),
				ModeChange: mode,
			}, nil
		}
		mode, err := env.Client.PermissionMode(ctx, env.Session.ID)
		if err != nil {
			return Result{}, fmt.Errorf("error reading permission mode: %w", err)
		}
		return Result{
			Output:     fmt.Sprintf("Permission mode: %s", mode),
			ModeChange: mode,
		}, nil
	}

	nextMode := api.PermissionMode(strings.TrimSpace(args[0]))
	if !hasSession {
		if !isValidPermissionMode(nextMode) {
			return Result{}, fmt.Errorf("error setting permission mode: invalid permission mode %q (valid: default, accept_edits, plan, ask, auto)", nextMode)
		}
		return Result{
			Output:     fmt.Sprintf("Permission mode set to: %s", nextMode),
			ModeChange: nextMode,
		}, nil
	}

	if err := env.Client.SetPermissionMode(ctx, env.Session.ID, nextMode); err != nil {
		return Result{}, fmt.Errorf("error setting permission mode: %w (valid: default, accept_edits, plan, ask, auto)", err)
	}
	return Result{
		Output:     fmt.Sprintf("Permission mode set to: %s", nextMode),
		ModeChange: nextMode,
	}, nil
}

func isValidPermissionMode(mode api.PermissionMode) bool {
	switch mode {
	case api.ModeDefault, api.ModeAcceptEdits, api.ModePlan, api.ModeAsk, api.ModeAuto:
		return true
	default:
		return false
	}
}

// HandleClear signals that the terminal screen should be cleared.
func HandleClear(_ context.Context, _ *Env, _ []string) (Result, error) {
	return Result{Clear: true}, nil
}

// HandleExit signals that the REPL session should terminate.
func HandleExit(_ context.Context, _ *Env, _ []string) (Result, error) {
	return Result{Quit: true}, nil
}
