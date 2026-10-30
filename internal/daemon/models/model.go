package models

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
	Pricing *Pricing
	// Source records where the model's data came from.
	Source Source
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

// Pricing is per million tokens, in USD.
type Pricing struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
	Reasoning  float64
	Tiers      []PricingTier
}

// PricingTier is a conditional pricing override.
type PricingTier struct {
	Label      string
	Condition  TierCondition
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
	Reasoning  float64
}

// TierCondition is a pricing tier trigger, e.g. {Type: "context", Size: 200000}.
type TierCondition struct {
	Type string
	Size int
}

// Source records where the model's data came from.
type Source struct {
	// BuiltIn reports that a loom built-in profile contributed data.
	BuiltIn bool
	// Manifest reports that a workspace manifest contributed data.
	Manifest bool
}
