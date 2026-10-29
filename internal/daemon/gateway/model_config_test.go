package gateway

import (
	"context"
	"testing"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
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
			spec := ModelSpec{ID: "model", Limits: ModelLimits{Context: 4000, Output: 1000}, Capabilities: &ModelCapabilities{Temperature: tc.override}}
			g := &gatewayImpl{providers: map[string]*providerInstance{"provider": {cfg: ProviderConfig{Name: "provider", Type: "ollama"}, llmProv: p, models: map[string]ModelSpec{"model": spec}}}}
			zero := float32(0)
			requested := &llm.ModelConfig{Temperature: &zero, Thinking: &llm.ThinkingConfig{Enabled: true, Effort: "high"}}
			resolved, err := g.ResolveConfig("provider", "model", ModelSettings{Config: requested})
			if err != nil {
				t.Fatal(err)
			}
			if (resolved.Config.Temperature != nil) != tc.wantTemp {
				t.Fatalf("temperature: %+v", resolved.Config.Temperature)
			}
			if requested.Temperature == nil || *requested.Temperature != 0 {
				t.Fatal("request mutated")
			}
			if resolved.Config.MaxTokens != 1000 || resolved.Config.ContextWindow != 4000 || resolved.Config.Thinking == nil {
				t.Fatalf("config: %+v", resolved.Config)
			}
			if tc.warning == "" && len(resolved.Diagnostics) != 0 || tc.warning != "" && (len(resolved.Diagnostics) != 1 || resolved.Diagnostics[0].Code != tc.warning) {
				t.Fatalf("diagnostics: %+v", resolved.Diagnostics)
			}
			model, err := g.ResolveLLM(context.Background(), "provider", "model", WithLLMConfig(requested))
			if err != nil {
				t.Fatal(err)
			}
			stream, err := model.Stream(context.Background(), []message.Message{message.NewUserText("test")})
			if err != nil {
				t.Fatal(err)
			}
			for range stream {
			}
			if (p.request.Temperature != nil) != tc.wantTemp || p.request.MaxTokens != 1000 || p.request.ContextWindow != 4000 {
				t.Fatalf("request: %+v", p.request)
			}
		})
	}
}

func TestEffectiveProfileAndSettings(t *testing.T) {
	base := &profile.Profile{Limits: profile.Limits{Context: 100, Output: 10, Dimensions: 1536}, Capabilities: profile.Capabilities{Completion: true, Decision: true, Embedding: true, Attachment: true, ToolCall: true, Temperature: true, ReasoningOptions: []profile.ReasoningOption{{Type: "effort", Values: []string{"low"}}}}, Modalities: profile.Modalities{Inputs: []profile.Modality{"text"}, Outputs: []profile.Modality{"text"}}}
	if p := MergeProfile(&ModelSpec{Capabilities: &ModelCapabilities{Dimensions: []int{384}, Modalities: &profile.Modalities{Inputs: []profile.Modality{}}}}, base); p.Limits.Dimensions != 384 || p.Modalities.Inputs == nil || len(p.Modalities.Inputs) != 0 {
		t.Fatalf("explicit capability overrides: %+v", p)
	}
	copy := MergeProfile(nil, base)
	copy.Modalities.Inputs[0] = "audio"
	copy.Capabilities.ReasoningOptions[0].Values[0] = "changed"
	if base.Modalities.Inputs[0] != "text" || base.Capabilities.ReasoningOptions[0].Values[0] != "low" {
		t.Fatal("merged profile mutated provider metadata")
	}
	caps := &ModelCapabilities{Completion: new(false), Decision: new(false), Embedding: new(false), Attachment: new(false), Tools: new(false), Temperature: new(false), StructuredOutput: new(true), Dimensions: []int{384, 768}, Thinking: []profile.ReasoningOption{{Type: "effort", Values: []string{"high"}}}, Modalities: &profile.Modalities{Inputs: []profile.Modality{"image"}}}
	spec := ModelSpec{ID: "model", Limits: ModelLimits{Context: 200, Output: 20, Dimensions: 384}, Capabilities: caps}
	inst := &providerInstance{cfg: ProviderConfig{Name: "local", Type: "ollama"}, llmProv: &thinkingTestProvider{modelProfile: base}, models: map[string]ModelSpec{"model": spec}}
	g := &gatewayImpl{providers: map[string]*providerInstance{"local": inst}}
	value := float32(0)
	settings := ModelSettings{Config: &llm.ModelConfig{Temperature: &value}, AgentThinking: &api.ThinkingSetting{Type: "effort", Effort: "low"}, SessionThinking: &api.ThinkingSetting{Type: "effort", Effort: "high"}}
	resolved, err := g.ResolveConfig("", "local/model", settings)
	if err != nil {
		t.Fatal(err)
	}
	p := resolved.Profile.Profile
	if p.Capabilities.Completion || p.Capabilities.Decision || p.Capabilities.Embedding || p.Capabilities.Attachment || p.Capabilities.ToolCall || p.Capabilities.Temperature || p.Limits.Context != 200 || p.Limits.Output != 20 || p.Limits.Dimensions != 384 || len(p.Modalities.Inputs) != 1 || p.Modalities.Inputs[0] != "image" || len(p.Modalities.Outputs) != 1 || p.Modalities.Outputs[0] != "text" {
		t.Fatalf("effective profile: %+v", p)
	}
	if resolved.Profile.StructuredOutput == nil || !*resolved.Profile.StructuredOutput || len(resolved.Profile.Dimensions) != 2 || resolved.Config.Thinking == nil || resolved.Config.Thinking.Effort != "high" || resolved.Config.Temperature != nil {
		t.Fatalf("effective settings: %+v", resolved)
	}
	settings.SessionThinking = nil
	resolved, err = g.ResolveConfig("local", "model", settings)
	if err != nil || resolved.Config.Thinking != nil || len(resolved.Diagnostics) != 2 || resolved.Diagnostics[0].Code != "thinking_unsupported" {
		t.Fatalf("unsupported agent defaults: %+v, %v", resolved, err)
	}
	settings.SessionThinking = &api.ThinkingSetting{Type: "effort", Effort: "low"}
	if _, err := g.ResolveConfig("local", "model", settings); err == nil {
		t.Fatal("unsupported explicit thinking accepted")
	}
	if _, err := g.ResolveConfig("", "unknown/model", settings); err == nil {
		t.Fatal("unregistered provider accepted")
	}
}

func TestAnthropicThinkingTemperatureConflict(t *testing.T) {
	v := float32(0.5)
	p := EffectiveProfile{ProviderType: "anthropic", Profile: &profile.Profile{Capabilities: profile.Capabilities{Temperature: true}}, TemperatureKnown: true}
	for _, thinking := range []*llm.ThinkingConfig{{Budget: 1024}, {Adaptive: true}} {
		r, err := ResolveSettings(p, ModelSettings{Config: &llm.ModelConfig{Temperature: &v, Thinking: thinking}})
		if err != nil {
			t.Fatal(err)
		}
		if r.Config.Temperature != nil || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "temperature_thinking_conflict" {
			t.Fatalf("result: %+v", r)
		}
	}
}
