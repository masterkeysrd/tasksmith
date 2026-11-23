package models

import (
	"slices"
	"testing"

	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/warp"
)

func TestSubscriptionPresetFallback(t *testing.T) {
	for _, tc := range []struct {
		provider string
		models   []string
		inputs   []profile.Modality
	}{
		{"claudecode", []string{"claude-haiku-5-5", "claude-sonnet-5-5", "claude-opus-5-5", "claude-fable-5-2"}, []profile.Modality{profile.ModalityText}},
		{"github-copilot", []string{"gpt-6-luna", "gpt-6.2-sol", "gpt-6.1-terra", "gpt-6.2-astra"}, []profile.Modality{profile.ModalityText, profile.ModalityImage, profile.ModalityPDF}},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			for _, model := range tc.models {
				p := Merge(Normalize(tc.provider, model), tc.provider, &warp.ProviderModel{ID: model}, nil).Profile()
				if !p.Capabilities.ToolCall || p.Capabilities.Attachment != (len(tc.inputs) > 1) || !slices.Equal(p.Modalities.Inputs, tc.inputs) || !p.Pricing.IsZero() {
					t.Errorf("unexpected fallback for %s: %+v", model, p)
				}
			}
			if p := Merge(Normalize(tc.provider, "unknown-model"), tc.provider, nil, nil).Profile(); p.Capabilities.ToolCall || p.Capabilities.Attachment {
				t.Fatalf("unexpected fallback for unknown model: %+v", p)
			}
		})
	}
}

func TestSubscriptionPresetFallbackPrecedence(t *testing.T) {
	id := ID("github-copilot/gpt-6.2-sol")
	manifest := &warp.ProviderModel{ID: id.Name()}
	live := &profile.Profile{
		ID:           id.Name(),
		Name:         "Account Sol",
		Capabilities: profile.Capabilities{Completion: true},
		Limits:       profile.Limits{Context: 54321},
		Modalities:   profile.Modalities{Inputs: []profile.Modality{profile.ModalityText}},
	}
	p := Merge(id, id.Provider(), manifest, live).Profile()
	if p.Capabilities.ToolCall || p.Capabilities.Attachment || p.Limits.Context != 54321 || !slices.Equal(p.Modalities.Inputs, live.Modalities.Inputs) {
		t.Fatalf("fallback overrode live profile: %+v", p)
	}
	manifest.Capabilities = &warp.ProviderModelCapabilities{Tools: new(false), Attachment: new(false)}
	p = Merge(id, id.Provider(), manifest, nil).Profile()
	if p.Capabilities.ToolCall || p.Capabilities.Attachment {
		t.Fatalf("fallback overrode explicit manifest flags: %+v", p)
	}
	live.Capabilities.ToolCall, live.Capabilities.Attachment = true, true
	p = Merge(id, id.Provider(), manifest, live).Profile()
	if p.Capabilities.ToolCall || p.Capabilities.Attachment {
		t.Fatalf("live profile overrode explicit manifest flags: %+v", p)
	}
}
