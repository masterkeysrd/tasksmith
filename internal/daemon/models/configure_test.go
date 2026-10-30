package models

import (
	"errors"
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
			cfg, diags, err := Configure(m, Preferences{Temperature: &zero})
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

	// Valid agent thinking is applied.
	cfg, _, err := Configure(m, Preferences{AgentThinking: &api.ThinkingSetting{Type: "effort", Effort: "low"}})
	if err != nil || cfg.Thinking == nil || cfg.Thinking.Effort != "low" {
		t.Fatalf("agent thinking: %+v err=%v", cfg, err)
	}

	// Unsupported agent thinking becomes a diagnostic, not an error.
	cfg, diags, err := Configure(m, Preferences{AgentThinking: &api.ThinkingSetting{Type: "effort", Effort: "high"}})
	if err != nil || cfg.Thinking != nil || len(diags) != 1 || diags[0].Code != "thinking_unsupported" {
		t.Fatalf("agent thinking diagnostic: %+v diags=%+v err=%v", cfg, diags, err)
	}

	// Unsupported explicit session thinking is an error.
	_, _, err = Configure(m, Preferences{SessionThinking: &api.ThinkingSetting{Type: "effort", Effort: "high"}})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("session thinking should error: %v", err)
	}

	// Session thinking overrides agent thinking.
	cfg, _, err = Configure(m, Preferences{
		AgentThinking:   &api.ThinkingSetting{Type: "effort", Effort: "low"},
		SessionThinking: &api.ThinkingSetting{Type: "effort", Effort: "low"},
	})
	if err != nil || cfg.Thinking == nil || cfg.Thinking.Effort != "low" {
		t.Fatalf("session override: %+v err=%v", cfg, err)
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
	cfg, diags, err := Configure(m, Preferences{Temperature: &v, SessionThinking: &api.ThinkingSetting{Type: "budget_tokens", Budget: 1024}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Temperature != nil || len(diags) != 1 || diags[0].Code != "temperature_thinking_conflict" {
		t.Fatalf("conflict: %+v diags=%+v", cfg, diags)
	}
}
