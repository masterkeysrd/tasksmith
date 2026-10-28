package command

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func thinkingCapability(kind string) (api.ThinkingCapability, error) {
	model, err := store.CurrentThinkingModel(context.Background())
	if err != nil {
		return api.ThinkingCapability{}, fmt.Errorf("cannot determine model thinking support: %w", err)
	}
	for _, capability := range model.Thinking {
		if capability.Type == kind || (kind == "budget" && capability.Type == "budget_tokens") {
			return capability, nil
		}
	}
	return api.ThinkingCapability{}, fmt.Errorf("%s is not supported by model %s", kind, model.ID)
}

func handleBudget(args []string) error {
	capability, err := thinkingCapability("budget")
	if err != nil {
		return err
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: /budget [tokens|default]")
	}
	if len(args) == 0 {
		state := store.GetSession()
		cfg := state.Config
		if state.Session != nil {
			cfg = state.Session.Config
		}
		value := capability.Default
		if cfg.Thinking != nil && (cfg.Thinking.Type == "budget" || cfg.Thinking.Type == "budget_tokens") {
			value = strconv.Itoa(cfg.Thinking.Budget)
		}
		if value == "" {
			value = "provider default"
		}
		store.AppendCommandOutput("/budget", "Thinking budget: "+value)
		return nil
	}
	if strings.EqualFold(args[0], "default") {
		store.SetThinking(api.ThinkingSetting{Type: "default"}, "/budget default", "Thinking reset to default")
		return nil
	}
	value, err := strconv.Atoi(args[0])
	if err != nil || value <= 0 || value > 2147483647 || (capability.Min > 0 && value < capability.Min) || (capability.Max > 0 && value > capability.Max) {
		return fmt.Errorf("invalid thinking budget %q (minimum %d, maximum %d tokens when specified)", args[0], capability.Min, capability.Max)
	}
	store.SetThinking(api.ThinkingSetting{Type: "budget", Budget: value}, "/budget "+args[0], fmt.Sprintf("Thinking budget set to %d tokens", value))
	return nil
}

func handleThinking(args []string) error {
	capability, err := thinkingCapability("toggle")
	if err != nil {
		return err
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: /thinking [on|off|default]")
	}
	if len(args) == 0 {
		state := store.GetSession()
		cfg := state.Config
		if state.Session != nil {
			cfg = state.Session.Config
		}
		value := capability.Default
		if cfg.Thinking != nil && cfg.Thinking.Type == "toggle" {
			value = strconv.FormatBool(cfg.Thinking.Enabled)
		}
		switch value {
		case "true":
			value = "on"
		case "false":
			value = "off"
		case "":
			value = "provider default"
		}
		store.AppendCommandOutput("/thinking", "Thinking: "+value)
		return nil
	}
	value := strings.ToLower(args[0])
	if value == "default" {
		store.SetThinking(api.ThinkingSetting{Type: "default"}, "/thinking default", "Thinking reset to default")
		return nil
	}
	if value != "on" && value != "off" {
		return fmt.Errorf("usage: /thinking [on|off|default]")
	}
	store.SetThinking(api.ThinkingSetting{Type: "toggle", Enabled: value == "on"}, "/thinking "+value, "Thinking set to "+value)
	return nil
}
