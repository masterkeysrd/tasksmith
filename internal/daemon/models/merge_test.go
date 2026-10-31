package models

import (
	"testing"

	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/warp"
)

func TestMergeLimitsFallback(t *testing.T) {
	// Neither layer sets limits: fallback 128000/16384 applied once.
	m := Merge(ID("p/m"), "ollama", nil, nil)
	if m.Limits.Context != 128000 || m.Limits.Output != 16384 {
		t.Fatalf("fallback limits: %+v", m.Limits)
	}

	// Base sets context only; output falls back.
	base := &profile.Profile{Limits: profile.Limits{Context: 200000}}
	m = Merge(ID("p/m"), "ollama", nil, base)
	if m.Limits.Context != 200000 || m.Limits.Output != 16384 {
		t.Fatalf("partial base limits: %+v", m.Limits)
	}

	// Manifest wins over base for scalars.
	manifest := &warp.ProviderModel{ID: "m", Limits: warp.ProviderModelLimits{Context: 100, Output: 10}}
	m = Merge(ID("p/m"), "ollama", manifest, base)
	if m.Limits.Context != 100 || m.Limits.Output != 10 {
		t.Fatalf("manifest scalar precedence: %+v", m.Limits)
	}
}

func TestMergeCapabilitiesFirstNonNil(t *testing.T) {
	// Unknown vs false: a nil capability stays nil (unknown) when no layer sets it.
	m := Merge(ID("p/m"), "ollama", nil, nil)
	if m.Capabilities.Completion != nil {
		t.Fatalf("completion should be unknown (nil): %+v", m.Capabilities)
	}
	// Completion defaults to true via SupportsCompletion.
	if !m.Capabilities.SupportsCompletion() {
		t.Fatalf("unknown completion should default to true")
	}
	if m.Capabilities.SupportsDecision() {
		t.Fatalf("unknown decision should default to false")
	}

	// Manifest sets a known false; base would say true. Manifest wins.
	base := &profile.Profile{Capabilities: profile.Capabilities{Decision: true, Temperature: true}}
	manifest := &warp.ProviderModel{ID: "m", Capabilities: &warp.ProviderModelCapabilities{Decision: new(false)}}
	m = Merge(ID("p/m"), "ollama", manifest, base)
	if m.Capabilities.Decision == nil || *m.Capabilities.Decision {
		t.Fatalf("manifest false should override base true: %+v", m.Capabilities.Decision)
	}
	// Temperature not set by manifest, so base's known true wins.
	if m.Capabilities.Temperature == nil || !*m.Capabilities.Temperature {
		t.Fatalf("base temperature should fill gap: %+v", m.Capabilities.Temperature)
	}
}

func TestMergeListsReplaced(t *testing.T) {
	base := &profile.Profile{Capabilities: profile.Capabilities{ReasoningOptions: []profile.ReasoningOption{{Type: "effort", Values: []string{"low"}}}}}
	manifest := &warp.ProviderModel{ID: "m", Capabilities: &warp.ProviderModelCapabilities{
		Thinking: []warp.ProviderModelThinkingCapability{{Type: "effort", AllowedEfforts: []string{"high"}}},
	}}
	m := Merge(ID("p/m"), "ollama", manifest, base)
	if len(m.Capabilities.Thinking) != 1 || m.Capabilities.Thinking[0].Values[0] != "high" {
		t.Fatalf("thinking list should be replaced by manifest: %+v", m.Capabilities.Thinking)
	}

	// Modalities replaced, not merged.
	manifest = &warp.ProviderModel{ID: "m", Capabilities: &warp.ProviderModelCapabilities{
		Modalities: &warp.ProviderModelModalities{Input: []string{"image"}},
	}}
	m = Merge(ID("p/m"), "ollama", manifest, base)
	if len(m.Modalities.Input) != 1 || m.Modalities.Input[0] != "image" {
		t.Fatalf("modalities should be replaced by manifest: %+v", m.Modalities)
	}
}

