package command

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
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
		Handler:     handleMode,
		Complete:    completeModes,
	})
	r.Register(Command{
		Name:        "model",
		Usage:       "/model [model_id]",
		Description: "Select or switch the active model",
		Handler:     handleModel,
		Complete:    completeModels,
	})
	r.Register(Command{
		Name:        "agent",
		Usage:       "/agent [agent_name]",
		Description: "Select or switch the active agent",
		Handler:     handleAgent,
		Complete:    completeAgents,
	})
	r.Register(Command{
		Name:        "effort",
		Usage:       "/effort [level|default]",
		Description: "Set reasoning effort for the model",
		Handler:     handleEffort,
		Complete:    completeEffort,
	})
	r.Register(Command{
		Name:        "budget",
		Usage:       "/budget [tokens|default]",
		Description: "Set the thinking token budget",
		Handler:     handleBudget,
	})
	r.Register(Command{
		Name:        "thinking",
		Usage:       "/thinking [on|off|default]",
		Description: "Toggle thinking for supported models",
		Handler:     handleThinking,
	})
	r.Register(Command{
		Name:        "theme",
		Usage:       "/theme [theme_name]",
		Description: "Select or switch the REPL color theme",
		Handler:     handleTheme,
		Complete:    completeThemes,
	})
	r.Register(Command{
		Name:        "exit",
		Aliases:     []string{"quit", "q"},
		Usage:       "/exit",
		Description: "Exit the REPL",
		Handler: func(_ []string) error {
			store.RequestExit()
			return nil
		},
	})
}

func (r *Registry) handleHelp(_ []string) error {
	cmds := r.List()
	opts := make([]uicomponents.Option, len(cmds))
	for i, cmd := range cmds {
		usage := cmd.Usage
		if len(cmd.Aliases) > 0 {
			aliases := make([]string, len(cmd.Aliases))
			for j, a := range cmd.Aliases {
				aliases[j] = "/" + a
			}
			usage = fmt.Sprintf("%s (%s)", usage, strings.Join(aliases, ", "))
		}
		opts[i] = uicomponents.Option{
			Value:       "/" + cmd.Name,
			Label:       usage,
			Description: cmd.Description,
		}
	}
	picker := components.NewPicker("Available commands:", opts, func(opt uicomponents.Option) {
		_ = r.Execute(opt.Value)
	})
	picker.Filterable = true
	picker.OnCancel = func() {
		store.AppendCommandOutput("/help", "Exited /help command")
	}
	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: picker,
	})
	return nil
}

func modeOptionsWithActive(active api.PermissionMode) []uicomponents.Option {
	modes := []struct {
		mode api.PermissionMode
		desc string
	}{
		{api.ModeDefault, "Confirm execution on every non-standard action"},
		{api.ModeAcceptEdits, "Auto-approve workspace file edits"},
		{api.ModePlan, "Read-only planning mode, prevents execution"},
		{api.ModeAsk, "Require confirmation for all actions"},
		{api.ModeAuto, "Auto-approve all actions without prompts"},
	}

	opts := make([]uicomponents.Option, len(modes))
	for i, m := range modes {
		opts[i] = uicomponents.Option{
			Value:       string(m.mode),
			Label:       string(m.mode),
			Description: m.desc,
			Current:     m.mode == active,
		}
	}
	return opts
}

func isValidPermissionMode(m api.PermissionMode) bool {
	switch m {
	case api.ModeDefault, api.ModeAcceptEdits, api.ModePlan, api.ModeAsk, api.ModeAuto:
		return true
	default:
		return false
	}
}

