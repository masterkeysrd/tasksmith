package models

import (
	"errors"
	"reflect"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestConfigureTemperature(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider string
		temp     *bool // model's known temperature capability; nil = unknown
		wantTemp bool
		warning  string
	}{
		{"unknown", "ollama", nil, true, "temperature_unknown"},
		{"supported", "ollama", new(true), true, ""},
		{"unsupported", "ollama", new(false), false, "temperature_unsupported"},
		{"override true", "ollama", new(true), true, ""},
		{"override false", "ollama", new(false), false, "temperature_unsupported"},
		{"workspace only true", "ollama", new(true), true, ""},
		{"workspace only false", "ollama", new(false), false, "temperature_unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Model{ID: ID("provider/model"), ProviderType: tc.provider, Limits: Limits{Context: 4000, Output: 1000}}
			if tc.temp != nil {
				m.Capabilities.Temperature = tc.temp
			}
			zero := float32(0)
			cfg, diags, err := m.Configure(Settings{Temperature: &zero})
			if err != nil {
				t.Fatal(err)
			}
			if (cfg.Temperature != nil) != tc.wantTemp {
				t.Fatalf("temperature: %+v", cfg.Temperature)
			}
			if cfg.MaxTokens != 1000 || cfg.ContextWindow != 4000 {
				t.Fatalf("limits not applied: %+v", cfg)
			}
			if tc.warning == "" && len(diags) != 0 || tc.warning != "" && (len(diags) != 1 || diags[0].Code != tc.warning) {
				t.Fatalf("diagnostics: %+v", diags)
			}
		})
	}
}

func TestConfigureThinking(t *testing.T) {
	m := Model{
		ID:           ID("provider/model"),
		ProviderType: "ollama",
		Limits:       Limits{Context: 4000, Output: 1000},
		Capabilities: Capabilities{
			Temperature: new(true),
			Thinking:    []ThinkingOption{{Type: "effort", Values: []string{"low"}}},
		},
	}

	// Valid thinking is applied.
	cfg, _, err := m.Configure(Settings{Thinking: &api.ThinkingSetting{Type: "effort", Effort: "low"}})
	if err != nil || cfg.Thinking == nil || cfg.Thinking.Effort != "low" {
		t.Fatalf("agent thinking: %+v err=%v", cfg, err)
	}

	// Unsupported thinking with StrictThinking=false becomes a diagnostic, not an error.
	cfg, diags, err := m.Configure(Settings{Thinking: &api.ThinkingSetting{Type: "effort", Effort: "high"}, StrictThinking: false})
	if err != nil || cfg.Thinking != nil || len(diags) != 1 || diags[0].Code != "thinking_unsupported" {
		t.Fatalf("agent thinking diagnostic: %+v diags=%+v err=%v", cfg, diags, err)
	}

	// Unsupported thinking with StrictThinking=true is an error.
	_, _, err = m.Configure(Settings{Thinking: &api.ThinkingSetting{Type: "effort", Effort: "high"}, StrictThinking: true})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("session thinking should error: %v", err)
	}

	// Valid strict thinking is applied.
	cfg, _, err = m.Configure(Settings{Thinking: &api.ThinkingSetting{Type: "effort", Effort: "low"}, StrictThinking: true})
	if err != nil || cfg.Thinking == nil || cfg.Thinking.Effort != "low" {
		t.Fatalf("session override: %+v err=%v", cfg, err)
	}
}