func TestMergePricingWholeObject(t *testing.T) {
	// Base pricing with tiers; manifest pricing without. Manifest wins whole.
	base := &profile.Profile{Pricing: profile.Pricing{
		Input: 1, Output: 2,
		TieredLimits: []profile.TierPricing{{Input: 3, Output: 4, Condition: profile.TierCondition{Type: profile.TierConditionContext, Size: 200000}}},
	}}
	manifest := &warp.ProviderModel{ID: "m", Cost: &warp.ProviderModelCost{Input: 10, Output: 20}}
	m := Merge(ID("p/m"), "ollama", manifest, base)
	if m.Pricing == nil || m.Pricing.Input != 10 || m.Pricing.Output != 20 || len(m.Pricing.Tiers) != 0 {
		t.Fatalf("manifest pricing should win whole: %+v", m.Pricing)
	}

	// No manifest pricing: base pricing (with tiers) is used whole.
	m = Merge(ID("p/m"), "ollama", nil, base)
	if m.Pricing == nil || m.Pricing.Input != 1 || m.Pricing.Output != 2 || len(m.Pricing.Tiers) != 1 {
		t.Fatalf("base pricing should be used whole: %+v", m.Pricing)
	}
	if m.Pricing.Tiers[0].Condition.Size != 200000 || m.Pricing.Tiers[0].Condition.Type != "context" {
		t.Fatalf("tier condition: %+v", m.Pricing.Tiers[0].Condition)
	}
}

func TestMergeSource(t *testing.T) {
	base := &profile.Profile{Name: "Base"}
	manifest := &warp.ProviderModel{ID: "m", Name: "Manifest"}
	m := Merge(ID("p/m"), "ollama", manifest, base)
	if !m.Source.Manifest || !m.Source.BuiltIn {
		t.Fatalf("source should record both layers: %+v", m.Source)
	}

	m = Merge(ID("p/m"), "ollama", manifest, nil)
	if !m.Source.Manifest || m.Source.BuiltIn {
		t.Fatalf("manifest-only source: %+v", m.Source)
	}

	m = Merge(ID("p/m"), "ollama", nil, base)
	if m.Source.Manifest || !m.Source.BuiltIn {
		t.Fatalf("base-only source: %+v", m.Source)
	}
}

func TestMergeNamePrecedence(t *testing.T) {
	// Manifest Name wins, then Label, then base Name, then id name.
	manifest := &warp.ProviderModel{ID: "m", Label: "labeled"}
	base := &profile.Profile{Name: "BaseName"}
	if m := Merge(ID("p/m"), "ollama", manifest, base); m.Name != "labeled" {
		t.Fatalf("label should fill name: %q", m.Name)
	}
	if m := Merge(ID("p/m"), "ollama", &warp.ProviderModel{ID: "m", Name: "named"}, base); m.Name != "named" {
		t.Fatalf("name should win: %q", m.Name)
	}
	if m := Merge(ID("p/m"), "ollama", nil, base); m.Name != "BaseName" {
		t.Fatalf("base name should fill: %q", m.Name)
	}
	if m := Merge(ID("p/m"), "ollama", nil, nil); m.Name != "m" {
		t.Fatalf("id name should fill: %q", m.Name)
	}
}

func TestMergeDimensionsFromCapabilities(t *testing.T) {
	manifest := &warp.ProviderModel{ID: "m", Capabilities: &warp.ProviderModelCapabilities{Dimensions: []int{384, 768}}}
	m := Merge(ID("p/m"), "ollama", manifest, nil)
	if m.Limits.Dimensions != 384 {
		t.Fatalf("limits dimensions should come from capabilities: %+v", m.Limits)
	}
}

func TestMergeIsolation(t *testing.T) {
	base := &profile.Profile{
		Name:         "Base",
		Limits:       profile.Limits{Context: 100, Output: 10},
		Modalities:   profile.Modalities{Inputs: []profile.Modality{"text"}},
		Capabilities: profile.Capabilities{ReasoningOptions: []profile.ReasoningOption{{Type: "effort", Values: []string{"low"}}}},
	}
	manifest := &warp.ProviderModel{ID: "m", Capabilities: &warp.ProviderModelCapabilities{
		Thinking:   []warp.ProviderModelThinkingCapability{{Type: "effort", AllowedEfforts: []string{"low"}}},
		Modalities: &warp.ProviderModelModalities{Input: []string{"text"}},
	}}
	m := Merge(ID("p/m"), "ollama", manifest, base)
	// Mutate the merged model's slices; base must not change.
	m.Modalities.Input[0] = "audio"
	m.Capabilities.Thinking[0].Values[0] = "changed"
	if base.Modalities.Inputs[0] != "text" || base.Capabilities.ReasoningOptions[0].Values[0] != "low" {
		t.Fatalf("merge mutated base metadata: %+v", base)
	}
}