func handleMode(args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		activeMode := store.GetSession().Config.Mode
		if store.GetSession().Session != nil {
			activeMode = store.GetSession().Session.Config.Mode
		}
		if activeMode == "" {
			activeMode = api.ModeDefault
		}

		picker := components.NewPicker("Select permission mode:", modeOptionsWithActive(activeMode), func(opt uicomponents.Option) {
			store.SetMode(api.PermissionMode(opt.Value))
			if store.GetSession().Session == nil {
				store.AppendCommandOutput("/mode "+opt.Value, fmt.Sprintf("Switched to %s mode.", opt.Value))
			}
		})
		picker.OnCancel = func() {
			store.AppendCommandOutput("/mode", "Exited /mode command")
		}
		store.EnqueueSlot(store.SlotItem{
			Kind: store.SlotKindPicker,
			Data: picker,
		})
		return nil
	}

	nextMode := api.PermissionMode(strings.TrimSpace(args[0]))
	if !isValidPermissionMode(nextMode) {
		return fmt.Errorf("invalid permission mode %q (valid: default, accept_edits, plan, ask, auto)", nextMode)
	}
	store.SetMode(nextMode)
	if store.GetSession().Session == nil {
		store.AppendCommandOutput("/mode "+string(nextMode), fmt.Sprintf("Switched to %s mode.", nextMode))
	}
	return nil
}

func handleModel(args []string) error {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		store.SetModel(strings.TrimSpace(args[0]))
		return nil
	}

	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: ModelPicker,
	})
	return nil
}

var loadingHint = trail.FC("LoadingHint", func(text string) trail.Node {
	store.UseHint(text)
	return nil
})

// ModelPicker renders an interactive model picker querying daemon models via wind.
var ModelPicker = trail.FC("ModelPicker", func(slotID string) trail.Node {
	c := store.Client()

	result := wind.Use("models", promise.WrapWithProps(func(ctx context.Context, _ string) ([]api.Model, error) {
		if c == nil {
			return nil, nil
		}
		return c.ListModels(ctx)
	}), wind.Options{Enabled: c != nil})

	kitex.UseEffect(func() {
		if c == nil {
			store.RemoveSlot(slotID)
			store.AppendNotice("Client unavailable.")
			return
		}
		if result.IsLoading {
			return
		}
		if result.IsError {
			store.RemoveSlot(slotID)
			store.AppendError("Error", result.Error.Error())
		} else if len(result.Data) == 0 {
			store.RemoveSlot(slotID)
			store.AppendNotice("No models available.")
		}
	}, []any{c == nil, result.IsLoading, result.IsError, result.Error, len(result.Data)})

	if c == nil {
		return nil
	}
	if result.IsLoading {
		return loadingHint("Loading models...")
	}
	if result.IsError || len(result.Data) == 0 {
		return nil
	}

	currentModel := store.GetSession().Config.Model
	if store.GetSession().Session != nil {
		currentModel = store.GetSession().Session.Config.Model
	}

	opts := make([]uicomponents.Option, len(result.Data))
	for i, m := range result.Data {
		label := cmp.Or(m.Label, m.Name, m.ID)
		opts[i] = uicomponents.Option{
			Value:       m.ID,
			Label:       label,
			Description: m.Provider,
			Current:     m.ID == currentModel,
		}
	}

	picker := components.NewPicker("Select model:", opts, func(opt uicomponents.Option) {
		store.RemoveSlot(slotID)
		store.SetModel(opt.Value)
	})
	picker.Filterable = true
	picker.OnCancel = func() {
		store.RemoveSlot(slotID)
		store.AppendCommandOutput("/model", "Exited /model command")
	}

	return components.Picker(picker)
})

func handleAgent(args []string) error {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		store.SetAgent(strings.TrimSpace(args[0]))
		return nil
	}

	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: AgentPicker,
	})
	return nil
}

