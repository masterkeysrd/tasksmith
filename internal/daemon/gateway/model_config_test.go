package gateway

import (
	"context"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/warp"
)

func TestEffectiveTemperatureConfig(t *testing.T) {
	for _, tc := range []struct {
		name     string
		base     *profile.Profile
		override *bool
		wantTemp bool
		warning  string
	}{
		{"unknown", nil, nil, true, "temperature_unknown"},
		{"supported", &profile.Profile{Capabilities: profile.Capabilities{Temperature: true}}, nil, true, ""},
		{"unsupported", &profile.Profile{}, nil, false, "temperature_unsupported"},
		{"override true", &profile.Profile{}, new(true), true, ""},
		{"override false", &profile.Profile{Capabilities: profile.Capabilities{Temperature: true}}, new(false), false, "temperature_unsupported"},
		{"workspace only true", nil, new(true), true, ""},
		{"workspace only false", nil, new(false), false, "temperature_unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &thinkingTestProvider{modelProfile: tc.base}
			spec := &warp.ProviderModel{ID: "model", Limits: warp.ProviderModelLimits{Context: 4000, Output: 1000}, Capabilities: &warp.ProviderModelCapabilities{Completion: new(true), Temperature: tc.override}}
			effective := models.Merge("provider/model", "ollama", spec, tc.base)
			g := &gatewayImpl{providers: map[string]*providerInstance{"provider": {cfg: ProviderConfig{Name: "provider", Type: "ollama"}, llmProv: p}}}
			zero := float32(0)
			resolved, diagnostics, err := effective.Configure(models.Settings{Temperature: &zero})
			if err != nil {
				t.Fatal(err)
			}
			if (resolved.Temperature != nil) != tc.wantTemp {
				t.Fatalf("temperature: %+v", resolved.Temperature)
			}
			if zero != 0 {
				t.Fatal("request mutated")
			}
			if resolved.MaxTokens != 1000 || resolved.ContextWindow != 4000 {
				t.Fatalf("config: %+v", resolved)
			}
			if tc.warning == "" && len(diagnostics) != 0 || tc.warning != "" && (len(diagnostics) != 1 || diagnostics[0].Code != tc.warning) {
				t.Fatalf("diagnostics: %+v", diagnostics)
			}
			model, err := g.LLM(context.Background(), effective, resolved)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := model.Stream(context.Background(), []message.Message{message.NewUserText("test")})
			if err != nil {
				t.Fatal(err)
			}
			for _, err := range stream {
				if err != nil {
					t.Fatal(err)
				}
			}
			if (p.request.Temperature != nil) != tc.wantTemp || p.request.MaxTokens != 1000 || p.request.ContextWindow != 4000 {
				t.Fatalf("request: %+v", p.request)
			}
		})
	}
}

func TestEffectiveProfileAndSettings(t *testing.T) {
	base := &profile.Profile{Limits: profile.Limits{Context: 100, Output: 10, Dimensions: 1536}, Capabilities: profile.Capabilities{Completion: true, Decision: true, Embedding: true, Attachment: true, ToolCall: true, Temperature: true, ReasoningOptions: []profile.ReasoningOption{{Type: "effort", Values: []string{"low"}}}}, Modalities: profile.Modalities{Inputs: []profile.Modality{"text"}, Outputs: []profile.Modality{"text"}}}
	caps := &warp.ProviderModelCapabilities{Completion: new(false), Decision: new(false), Embedding: new(false), Attachment: new(false), Tools: new(false), Temperature: new(false), StructuredOutput: new(true), Dimensions: []int{384, 768}, Thinking: []warp.ProviderModelThinkingCapability{{Type: "effort", AllowedEfforts: []string{"high"}}}, Modalities: &warp.ProviderModelModalities{Input: []string{"image"}}}
	spec := &warp.ProviderModel{ID: "model", Limits: warp.ProviderModelLimits{Context: 200, Output: 20, Dimensions: 384}, Capabilities: caps}
	model := models.Merge("local/model", "ollama", spec, base)
	p := model.Profile()
	if p.Capabilities.Completion || p.Capabilities.Decision || p.Capabilities.Embedding || p.Capabilities.Attachment || p.Capabilities.ToolCall || p.Capabilities.Temperature || p.Limits.Context != 200 || p.Limits.Output != 20 || p.Limits.Dimensions != 384 || len(p.Modalities.Inputs) != 1 || p.Modalities.Inputs[0] != "image" || len(p.Modalities.Outputs) != 1 || p.Modalities.Outputs[0] != "text" {
		t.Fatalf("effective profile: %+v", p)
	}
	value := float32(0)
	settings := models.Settings{Temperature: &value, Thinking: &api.ThinkingSetting{Type: "effort", Effort: "high"}, StrictThinking: true}
	resolved, diagnostics, err := model.Configure(settings)
	if err != nil || model.Capabilities.StructuredOutput == nil || !*model.Capabilities.StructuredOutput || len(model.Capabilities.Dimensions) != 2 || resolved.Thinking == nil || resolved.Thinking.Effort != "high" || resolved.Temperature != nil {
		t.Fatalf("effective settings: %+v, %+v, %v", resolved, diagnostics, err)
	}
	settings = models.Settings{Temperature: &value, Thinking: &api.ThinkingSetting{Type: "effort", Effort: "low"}, StrictThinking: false}
	resolved, diagnostics, err = model.Configure(settings)
	if err != nil || resolved.Thinking != nil || len(diagnostics) != 2 || diagnostics[0].Code != "thinking_unsupported" {
		t.Fatalf("unsupported agent defaults: %+v, %+v, %v", resolved, diagnostics, err)
	}
	settings = models.Settings{Temperature: &value, Thinking: &api.ThinkingSetting{Type: "effort", Effort: "low"}, StrictThinking: true}
	if _, _, err := model.Configure(settings); err == nil {
		t.Fatal("unsupported explicit thinking accepted")
	}
}

func TestAnthropicThinkingTemperatureConflict(t *testing.T) {
	v := float32(0.5)
	m := models.Model{ProviderType: "anthropic", Capabilities: models.Capabilities{Temperature: new(true), Thinking: []models.ThinkingOption{{Type: "budget_tokens", Min: 1, Max: 4096}}}}
	cfg, diagnostics, err := m.Configure(models.Settings{Temperature: &v, Thinking: &api.ThinkingSetting{Type: "budget", Budget: 1024}, StrictThinking: true})
	if err != nil || cfg.Temperature != nil || len(diagnostics) != 1 || diagnostics[0].Code != "temperature_thinking_conflict" {
		t.Fatalf("result: %+v, %+v, %v", cfg, diagnostics, err)
	}
}
