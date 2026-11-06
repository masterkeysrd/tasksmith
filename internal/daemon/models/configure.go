package models

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
)

// ErrInvalidConfig indicates an invalid explicit session override.
var ErrInvalidConfig = errors.New("models: invalid configuration")

// Settings holds the caller's desired model execution settings.
type Settings struct {
	Temperature    *float32
	Thinking       *api.ThinkingSetting
	StrictThinking bool
}

// Configure validates settings against m and returns the effective loom config.
//
// It is a pure function: it moves the gateway's settings resolution out of the
// gateway, unchanged in behavior. This includes the thinking validation, the
// Anthropic temperature/thinking conflict, and the unknown/unsupported
// temperature diagnostics.
func (m Model) Configure(s Settings) (llm.ModelConfig, []api.SessionDiagnostic, error) {
	cfg := llm.ModelConfig{Temperature: s.Temperature}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = m.Limits.Output
	}
	if cfg.ContextWindow == 0 {
		cfg.ContextWindow = m.Limits.Context
	}

	var diags []api.SessionDiagnostic

	setting := s.Thinking
	if setting == nil {
		setting = m.DefaultThinking()
	}
	var thinkingErr error
	if setting != nil {
		prof := m.Profile()
		if err := validateThinking(setting, &prof); err != nil {
			switch {
			case s.Thinking == nil:
				// The model declared this default itself, so an unusable value is
				// ignored rather than failing the turn the user never configured.
				cfg.Thinking = nil
			case s.StrictThinking:
				thinkingErr = err
				cfg.Thinking = nil
			default:
				diags = append(diags, api.SessionDiagnostic{
					Code:    "thinking_unsupported",
					Setting: "thinking",
					Message: fmt.Sprintf("Agent thinking setting is unavailable and will be omitted: %v", err),
				})
				cfg.Thinking = nil
			}
		} else {
			cfg.Thinking = thinkingConfig(setting)
		}
	}

	if cfg.Temperature != nil {
		if m.ProviderType == "anthropic" && cfg.Thinking != nil && (cfg.Thinking.Budget > 0 || cfg.Thinking.Adaptive) {
			diags = append(diags, api.SessionDiagnostic{
				Code:    "temperature_thinking_conflict",
				Setting: "temperature",
				Message: "Temperature cannot be combined with Anthropic extended thinking and will be omitted.",
			})
			cfg.Temperature = nil
		} else if m.Capabilities.Temperature == nil {
			diags = append(diags, api.SessionDiagnostic{
				Code:    "temperature_unknown",
				Setting: "temperature",
				Message: "Temperature support is unknown; the configured value will be sent and may be rejected by the provider.",
			})
		} else if !*m.Capabilities.Temperature {
			diags = append(diags, api.SessionDiagnostic{
				Code:    "temperature_unsupported",
				Setting: "temperature",
				Message: fmt.Sprintf("Temperature %g is not supported by this model and will be omitted.", *cfg.Temperature),
			})
			cfg.Temperature = nil
		}
	}

	return cfg, diags, thinkingErr
}

// EffectiveThinking resolves the thinking setting a session would actually run
// with, given an optional session override and the active agent's default.
//
// Precedence is session override, then agent default, then the model's own
// default. An agent default the model cannot honor resolves to nil, because
// [Model.Configure] omits thinking in that case and the provider default
// applies.
func (m Model) EffectiveThinking(session, agent *api.ThinkingSetting) *api.ThinkingSetting {
	if isExplicitThinking(session) {
		return session
	}
	if isExplicitThinking(agent) {
		if m.SupportsThinking(agent) {
			return agent
		}
		return nil
	}
	return m.DefaultThinking()
}

// SupportsThinking reports whether the model can honor setting.
func (m Model) SupportsThinking(setting *api.ThinkingSetting) bool {
	if !isExplicitThinking(setting) {
		return false
	}
	prof := m.Profile()
	return validateThinking(setting, &prof) == nil
}

// isExplicitThinking reports whether setting requests a thinking strategy
// rather than the absence of an override.
func isExplicitThinking(setting *api.ThinkingSetting) bool {
	return setting != nil && setting.Type != "" && setting.Type != "default"
}

// DefaultThinking returns the thinking setting implied by the model's own
// declared thinking capabilities, or nil when the model expresses no default.
//
// It is the lowest-priority layer of thinking resolution: a session override or
// an agent default always takes precedence over it. A capability explicitly
// marked as the model's primary strategy wins; otherwise the first capability
// declaring a usable default is used.
func (m Model) DefaultThinking() *api.ThinkingSetting {
	for _, option := range m.Capabilities.Thinking {
		if option.IsDefault {
			return thinkingSettingForOption(option)
		}
	}
	for _, option := range m.Capabilities.Thinking {
		if setting := thinkingSettingForOption(option); setting != nil {
			return setting
		}
	}
	return nil
}

// thinkingSettingForOption converts one thinking capability into the session
// setting it implies. It returns nil when the capability declares no default
// this layer can express, which leaves thinking untouched rather than guessing.
func thinkingSettingForOption(option ThinkingOption) *api.ThinkingSetting {
	switch option.Type {
	case "effort":
		effort := option.Default
		if effort == "" && option.IsDefault && len(option.Values) > 0 {
			effort = option.Values[0]
		}
		if effort == "" {
			return nil
		}
		return &api.ThinkingSetting{Type: "effort", Effort: effort}
	case "budget", "budget_tokens":
		budget := 0
		if option.Default != "" {
			if parsed, err := strconv.Atoi(option.Default); err == nil {
				budget = parsed
			}
		}
		if budget <= 0 && option.IsDefault {
			budget = option.Min
		}
		if budget <= 0 {
			return nil
		}
		return &api.ThinkingSetting{Type: "budget", Budget: budget}
	case "toggle":
		if strings.EqualFold(option.Default, "true") || (option.IsDefault && option.Default == "") {
			return &api.ThinkingSetting{Type: "toggle", Enabled: true}
		}
	case "adaptive":
		if option.IsDefault {
			return &api.ThinkingSetting{Type: "adaptive"}
		}
	}
	return nil
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
		case "toggle", "adaptive":
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
	case "adaptive":
		return &llm.ThinkingConfig{Enabled: true, Adaptive: true}
	}
	return nil
}
