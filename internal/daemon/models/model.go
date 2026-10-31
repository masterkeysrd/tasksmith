package models

import (
	"cmp"
	"slices"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/ptr"
)

// Model is the complete, effective description of a model in a workspace.
// It is immutable once built.
type Model struct {
	// ID is the qualified identifier "<provider>/<model>".
	ID ID
	// Name is the display name.
	Name  string
	Label string
	// ProviderType is the provider kind ("anthropic", "openai", "genai", ...).
	// It is needed by Configure to apply provider-specific rules.
	ProviderType string
	// Limits holds token and vector-dimension limits.
	Limits Limits
	// Capabilities holds the workload capabilities. Each *bool is nil when
	// unknown, which is distinct from a known false.
	Capabilities Capabilities
	// Modalities holds the supported input and output data modalities.
	Modalities Modalities
	// Pricing is the per-million-token pricing. nil means unpriced.
	Pricing *api.ModelPricing
	// Source records where the model's data came from.
	Source Source
}

// Clone returns a deep copy of m.
func (m Model) Clone() Model {
	m.Capabilities.Completion = ptr.Clone(m.Capabilities.Completion)
	m.Capabilities.Decision = ptr.Clone(m.Capabilities.Decision)
	m.Capabilities.Embedding = ptr.Clone(m.Capabilities.Embedding)
	m.Capabilities.Tools = ptr.Clone(m.Capabilities.Tools)
	m.Capabilities.Attachment = ptr.Clone(m.Capabilities.Attachment)
	m.Capabilities.StructuredOutput = ptr.Clone(m.Capabilities.StructuredOutput)
	m.Capabilities.Temperature = ptr.Clone(m.Capabilities.Temperature)
	m.Capabilities.Dimensions = slices.Clone(m.Capabilities.Dimensions)
	if m.Capabilities.Thinking != nil {
		th := make([]ThinkingOption, len(m.Capabilities.Thinking))
		for i, opt := range m.Capabilities.Thinking {
			opt.Values = slices.Clone(opt.Values)
			th[i] = opt
		}
		m.Capabilities.Thinking = th
	}
	m.Modalities.Input = slices.Clone(m.Modalities.Input)
	m.Modalities.Output = slices.Clone(m.Modalities.Output)
	if m.Pricing != nil {
		pricing := *m.Pricing
		pricing.Tiers = slices.Clone(pricing.Tiers)
		m.Pricing = &pricing
	}
	return m
}

// ToAPI projects an internal catalog [Model] onto the public [api.Model] representation.
func (m Model) ToAPI() api.Model {
	out := api.Model{
		ID:           string(m.ID),
		Provider:     m.ID.Provider(),
		Name:         m.Name,
		Label:        cmp.Or(m.Label, m.Name),
		ContextLimit: m.Limits.Context,
		OutputLimit:  m.Limits.Output,
		Capabilities: api.ModelCapabilities{
			Completion:       ptr.Clone(m.Capabilities.Completion),
			Decision:         ptr.Clone(m.Capabilities.Decision),
			Embedding:        ptr.Clone(m.Capabilities.Embedding),
			Tools:            ptr.Clone(m.Capabilities.Tools),
			Attachment:       ptr.Clone(m.Capabilities.Attachment),
			StructuredOutput: ptr.Clone(m.Capabilities.StructuredOutput),
			Temperature:      ptr.Clone(m.Capabilities.Temperature),
			Dimensions:       slices.Clone(m.Capabilities.Dimensions),
			InputModalities:  slices.Clone(m.Modalities.Input),
			OutputModalities: slices.Clone(m.Modalities.Output),
		},
	}
	for _, o := range m.Capabilities.Thinking {
		out.Thinking = append(out.Thinking, api.ThinkingCapability{
			Type:    o.Type,
			Values:  ToAPIThinkingValues(o.Type, o.Values),
			Default: o.Default,
			Min:     o.Min,
			Max:     o.Max,
		})
	}
	if p := m.Pricing; p != nil {
		pricing := *p
		pricing.Tiers = slices.Clone(p.Tiers)
		out.Pricing = &pricing
	}
	return out
}

// ToAPIThinkingValues projects a list of thinking option values onto the public
// [api.ThinkingValue] representation, attaching human-readable descriptions for
// known reasoning-effort levels.
func ToAPIThinkingValues(kind string, values []string) []api.ThinkingValue {
	if len(values) == 0 {
		return nil
	}
	out := make([]api.ThinkingValue, len(values))
	for i, value := range values {
		out[i].Value = value
		if strings.EqualFold(kind, "effort") {
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "none":
				out[i].Description = "Requests no reasoning effort, where supported."
			case "minimal":
				out[i].Description = "Minimal reasoning; potentially faster."
			case "low":
				out[i].Description = "Less reasoning; potentially faster."
			case "medium":
				out[i].Description = "Balances reasoning depth and speed."
			case "high":
				out[i].Description = "More reasoning; potentially slower."
			case "xhigh":
				out[i].Description = "More extensive reasoning; potentially slower."
			case "max":
				out[i].Description = "Maximum reasoning; potentially slower."
			case "auto":
				out[i].Description = "Lets the provider choose the reasoning effort."
			}
		}
	}
	return out
}

// Limits defines token and vector-dimension limits for a model.
type Limits struct {
	Context    int
	Output     int
	Dimensions int
}

// Capabilities uses *bool: nil means unknown, which is distinct from false.
type Capabilities struct {
	Completion       *bool
	Decision         *bool
	Embedding        *bool
	Tools            *bool
	Attachment       *bool
	StructuredOutput *bool
	Temperature      *bool
	Dimensions       []int
	Thinking         []ThinkingOption
}

// SupportsCompletion reports whether the model supports text/chat completion.
// Defaults to true when unknown.
func (c Capabilities) SupportsCompletion() bool {
	if c.Completion == nil {
		return true
	}
	return *c.Completion
}

// SupportsDecision reports whether the model supports native decision evaluation.
// Defaults to false when unknown.
func (c Capabilities) SupportsDecision() bool {
	if c.Decision == nil {
		return false
	}
	return *c.Decision
}

// SupportsEmbedding reports whether the model supports vector embedding
// generation. Defaults to false when unknown.
func (c Capabilities) SupportsEmbedding() bool {
	if c.Embedding == nil {
		return false
	}
	return *c.Embedding
}

// SupportsTools reports whether the model supports tool calls.
// Defaults to false when unknown.
func (c Capabilities) SupportsTools() bool {
	if c.Tools == nil {
		return false
	}
	return *c.Tools
}

// ThinkingOption describes a supported reasoning configuration strategy.
type ThinkingOption struct {
	// Type is "effort" | "budget_tokens" | "toggle" | ...
	Type string
	// Values lists the allowed values for the option.
	Values []string
	// Default is the default value.
	Default string
	// Min is the minimum budget (for budget_tokens).
	Min int
	// Max is the maximum budget (for budget_tokens).
	Max int
}

// Modalities lists the supported input and output data modalities of a model.
type Modalities struct {
	Input  []string
	Output []string
}

// Source records where the model's data came from.
type Source struct {
	// BuiltIn reports that a loom built-in profile contributed data.
	BuiltIn bool
	// Manifest reports that a workspace manifest contributed data.
	Manifest bool
}
