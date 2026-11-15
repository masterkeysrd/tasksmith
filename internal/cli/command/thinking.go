package command

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

// thinkingModel is the active model together with the thinking strategies it
// declares. The model is fetched once per command so a handler that checks both
// the toggle and the adaptive strategy does not ask the daemon twice.
type thinkingModel struct {
	model *api.Model
}

func newThinkingModel(ctx context.Context) (thinkingModel, error) {
	model, err := store.CurrentThinkingModel(ctx)
	if err != nil {
		return thinkingModel{}, fmt.Errorf("cannot determine model thinking support: %w", err)
	}
	return thinkingModel{model: model}, nil
}

func currentThinkingModel() (thinkingModel, error) {
	return newThinkingModel(context.Background())
}

// capability returns the declared strategy such as "toggle", "effort" or
// "adaptive". The budget strategy is also spelled budget_tokens by providers.
func (m thinkingModel) capability(kind string) (api.ThinkingCapability, bool) {
	for _, capability := range m.model.Thinking {
		if capability.Type == kind || (kind == "budget" && capability.Type == "budget_tokens") {
			return capability, true
		}
	}
	return api.ThinkingCapability{}, false
}

// requireCapability reports a strategy the model does not declare, naming the
// model so the user knows which one was inspected.
func (m thinkingModel) requireCapability(kind string) (api.ThinkingCapability, error) {
	if capability, ok := m.capability(kind); ok {
		return capability, nil
	}
	return api.ThinkingCapability{}, fmt.Errorf("%s is not supported by model %s", kind, m.model.ID)
}

// defaultThinking reports what the model does with thinking when the session has
// no override, and whether the model declares any default at all.
func (m thinkingModel) defaultThinking() (*api.ThinkingSetting, bool) {
	setting := store.ModelThinkingDefault(m.model)
	return setting, setting != nil
}

// thinkingValueLabel renders a thinking setting as the short state a command
// reports: "on", "off", "adaptive", an effort name, or a token budget. The status
// line uses the store's longer phrasing instead, which is why this lives here.
func thinkingValueLabel(setting *api.ThinkingSetting) string {
	if setting == nil {
		return ""
	}
	switch setting.Type {
	case "effort":
		return setting.Effort
	case "budget", "budget_tokens":
		return fmt.Sprintf("budget %d tokens", setting.Budget)
	case "toggle":
		if setting.Enabled {
			return "on"
		}
		return "off"
	case "adaptive":
		return "adaptive"
	}
	return ""
}

// effectiveConfig returns the config in force, preferring a live session's over
// the local draft it was created from.
func effectiveConfig() api.SessionConfig {
	state := store.GetSession()
	if state.Session != nil {
		return state.Session.Config
	}
	return state.Config
}

func handleBudget(args []string) error {
	model, err := currentThinkingModel()
	if err != nil {
		return err
	}
	capability, err := model.requireCapability("budget")
	if err != nil {
		return err
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: /budget [tokens|default]")
	}
	if len(args) == 0 {
		cfg := effectiveConfig()
		if cfg.Thinking != nil && (cfg.Thinking.Type == "budget" || cfg.Thinking.Type == "budget_tokens") {
			store.AppendCommandOutput("/budget", "Thinking budget: "+strconv.Itoa(cfg.Thinking.Budget))
			return nil
		}
		// Without an override the model decides, so report its default and the
		// range it will accept rather than a bare capability default.
		if def, ok := model.defaultThinking(); ok && (def.Type == "budget" || def.Type == "budget_tokens") {
			store.AppendCommandOutput("/budget", fmt.Sprintf("Thinking budget: default (%d tokens) — range %d–%d", def.Budget, capability.Min, capability.Max))
			return nil
		}
		value := capability.Default
		if value == "" {
			value = store.ThinkingProviderDefault
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

// usageThinking is the argument form of /thinking. A model may support the
// on/off toggle, adaptive thinking, or both, so both spellings are always shown.
const usageThinking = "/thinking [on|off|adaptive|default]"

func handleThinking(args []string) error {
	model, err := currentThinkingModel()
	if err != nil {
		return err
	}
	_, hasToggle := model.capability("toggle")
	_, hasAdaptive := model.capability("adaptive")
	if !hasToggle && !hasAdaptive {
		return fmt.Errorf("thinking toggle/adaptive is not supported by model %s", model.model.ID)
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: %s", usageThinking)
	}
	if len(args) == 0 {
		store.AppendCommandOutput("/thinking", "Thinking: "+thinkingState(model))
		return nil
	}
	value := strings.ToLower(args[0])
	if value == "default" {
		store.SetThinking(api.ThinkingSetting{Type: "default"}, "/thinking default", "Thinking reset to default")
		return nil
	}
	if value == "adaptive" {
		if !hasAdaptive {
			return fmt.Errorf("adaptive thinking is not supported by model %s", model.model.ID)
		}
		store.SetThinking(api.ThinkingSetting{Type: "adaptive"}, "/thinking adaptive", "Thinking set to adaptive")
		return nil
	}
	if value != "on" && value != "off" {
		return fmt.Errorf("usage: %s", usageThinking)
	}
	if !hasToggle {
		return fmt.Errorf("thinking toggle is not supported by model %s", model.model.ID)
	}
	store.SetThinking(api.ThinkingSetting{Type: "toggle", Enabled: value == "on"}, "/thinking "+value, "Thinking set to "+value)
	return nil
}

// thinkingState describes what the session will do: the session override when
// there is one, then the model's own default, then the strategy default, and
// finally that the provider decides.
func thinkingState(model thinkingModel) string {
	cfg := effectiveConfig()
	if cfg.Thinking != nil {
		if label := thinkingValueLabel(cfg.Thinking); label != "" {
			return label
		}
	}
	if def, ok := model.defaultThinking(); ok {
		if label := thinkingValueLabel(def); label != "" {
			return "default (" + label + ")"
		}
	}
	if toggle, ok := model.capability("toggle"); ok {
		switch toggle.Default {
		case "true":
			return "on"
		case "false":
			return "off"
		}
	}
	return store.ThinkingProviderDefault
}
