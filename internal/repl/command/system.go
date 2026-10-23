package command

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
	"github.com/masterkeysrd/tasksmith/internal/ui/components"
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
		Options:     modeOptions,
		Complete:    completeModes,
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

func (r *Registry) handleHelp(env Env, _ []string) error {
	cmds := r.List()
	opts := make([]components.Option, len(cmds))
	for i, cmd := range cmds {
		usage := cmd.Usage
		if len(cmd.Aliases) > 0 {
			aliases := make([]string, len(cmd.Aliases))
			for j, a := range cmd.Aliases {
				aliases[j] = "/" + a
			}
			usage = fmt.Sprintf("%s (%s)", usage, strings.Join(aliases, ", "))
		}
		opts[i] = components.Option{
			Value:       "/" + cmd.Name,
			Label:       usage,
			Description: cmd.Description,
		}
	}
	picker := ui.NewPicker("Available commands:", opts, func(opt components.Option) {
		_ = r.Execute(env, opt.Value)
	})
	picker.Filterable = true
	picker.OnCancel = func() {
		if env.AppendOutput != nil {
			env.AppendOutput(ui.CommandOutputProps{
				Command: "/help",
				Output:  "Exited /help command",
			})
		}
	}
	if env.OpenPicker != nil {
		env.OpenPicker(picker)
	}
	return nil
}

// HandleMode displays or alters the active permission mode.
func HandleMode(env Env, args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		mode := cmp.Or(env.Mode, api.ModeDefault)
		picker := ui.NewPicker("Select permission mode:", modeOptionsWithActive(mode), func(opt components.Option) {
			if env.SetMode != nil {
				env.SetMode(api.PermissionMode(opt.Value))
			}
			if env.AppendOutput != nil {
				env.AppendOutput(ui.CommandOutputProps{
					Command: "/mode",
					Output:  fmt.Sprintf("Switched to %s mode.", opt.Value),
				})
			}
		})
		picker.OnCancel = func() {
			if env.AppendOutput != nil {
				env.AppendOutput(ui.CommandOutputProps{
					Command: "/mode",
					Output:  "Exited /mode command",
				})
			}
		}
		if env.OpenPicker != nil {
			env.OpenPicker(picker)
		}
		return nil
	}

	nextMode := api.PermissionMode(strings.TrimSpace(args[0]))
	if !isValidPermissionMode(nextMode) {
		return fmt.Errorf("error setting permission mode: invalid permission mode %q (valid: default, accept_edits, plan, ask, auto)", nextMode)
	}
	if env.SetMode != nil {
		env.SetMode(nextMode)
	}
	if env.AppendOutput != nil {
		env.AppendOutput(ui.CommandOutputProps{
			Command: "/mode " + string(nextMode),
			Output:  fmt.Sprintf("Switched to %s mode.", nextMode),
		})
	}
	return nil
}

// HandleClear clears the terminal screen.
func HandleClear(env Env, _ []string) error {
	if env.ClearScreen != nil {
		env.ClearScreen()
	}
	return nil
}

// HandleExit terminates the REPL.
func HandleExit(env Env, _ []string) error {
	if env.Exit != nil {
		env.Exit()
	}
	return nil
}

func modeOptionsWithActive(activeMode api.PermissionMode) []components.Option {
	if activeMode == "" {
		activeMode = api.ModeDefault
	}
	return []components.Option{
		{
			Value:       string(api.ModeDefault),
			Label:       string(api.ModeDefault),
			Description: "Allow safe reads; ask for writes, commands, and network",
			Current:     activeMode == api.ModeDefault,
		},
		{
			Value:       string(api.ModeAcceptEdits),
			Label:       string(api.ModeAcceptEdits),
			Description: "Allow safe reads and workspace file edits",
			Current:     activeMode == api.ModeAcceptEdits,
		},
		{
			Value:       string(api.ModePlan),
			Label:       string(api.ModePlan),
			Description: "Allow safe reads; disallow all writes, commands, and network",
			Current:     activeMode == api.ModePlan,
		},
		{
			Value:       string(api.ModeAsk),
			Label:       string(api.ModeAsk),
			Description: "Ask confirmation for all tool actions",
			Current:     activeMode == api.ModeAsk,
		},
		{
			Value:       string(api.ModeAuto),
			Label:       string(api.ModeAuto),
			Description: "Automatically allow all tool actions without prompting",
			Current:     activeMode == api.ModeAuto,
		},
	}
}

func modeOptions(env Env) []components.Option {
	return modeOptionsWithActive(env.Mode)
}

func isValidPermissionMode(m api.PermissionMode) bool {
	switch m {
	case api.ModeDefault, api.ModeAcceptEdits, api.ModePlan, api.ModeAsk, api.ModeAuto:
		return true
	default:
		return false
	}
}
