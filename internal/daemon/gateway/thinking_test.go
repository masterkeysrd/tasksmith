package gateway

import (
	"context"
	"reflect"
	"testing"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
)

type thinkingTestProvider struct {
	request      *llm.Request
	modelProfile *profile.Profile
}

func (p *thinkingTestProvider) Name() string                    { return "test" }
func (p *thinkingTestProvider) ListProfiles() []profile.Profile { return nil }
func (p *thinkingTestProvider) GetProfile(string) (profile.Profile, bool) {
	if p.modelProfile != nil {
		return *p.modelProfile, true
	}
	return profile.Profile{}, false
}
func (p *thinkingTestProvider) SearchProfiles(string) []profile.Profile { return nil }
func (p *thinkingTestProvider) OverrideProfile(string, profile.Profile) {}
func (p *thinkingTestProvider) Stream(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
	p.request = req
	return func(yield func(message.AssistantChunk, error) bool) {
		yield(message.AssistantChunk{Done: true}, nil)
	}, nil
}

func TestThinkingCapabilitiesPreserved(t *testing.T) {
	options := []profile.ReasoningOption{
		{Type: "effort", Values: []string{"minimal", "high"}, Default: "minimal"},
		{Type: "budget_tokens", Min: 100, Max: 1000},
		{Type: "toggle", Default: "false"},
	}
	for _, providerType := range []string{"ollama", "anthropic", "openai", "genai", "antigravity", "custom"} {
		for _, source := range []string{"profile", "workspace"} {
			t.Run(providerType+"/"+source, func(t *testing.T) {
				provider := &thinkingTestProvider{}
				spec := ModelSpec{ID: "model"}
				if source == "profile" {
					provider.modelProfile = &profile.Profile{Capabilities: profile.Capabilities{ReasoningOptions: options}}
				} else {
					spec.Capabilities = &ModelCapabilities{Thinking: options}
				}
				inst := &providerInstance{cfg: ProviderConfig{Name: "test", Type: providerType}, llmProv: provider, models: map[string]ModelSpec{"model": spec}}
				gw := &gatewayImpl{providers: map[string]*providerInstance{"test": inst}}
				got, ok := gw.ModelProfile("test/model")
				if !ok || !reflect.DeepEqual(got.Capabilities.ReasoningOptions, options) {
					t.Fatalf("thinking options changed: %+v", got)
				}
			})
		}
	}
}

func TestThinkingProfileAndRequest(t *testing.T) {
	base := &profile.Profile{Capabilities: profile.Capabilities{ReasoningOptions: []profile.ReasoningOption{{Type: "effort", Values: []string{"low", "high"}}}}}
	spec := ModelSpec{ID: "model", Capabilities: &ModelCapabilities{Thinking: []profile.ReasoningOption{{Type: "effort", Values: []string{"minimal"}, Default: "minimal"}}}}
	merged := MergeProfile(&spec, base)
	if got := merged.Capabilities.ReasoningOptions; len(got) != 1 || len(got[0].Values) != 1 || got[0].Values[0] != "minimal" {
		t.Fatalf("workspace thinking override: %+v", got)
	}
	provider := &thinkingTestProvider{}
	inst := &providerInstance{cfg: ProviderConfig{Name: "test", Type: "ollama", Models: []ModelSpec{spec}}, llmProv: provider, models: map[string]ModelSpec{"model": spec}}
	gw := &gatewayImpl{providers: map[string]*providerInstance{"test": inst}, providerList: []*providerInstance{inst}}
	model, err := gw.ResolveLLM(context.Background(), "test/model", WithLLMConfig(&llm.ModelConfig{Thinking: &llm.ThinkingConfig{Enabled: true, Effort: "minimal"}}))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := model.Stream(context.Background(), []message.Message{message.NewUserText("test")})
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	if provider.request == nil || provider.request.Thinking == nil || provider.request.Thinking.Effort != "minimal" {
		t.Fatalf("thinking did not reach provider: %+v", provider.request)
	}
}
