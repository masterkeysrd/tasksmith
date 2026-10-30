package gateway

import (
	"context"
	"reflect"
	"testing"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
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
				var manifest *workspace.ProviderModel
				if source == "profile" {
					provider.modelProfile = &profile.Profile{Capabilities: profile.Capabilities{ReasoningOptions: options}}
				} else {
					manifest = &workspace.ProviderModel{ID: "model", Capabilities: &workspace.ModelCapabilities{Thinking: []workspace.ThinkingCapability{{Type: "effort", AllowedEfforts: []string{"minimal", "high"}, Default: "minimal"}, {Type: "budget_tokens", MaxBudgetTokens: 1000}, {Type: "toggle", Default: false}}}}
				}
				inst := &providerInstance{cfg: ProviderConfig{Name: "test", Type: providerType}, llmProv: provider}
				gw := &gatewayImpl{providers: map[string]*providerInstance{"test": inst}}
				base, found := gw.GetProfile("test/model")
				var baseProfile *profile.Profile
				if found {
					baseProfile = &base
				}
				got := models.Merge("test/model", providerType, manifest, baseProfile).Profile().Capabilities.ReasoningOptions
				if source == "profile" && !reflect.DeepEqual(got, options) {
					t.Fatalf("thinking options changed: %+v", got)
				}
				if source == "workspace" && (len(got) != 3 || !reflect.DeepEqual(got[0].Values, options[0].Values) || got[1].Max != 1000) {
					t.Fatalf("workspace thinking options changed: %+v", got)
				}
			})
		}
	}
}

func TestThinkingProfileAndRequest(t *testing.T) {
	base := &profile.Profile{Capabilities: profile.Capabilities{Completion: true, ReasoningOptions: []profile.ReasoningOption{{Type: "effort", Values: []string{"low", "high"}}}}}
	spec := &workspace.ProviderModel{ID: "model", Capabilities: &workspace.ModelCapabilities{Thinking: []workspace.ThinkingCapability{{Type: "effort", AllowedEfforts: []string{"minimal"}, Default: "minimal"}}}}
	merged := models.Merge("test/model", "ollama", spec, base)
	if got := merged.Capabilities.Thinking; len(got) != 1 || len(got[0].Values) != 1 || got[0].Values[0] != "minimal" {
		t.Fatalf("workspace thinking override: %+v", got)
	}
	provider := &thinkingTestProvider{}
	inst := &providerInstance{cfg: ProviderConfig{Name: "test", Type: "ollama"}, llmProv: provider}
	gw := &gatewayImpl{providers: map[string]*providerInstance{"test": inst}}
	model, err := gw.LLM(context.Background(), merged, llm.ModelConfig{Thinking: &llm.ThinkingConfig{Enabled: true, Effort: "minimal"}})
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
