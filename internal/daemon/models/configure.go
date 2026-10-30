package models

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
)

// ErrInvalidConfig indicates an invalid explicit session override.
var ErrInvalidConfig = errors.New("models: invalid configuration")

// Preferences are the caller's desired call settings.
type Preferences struct {
	// Temperature is the caller's requested sampling temperature.
	Temperature *float32
	// AgentThinking is the agent-level thinking default.
	AgentThinking *api.ThinkingSetting
	// SessionThinking is the explicit session-level thinking override.
	SessionThinking *api.ThinkingSetting
}

// Diagnostic describes a non-fatal configuration issue.
type Diagnostic struct {
	Code    string `json:"code"`
	Setting string `json:"setting"`
	Message string `json:"message"`
}

// Configure validates prefs against m and returns the effective loom config.
//
// It is a pure function: it moves the gateway's settings resolution out of the
// gateway, unchanged in behavior. This includes the thinking validation, the
// Anthropic temperature/thinking conflict, and the unknown/unsupported
// temperature diagnostics.
//
// It returns an error only for invalid explicit session overrides (today's
// ErrInvalidConfig behavior); agent-level problems become diagnostics.
func Configure(m Model, prefs Preferences) (llm.ModelConfig, []Diagnostic, error) {
	cfg := llm.ModelConfig{Temperature: prefs.Temperature}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = m.Limits.Output
	}
	if cfg.ContextWindow == 0 {
		cfg.ContextWindow = m.Limits.Context
	}

	var diags []Diagnostic

	setting := prefs.SessionThinking
	if setting == nil {
		setting = prefs.AgentThinking
	}
	var thinkingErr error
	if setting != nil {
		prof := m.Profile()
		if err := validateThinking(setting, &prof); err != nil {
			if prefs.SessionThinking != nil {
				thinkingErr = err
			} else {
				diags = append(diags, Diagnostic{
					Code:    "thinking_unsupported",
					Setting: "thinking",
					Message: fmt.Sprintf("Agent thinking setting is unavailable and will be omitted: %v", err),
				})
			}
			cfg.Thinking = nil
		} else {
			cfg.Thinking = thinkingConfig(setting)
		}
	}

	if cfg.Temperature != nil {
		if m.ProviderType == "anthropic" && cfg.Thinking != nil && (cfg.Thinking.Budget > 0 || cfg.Thinking.Adaptive) {
			diags = append(diags, Diagnostic{
				Code:    "temperature_thinking_conflict",
				Setting: "temperature",
				Message: "Temperature cannot be combined with Anthropic extended thinking and will be omitted.",
			})
			cfg.Temperature = nil
		} else if m.Capabilities.Temperature == nil {
			diags = append(diags, Diagnostic{
				Code:    "temperature_unknown",
				Setting: "temperature",
				Message: "Temperature support is unknown; the configured value will be sent and may be rejected by the provider.",
			})
		} else if !*m.Capabilities.Temperature {
			diags = append(diags, Diagnostic{
				Code:    "temperature_unsupported",
				Setting: "temperature",
				Message: fmt.Sprintf("Temperature %g is not supported by this model and will be omitted.", *cfg.Temperature),
			})
			cfg.Temperature = nil
		}
	}

	return cfg, diags, thinkingErr
}

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
		if option.Type != kind && (kind != "budget_tokens" || option.Type != "budget") {
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