// AgentPicker renders an interactive agent picker querying daemon agents via wind.
var AgentPicker = trail.FC("AgentPicker", func(slotID string) trail.Node {
	c := store.Client()

	result := wind.Use("agents", promise.WrapWithProps(func(ctx context.Context, _ string) ([]api.Agent, error) {
		if c == nil {
			return nil, nil
		}
		return c.ListAgents(ctx)
	}), wind.Options{Enabled: c != nil})

	kitex.UseEffect(func() {
		if c == nil {
			store.RemoveSlot(slotID)
			store.AppendNotice("Client unavailable.")
			return
		}
		if result.IsLoading {
			return
		}
		if result.IsError {
			store.RemoveSlot(slotID)
			store.AppendError("Error", result.Error.Error())
		} else if len(result.Data) == 0 {
			store.RemoveSlot(slotID)
			store.AppendNotice("No agents available.")
		}
	}, []any{c == nil, result.IsLoading, result.IsError, result.Error, len(result.Data)})

	if c == nil {
		return nil
	}
	if result.IsLoading {
		return loadingHint("Loading agents...")
	}
	if result.IsError || len(result.Data) == 0 {
		return nil
	}

	currentAgent := store.GetSession().Config.Agent
	if store.GetSession().Session != nil {
		currentAgent = store.GetSession().Session.Config.Agent
	}

	opts := make([]uicomponents.Option, len(result.Data))
	for i, a := range result.Data {
		opts[i] = uicomponents.Option{
			Value:       a.Name,
			Label:       a.Name,
			Description: a.Description,
			Current:     a.Name == currentAgent,
		}
	}

	picker := components.NewPicker("Select agent:", opts, func(opt uicomponents.Option) {
		store.RemoveSlot(slotID)
		store.SetAgent(opt.Value)
	})
	picker.Filterable = true
	picker.OnCancel = func() {
		store.RemoveSlot(slotID)
		store.AppendCommandOutput("/agent", "Exited /agent command")
	}

	return components.Picker(picker)
})

func handleEffort(args []string) error {
	capability, err := thinkingCapability("effort")
	if err != nil {
		return err
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: /effort [level|default]")
	}
	if len(args) == 1 {
		value := strings.ToLower(args[0])
		if value == "default" {
			store.SetThinking(api.ThinkingSetting{Type: "default"}, "/effort default", "Thinking reset to default")
			return nil
		}
		values := make([]string, 0, len(capability.Values))
		for _, option := range capability.Values {
			values = append(values, option.Value)
		}
		if !slices.Contains(values, value) {
			return fmt.Errorf("unsupported effort %q (available: %s)", value, strings.Join(values, ", "))
		}
		store.SetThinking(api.ThinkingSetting{Type: "effort", Effort: value}, "/effort "+value, "Thinking effort set to "+value)
		return nil
	}
	opts := make([]uicomponents.Option, 0, len(capability.Values)+1)
	for _, value := range capability.Values {
		opts = append(opts, uicomponents.Option{Value: value.Value, Label: value.Value, Description: value.Description, Current: strings.EqualFold(value.Value, store.GetSession().Thinking)})
	}
	opts = append(opts, uicomponents.Option{Value: "default", Label: "default"})
	picker := components.NewPicker("Select thinking effort:", opts, func(opt uicomponents.Option) {
		if err := handleEffort([]string{opt.Value}); err != nil {
			store.AppendError("Error", err.Error())
		}
	})
	picker.OnCancel = func() { store.AppendCommandOutput("/effort", "Exited /effort command") }
	store.EnqueueSlot(store.SlotItem{Kind: store.SlotKindPicker, Data: picker})
	return nil
}

func handleTheme(args []string) error {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		themeName := strings.TrimSpace(args[0])
		if err := theme.Set(themeName); err != nil {
			return err
		}
		store.AppendCommandOutput("/theme "+themeName, fmt.Sprintf("Theme set to %s.", themeName))
		return nil
	}

	themes := theme.List()
	current := theme.GetName()
	opts := make([]uicomponents.Option, len(themes))
	for i, name := range themes {
		opts[i] = uicomponents.Option{
			Value:   name,
			Label:   name,
			Current: name == current,
		}
	}

	picker := components.NewPicker("Select theme:", opts, func(opt uicomponents.Option) {
		if err := theme.Set(opt.Value); err == nil {
			store.AppendCommandOutput("/theme "+opt.Value, fmt.Sprintf("Theme set to %s.", opt.Value))
		}
	})
	picker.Filterable = true
	picker.OnCancel = func() {
		store.AppendCommandOutput("/theme", "Exited /theme command")
	}
	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: picker,
	})
	return nil
}
