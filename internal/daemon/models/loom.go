package models

import "github.com/masterkeysrd/loom/profile"

// Profile derives the loom profile view of the model. It is the only conversion
// from TaskSmith to loom and is computed on demand; the result is never stored.
func (m Model) Profile() profile.Profile {
	p := profile.Profile{
		ID:   m.ID.Name(),
		Name: m.Name,
		Limits: profile.Limits{
			Context:    m.Limits.Context,
			Output:     m.Limits.Output,
			Dimensions: m.Limits.Dimensions,
		},
		Modalities: profile.Modalities{
			Inputs:  stringsToModalities(m.Modalities.Input),
			Outputs: stringsToModalities(m.Modalities.Output),
		},
		Capabilities: profile.Capabilities{
			Completion:       capBool(m.Capabilities.Completion, true),
			Decision:         capBool(m.Capabilities.Decision, false),
			Embedding:        capBool(m.Capabilities.Embedding, false),
			Attachment:       capBool(m.Capabilities.Attachment, false),
			ToolCall:         capBool(m.Capabilities.Tools, false),
			Temperature:      capBool(m.Capabilities.Temperature, false),
			Reasoning:        len(m.Capabilities.Thinking) > 0,
			ReasoningOptions: thinkingToReasoning(m.Capabilities.Thinking),
		},
	}
	if m.Pricing != nil {
		p.Pricing = m.toLoomPricing()
	}
	return p
}

func (m Model) toLoomPricing() profile.Pricing {
	out := profile.Pricing{
		Input:      m.Pricing.Input,
		Output:     m.Pricing.Output,
		CacheRead:  m.Pricing.CacheRead,
		CacheWrite: m.Pricing.CacheWrite,
		Reasoning:  m.Pricing.Reasoning,
	}
	if len(m.Pricing.Tiers) > 0 {
		out.TieredLimits = make([]profile.TierPricing, 0, len(m.Pricing.Tiers))
		for _, t := range m.Pricing.Tiers {
			out.TieredLimits = append(out.TieredLimits, profile.TierPricing{
				Input:      t.Input,
				Output:     t.Output,
				CacheRead:  t.CacheRead,
				CacheWrite: t.CacheWrite,
				Reasoning:  t.Reasoning,
				Label:      t.Label,
				Condition:  profile.TierCondition{Type: profile.TierConditionType(t.Condition.Type), Size: t.Condition.Size},
			})
		}
	}
	return out
}

// fromProfile is the only conversion from loom to TaskSmith. It builds the
// built-in layer of a Model from a loom profile. The model part of id and the
// provider type are supplied by the caller, since a profile does not carry them.
func fromProfile(base *profile.Profile, id ID, providerType string) Model {
	var m Model
	m.ID = id
	m.ProviderType = providerType
	if base == nil {
		return m
	}
	m.Source.BuiltIn = true
	m.Name = base.Name
	m.Limits = Limits{
		Context:    base.Limits.Context,
		Output:     base.Limits.Output,
		Dimensions: base.Limits.Dimensions,
	}
	c := base.Capabilities
	m.Capabilities = Capabilities{
		Completion:  boolPtr(c.Completion),
		Decision:    boolPtr(c.Decision),
		Embedding:   boolPtr(c.Embedding),
		Tools:       boolPtr(c.ToolCall),
		Attachment:  boolPtr(c.Attachment),
		Temperature: boolPtr(c.Temperature),
		Thinking:    reasoningToThinking(c.ReasoningOptions),
	}
	if base.Modalities.Inputs != nil {
		m.Modalities.Input = modalitySlice(base.Modalities.Inputs)
	}
	if base.Modalities.Outputs != nil {
		m.Modalities.Output = modalitySlice(base.Modalities.Outputs)
	}
	if hasPricing(base.Pricing) {
		m.Pricing = &Pricing{
			Input:      base.Pricing.Input,
			Output:     base.Pricing.Output,
			CacheRead:  base.Pricing.CacheRead,
			CacheWrite: base.Pricing.CacheWrite,
			Reasoning:  base.Pricing.Reasoning,
		}
		for _, t := range base.Pricing.TieredLimits {
			m.Pricing.Tiers = append(m.Pricing.Tiers, PricingTier{
				Label:      t.Label,
				Condition:  TierCondition{Type: string(t.Condition.Type), Size: t.Condition.Size},
				Input:      t.Input,
				Output:     t.Output,
				CacheRead:  t.CacheRead,
				CacheWrite: t.CacheWrite,
				Reasoning:  t.Reasoning,
			})
		}
	}
	return m
}

func capBool(b *bool, def bool) bool {
	if b == nil {
		return def
	}
	return *b
}

func stringsToModalities(in []string) []profile.Modality {
	if in == nil {
		return nil
	}
	out := make([]profile.Modality, len(in))
	for i, m := range in {
		out[i] = profile.Modality(m)
	}
	return out
}

func modalitySlice(in []profile.Modality) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	for i, m := range in {
		out[i] = string(m)
	}
	return out
}

// thinkingToReasoning converts the model's thinking options to loom reasoning
// options.
func thinkingToReasoning(in []ThinkingOption) []profile.ReasoningOption {
	if len(in) == 0 {
		return nil
	}
	out := make([]profile.ReasoningOption, len(in))
	for i, o := range in {
		out[i] = profile.ReasoningOption{
			Type:    o.Type,
			Values:  append([]string(nil), o.Values...),
			Default: o.Default,
			Min:     o.Min,
			Max:     o.Max,
		}
	}
	return out
}

// reasoningToThinking converts loom reasoning options to the model's thinking
// options.
func reasoningToThinking(in []profile.ReasoningOption) []ThinkingOption {
	if len(in) == 0 {
		return nil
	}
	out := make([]ThinkingOption, len(in))
	for i, o := range in {
		out[i] = ThinkingOption{
			Type:    o.Type,
			Values:  append([]string(nil), o.Values...),
			Default: o.Default,
			Min:     o.Min,
			Max:     o.Max,
		}
	}
	return out
}

func hasPricing(p profile.Pricing) bool {
	return p.Input != 0 || p.Output != 0 || p.CacheRead != 0 || p.CacheWrite != 0 ||
		p.Reasoning != 0 || len(p.TieredLimits) > 0
}
