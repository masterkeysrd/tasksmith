package session

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
)

func validateThinking(setting *api.ThinkingSetting, prof *profile.Profile) error {
	if setting == nil || setting.Type == "default" {
		return nil
	}
	if prof == nil {
		return fmt.Errorf("%w: thinking capabilities are unavailable for this model", ErrInvalidConfig)
	}
	kind := setting.Type
	if kind == "budget" {
		kind = "budget_tokens"
	}
	for _, option := range prof.Capabilities.ReasoningOptions {
		if option.Type != kind && !(kind == "budget_tokens" && option.Type == "budget") {
			continue
		}
		switch kind {
		case "effort":
			if setting.Effort == "" || !slices.Contains(option.Values, setting.Effort) {
				return fmt.Errorf("%w: unsupported effort %q (available: %s)", ErrInvalidConfig, setting.Effort, strings.Join(option.Values, ", "))
			}
		case "budget_tokens":
			if setting.Budget <= 0 || setting.Budget > 2147483647 || (option.Min > 0 && setting.Budget < option.Min) || (option.Max > 0 && setting.Budget > option.Max) {
				return fmt.Errorf("%w: thinking budget %d is outside the supported range (%d to %d tokens)", ErrInvalidConfig, setting.Budget, option.Min, option.Max)
			}
		case "toggle":
		default:
			return fmt.Errorf("%w: unknown thinking mode %q", ErrInvalidConfig, setting.Type)
		}
		return nil
	}
	return fmt.Errorf("%w: %s is not supported by the current model", ErrInvalidConfig, setting.Type)
}

func thinkingConfig(setting *api.ThinkingSetting) *llm.ThinkingConfig {
	if setting == nil {
		return nil
	}
	switch setting.Type {
	case "effort":
		return &llm.ThinkingConfig{Enabled: true, Effort: setting.Effort}
	case "budget", "budget_tokens":
		return &llm.ThinkingConfig{Enabled: true, Budget: setting.Budget}
	case "toggle":
		return &llm.ThinkingConfig{Enabled: setting.Enabled}
	}
	return nil
}

func AgentThinking(thinking map[string]any) *api.ThinkingSetting {
	if thinking == nil {
		return nil
	}
	if effort, ok := thinking["effort"].(string); ok && effort != "" {
		return &api.ThinkingSetting{Type: "effort", Effort: effort}
	}
	if value, err := strconv.Atoi(fmt.Sprint(thinking["budget_tokens"])); err == nil && value > 0 {
		return &api.ThinkingSetting{Type: "budget", Budget: value}
	}
	if value, ok := thinking["enabled"].(bool); ok {
		return &api.ThinkingSetting{Type: "toggle", Enabled: value}
	}
	if typ, _ := thinking["type"].(string); typ == "effort" {
		if effort, ok := thinking["default"].(string); ok && effort != "" {
			return &api.ThinkingSetting{Type: "effort", Effort: effort}
		}
	}
	return nil
}
