package command

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
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
		OnSelect: func(opt ui.Option) Event {
			return ChangeModeMsg{Mode: api.PermissionMode(opt.Value)}
		},
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

func (r *Registry) handleHelp(_ Env, _ []string) (Event, error) {
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
	return PrintMsg{Text: strings.Join(rows, "\n")}, nil
}

// HandleMode displays or alters the active permission mode.
func HandleMode(env Env, args []string) (Event, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		mode := cmp.Or(env.Mode, api.ModeDefault)
		picker := ui.NewPicker("Select permission mode:", modeOptionsWithActive(mode), func(opt ui.Option) any {
			return ChangeModeMsg{Mode: api.PermissionMode(opt.Value)}
		})
		return OpenPickerMsg{
			Picker: picker,
		}, nil
	}

	nextMode := api.PermissionMode(strings.TrimSpace(args[0]))
	if !isValidPermissionMode(nextMode) {
		return nil, fmt.Errorf("error setting permission mode: invalid permission mode %q (valid: default, accept_edits, plan, ask, auto)", nextMode)
	}
	return ChangeModeMsg{Mode: nextMode}, nil
}

func modeOptionsWithActive(activeMode api.PermissionMode) []ui.Option {
	if activeMode == "" {
		activeMode = api.ModeDefault
	}
	return []ui.Option{
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
			Description: "Read-only mode; block writes, edits, and shell commands",
			Current:     activeMode == api.ModePlan,
		},
		{
			Value:       string(api.ModeAsk),
			Label:       string(api.ModeAsk),
			Description: "Ask for confirmation before every tool call",
			Current:     activeMode == api.ModeAsk,
		},
		{
			Value:       string(api.ModeAuto),
			Label:       string(api.ModeAuto),
			Description: "Allow all workspace actions except unsafe operations",
			Current:     activeMode == api.ModeAuto,
		},
	}
}

func modeOptions(env Env) []ui.Option {
	mode := cmp.Or(env.Mode, api.ModeDefault)
	return modeOptionsWithActive(mode)
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
func HandleClear(_ Env, _ []string) (Event, error) {
	return ClearScreenMsg{}, nil
}

// HandleExit signals that the REPL session should terminate.
func HandleExit(_ Env, _ []string) (Event, error) {
	return ExitMsg{}, nil
}