func TestDefaultThinking(t *testing.T) {
	think := func(opts ...ThinkingOption) Model {
		return Model{ID: ID("provider/model"), ProviderType: "ollama", Capabilities: Capabilities{Thinking: opts}}
	}
	for _, tc := range []struct {
		name  string
		model Model
		want  *api.ThinkingSetting
	}{
		{"none", think(), nil},
		{"effort default", think(ThinkingOption{Type: "effort", Values: []string{"low", "high"}, Default: "high"}), &api.ThinkingSetting{Type: "effort", Effort: "high"}},
		{"isDefault wins over earlier option", think(
			ThinkingOption{Type: "effort", Default: "low"},
			ThinkingOption{Type: "budget_tokens", Default: "4096", IsDefault: true},
		), &api.ThinkingSetting{Type: "budget", Budget: 4096}},
		{"isDefault effort without default falls back to first value", think(ThinkingOption{Type: "effort", Values: []string{"minimal", "low"}, IsDefault: true}), &api.ThinkingSetting{Type: "effort", Effort: "minimal"}},
		{"isDefault budget without default falls back to min", think(ThinkingOption{Type: "budget_tokens", Min: 1024, Max: 16000, IsDefault: true}), &api.ThinkingSetting{Type: "budget", Budget: 1024}},
		{"budget without usable default", think(ThinkingOption{Type: "budget_tokens", Max: 16000, IsDefault: true}), nil},
		{"toggle true", think(ThinkingOption{Type: "toggle", Default: "true"}), &api.ThinkingSetting{Type: "toggle", Enabled: true}},
		{"toggle isDefault without default", think(ThinkingOption{Type: "toggle", IsDefault: true}), &api.ThinkingSetting{Type: "toggle", Enabled: true}},
		{"toggle false stays unset", think(ThinkingOption{Type: "toggle", Default: "false"}), nil},
		{"adaptive isDefault", think(ThinkingOption{Type: "adaptive", IsDefault: true}), &api.ThinkingSetting{Type: "adaptive"}},
		{"adaptive without isDefault", think(ThinkingOption{Type: "adaptive"}), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.model.DefaultThinking()
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("DefaultThinking() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestConfigureAppliesModelDefaultThinking(t *testing.T) {
	m := Model{
		ID:           ID("provider/model"),
		ProviderType: "ollama",
		Limits:       Limits{Context: 4000, Output: 1000},
		Capabilities: Capabilities{
			Temperature: new(true),
			Thinking: []ThinkingOption{
				{Type: "effort", Values: []string{"low", "medium", "high"}, Default: "medium", IsDefault: true},
			},
		},
	}

	// No session or agent setting: the model's declared default is applied.
	cfg, diags, err := m.Configure(Settings{})
	if err != nil || len(diags) != 0 {
		t.Fatalf("model default: diags=%+v err=%v", diags, err)
	}
	if cfg.Thinking == nil || cfg.Thinking.Effort != "medium" {
		t.Fatalf("model default thinking: %+v", cfg.Thinking)
	}

	// A session override wins over the model default.
	cfg, _, err = m.Configure(Settings{Thinking: &api.ThinkingSetting{Type: "effort", Effort: "low"}, StrictThinking: true})
	if err != nil || cfg.Thinking == nil || cfg.Thinking.Effort != "low" {
		t.Fatalf("session override over model default: %+v err=%v", cfg, err)
	}

	// A default the model cannot honor is ignored silently instead of failing
	// the turn or warning about an override the user never set.
	broken := m
	broken.Capabilities.Thinking = []ThinkingOption{{Type: "effort", Values: []string{"low"}, Default: "unheard-of", IsDefault: true}}
	cfg, diags, err = broken.Configure(Settings{})
	if err != nil || len(diags) != 0 || cfg.Thinking != nil {
		t.Fatalf("unusable model default: cfg=%+v diags=%+v err=%v", cfg, diags, err)
	}

	// Temperature still conflicts with the model default on Anthropic.
	anth := Model{
		ID:           ID("anthropic/claude"),
		ProviderType: "anthropic",
		Limits:       Limits{Context: 4000, Output: 1000},
		Capabilities: Capabilities{
			Temperature: new(true),
			Thinking:    []ThinkingOption{{Type: "budget_tokens", Min: 1024, Default: "2048", IsDefault: true}},
		},
	}
	v := float32(0.5)
	cfg, diags, err = anth.Configure(Settings{Temperature: &v})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Thinking == nil || cfg.Temperature != nil || len(diags) != 1 || diags[0].Code != "temperature_thinking_conflict" {
		t.Fatalf("model default vs temperature: %+v diags=%+v", cfg, diags)
	}
}

func TestEffectiveThinkingPrecedence(t *testing.T) {
	m := Model{
		ID:           ID("provider/model"),
		ProviderType: "ollama",
		Capabilities: Capabilities{
			Thinking: []ThinkingOption{{Type: "effort", Values: []string{"low", "high"}, Default: "high", IsDefault: true}},
		},
	}
	modelDefault := &api.ThinkingSetting{Type: "effort", Effort: "high"}
	agent := &api.ThinkingSetting{Type: "effort", Effort: "low"}
	session := &api.ThinkingSetting{Type: "effort", Effort: "high"}

	if got := m.EffectiveThinking(nil, nil); !reflect.DeepEqual(got, modelDefault) {
		t.Fatalf("model default: %+v", got)
	}
	if got := m.EffectiveThinking(nil, agent); !reflect.DeepEqual(got, agent) {
		t.Fatalf("agent default: %+v", got)
	}
	if got := m.EffectiveThinking(session, agent); !reflect.DeepEqual(got, session) {
		t.Fatalf("session override: %+v", got)
	}
	// A "default" setting means "no override", so the model default applies.
	if got := m.EffectiveThinking(&api.ThinkingSetting{Type: "default"}, agent); !reflect.DeepEqual(got, agent) {
		t.Fatalf("default setting: %+v", got)
	}
	// An agent default the model cannot honor leaves thinking unset, matching
	// what Configure sends to the provider.
	unsupported := &api.ThinkingSetting{Type: "budget", Budget: 512}
	if got := m.EffectiveThinking(nil, unsupported); got != nil {
		t.Fatalf("unsupported agent default: %+v", got)
	}
}

func TestConfigureAnthropicThinkingTemperatureConflict(t *testing.T) {
	m := Model{
		ID:           ID("anthropic/claude"),
		ProviderType: "anthropic",
		Limits:       Limits{Context: 4000, Output: 1000},
		Capabilities: Capabilities{
			Temperature: new(true),
			Thinking:    []ThinkingOption{{Type: "budget_tokens", Min: 100, Max: 10000}},
		},
	}
	v := float32(0.5)
	// A supported budget thinking combined with temperature on Anthropic is a
	// conflict: the temperature is dropped with a diagnostic.
	cfg, diags, err := m.Configure(Settings{
		Temperature:    &v,
		Thinking:       &api.ThinkingSetting{Type: "budget_tokens", Budget: 1024},
		StrictThinking: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Temperature != nil || len(diags) != 1 || diags[0].Code != "temperature_thinking_conflict" {
		t.Fatalf("conflict: %+v diags=%+v", cfg, diags)
	}
}

func TestConfigureAdaptiveThinking(t *testing.T) {
	// A model whose profile advertises the "adaptive" reasoning option applies
	// adaptive thinking end-to-end.
	m := Model{
		ID:           ID("provider/model"),
		ProviderType: "ollama",
		Limits:       Limits{Context: 4000, Output: 1000},
		Capabilities: Capabilities{
			Temperature: new(true),
			Thinking:    []ThinkingOption{{Type: "adaptive"}},
		},
	}

	cfg, _, err := m.Configure(Settings{Thinking: &api.ThinkingSetting{Type: "adaptive"}})
	if err != nil {
		t.Fatalf("adaptive thinking err: %v", err)
	}
	if cfg.Thinking == nil || !cfg.Thinking.Adaptive || !cfg.Thinking.Enabled {
		t.Fatalf("expected adaptive thinking enabled, got %+v", cfg.Thinking)
	}

	// The same model with StrictThinking=true still applies adaptive thinking.
	cfg, _, err = m.Configure(Settings{Thinking: &api.ThinkingSetting{Type: "adaptive"}, StrictThinking: true})
	if err != nil {
		t.Fatalf("strict adaptive thinking err: %v", err)
	}
	if cfg.Thinking == nil || !cfg.Thinking.Adaptive {
		t.Fatalf("expected strict adaptive thinking enabled, got %+v", cfg.Thinking)
	}

	// A model without the adaptive option rejects strict adaptive thinking.
	unsupported := Model{
		ID:           ID("provider/model"),
		ProviderType: "ollama",
		Limits:       Limits{Context: 4000, Output: 1000},
		Capabilities: Capabilities{
			Temperature: new(true),
			Thinking:    []ThinkingOption{{Type: "effort", Values: []string{"low"}}},
		},
	}
	_, _, err = unsupported.Configure(Settings{Thinking: &api.ThinkingSetting{Type: "adaptive"}, StrictThinking: true})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected unsupported adaptive thinking to error, got %v", err)
	}

	// On Anthropic, adaptive thinking combined with temperature is a conflict:
	// the temperature is dropped with a diagnostic.
	anth := Model{
		ID:           ID("anthropic/claude"),
		ProviderType: "anthropic",
		Limits:       Limits{Context: 4000, Output: 1000},
		Capabilities: Capabilities{
			Temperature: new(true),
			Thinking:    []ThinkingOption{{Type: "adaptive"}},
		},
	}
	v := float32(0.5)
	cfg, diags, err := anth.Configure(Settings{
		Temperature:    &v,
		Thinking:       &api.ThinkingSetting{Type: "adaptive"},
		StrictThinking: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Thinking == nil || !cfg.Thinking.Adaptive {
		t.Fatalf("expected adaptive thinking enabled, got %+v", cfg.Thinking)
	}
	if cfg.Temperature != nil || len(diags) != 1 || diags[0].Code != "temperature_thinking_conflict" {
		t.Fatalf("conflict: %+v diags=%+v", cfg, diags)
	}
}
