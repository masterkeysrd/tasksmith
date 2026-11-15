package models

import (
	"testing"

	"github.com/masterkeysrd/loom/profile"
)

func TestProfileRoundTrip(t *testing.T) {
	base := profile.Profile{
		Name:       "Base",
		Limits:     profile.Limits{Context: 100, Output: 10, Dimensions: 384},
		Modalities: profile.Modalities{Inputs: []profile.Modality{"text", "image"}, Outputs: []profile.Modality{"text"}},
		Capabilities: profile.Capabilities{
			Completion:  true,
			Decision:    true,
			Embedding:   true,
			Attachment:  true,
			ToolCall:    true,
			Temperature: true,
			ReasoningOptions: []profile.ReasoningOption{
				{Type: "effort", Values: []string{"low", "high"}, Default: "low"},
				{Type: "budget_tokens", Min: 100, Max: 1000},
			},
		},
		Pricing: profile.Pricing{
			Input: 1, Output: 2, CacheRead: 0.1, CacheWrite: 0.2, Reasoning: 0.3,
			TieredLimits: []profile.TierPricing{{Input: 3, Output: 4, Condition: profile.TierCondition{Type: profile.TierConditionContext, Size: 200000}}},
		},
	}
	m := fromProfile(&base, ID("p/m"), "ollama")

	if m.Name != "Base" || m.ProviderType != "ollama" {
		t.Fatalf("fromProfile: %+v", m)
	}
	if m.Limits.Context != 100 || m.Limits.Output != 10 || m.Limits.Dimensions != 384 {
		t.Fatalf("limits: %+v", m.Limits)
	}
	if m.Capabilities.Completion == nil || !*m.Capabilities.Completion {
		t.Fatalf("completion: %+v", m.Capabilities.Completion)
	}
	if len(m.Capabilities.Thinking) != 2 {
		t.Fatalf("thinking: %+v", m.Capabilities.Thinking)
	}
	if m.Pricing == nil || m.Pricing.Input != 1 || m.Pricing.Reasoning != 0.3 || len(m.Pricing.Tiers) != 1 {
		t.Fatalf("pricing: %+v", m.Pricing)
	}
	if m.Pricing.Tiers[0].ConditionSize != 200000 {
		t.Fatalf("tier: %+v", m.Pricing.Tiers[0])
	}

	// Profile() should reproduce the loom view.
	p := m.Profile()
	if p.Name != "Base" || p.Limits.Context != 100 || p.Limits.Dimensions != 384 {
		t.Fatalf("profile: %+v", p)
	}
	if !p.Capabilities.Completion || !p.Capabilities.ToolCall || !p.Capabilities.Temperature {
		t.Fatalf("capabilities: %+v", p.Capabilities)
	}
	if len(p.Capabilities.ReasoningOptions) != 2 {
		t.Fatalf("reasoning options: %+v", p.Capabilities.ReasoningOptions)
	}
	if p.Pricing.Input != 1 || len(p.Pricing.TieredLimits) != 1 || p.Pricing.TieredLimits[0].Condition.Size != 200000 {
		t.Fatalf("pricing: %+v", p.Pricing)
	}
	if len(p.Modalities.Inputs) != 2 || p.Modalities.Inputs[0] != "text" {
		t.Fatalf("modalities: %+v", p.Modalities)
	}
}

func TestProfileUnknownCapabilities(t *testing.T) {
	// A model with unknown capabilities (no layers) produces a profile with the
	// default capability semantics: completion true, others false.
	m := Model{ID: ID("p/m")}
	p := m.Profile()
	if !p.Capabilities.Completion {
		t.Fatalf("completion should default to true")
	}
	if p.Capabilities.ToolCall || p.Capabilities.Temperature {
		t.Fatalf("unknown capabilities should default to false: %+v", p.Capabilities)
	}
}

func TestFromProfileNil(t *testing.T) {
	m := fromProfile(nil, ID("p/m"), "ollama")
	if m.Name != "" || m.ID != ID("p/m") {
		t.Fatalf("nil base: %+v", m)
	}
	if m.Pricing != nil {
		t.Fatalf("nil base should have nil pricing")
	}
}
