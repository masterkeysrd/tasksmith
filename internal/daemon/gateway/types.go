package gateway

import (
	"context"
	"errors"

	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/profile"
)

// Common errors returned by the gateway package.
var (
	ErrProviderNotFound      = errors.New("gateway: provider not found")
	ErrModelNotFound         = errors.New("gateway: model not found")
	ErrUnsupportedCapability = errors.New("gateway: model does not support requested capability")
	ErrMissingCredential     = errors.New("gateway: missing required authentication credential")
	ErrInvalidConfig         = errors.New("gateway: invalid configuration")
	ErrGatewayClosed         = errors.New("gateway: gateway is closed")
)

// AuthType describes the HTTP authentication scheme.
type AuthType string

const (
	AuthTypeBearer AuthType = "bearer"
	AuthTypeAPIKey AuthType = "api-key"
	AuthTypeBasic  AuthType = "basic"
)

// AuthConfig defines authentication parameters for a provider.
type AuthConfig struct {
	Type   AuthType `json:"type,omitempty"`
	Header string   `json:"header,omitempty"` // Custom HTTP header (defaults to Authorization for bearer)
	Env    string   `json:"env,omitempty"`    // Key name in the injected environment map
	File   string   `json:"file,omitempty"`   // Explicit path to credential file
}

// ModelCapabilities specifies the workload capabilities supported by a model.
type ModelCapabilities struct {
	Completion *bool                     `json:"completion,omitempty"`
	Decision   *bool                     `json:"decision,omitempty"`
	Embedding  *bool                     `json:"embedding,omitempty"`
	Attachment *bool                     `json:"attachment,omitempty"`
	Tools      *bool                     `json:"tools,omitempty"`
	Dimensions []int                     `json:"dimensions,omitempty"`
	Thinking   []profile.ReasoningOption `json:"thinking,omitempty"`
}

// SupportsCompletion reports whether the model supports text/chat completion.
// Defaults to true if unspecified.
func (c *ModelCapabilities) SupportsCompletion() bool {
	if c == nil || c.Completion == nil {
		return true
	}
	return *c.Completion
}

// SupportsDecision reports whether the model supports native decision evaluation.
// Defaults to false if unspecified.
func (c *ModelCapabilities) SupportsDecision() bool {
	if c == nil || c.Decision == nil {
		return false
	}
	return *c.Decision
}

// SupportsEmbedding reports whether the model supports vector embedding generation.
// Defaults to false if unspecified.
func (c *ModelCapabilities) SupportsEmbedding() bool {
	if c == nil || c.Embedding == nil {
		return false
	}
	return *c.Embedding
}

// ModelLimits specifies token limits and output dimensions.
type ModelLimits struct {
	Context    int `json:"context,omitempty"`
	Output     int `json:"output,omitempty"`
	Dimensions int `json:"dimensions,omitempty"`
}

// ModelSpec describes an available model from a provider.
type ModelSpec struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Label        string             `json:"label,omitempty"`
	Limits       ModelLimits        `json:"limits"`
	Capabilities *ModelCapabilities `json:"capabilities,omitempty"`
}

// ProviderConfig configures a single upstream backend provider.
type ProviderConfig struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"` // e.g. "ollama", "openai", "anthropic", "genai", "typesafe", "antigravity"
	Endpoint string            `json:"endpoint,omitempty"`
	Auth     *AuthConfig       `json:"auth,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Config   map[string]any    `json:"config,omitempty"`
	Models   []ModelSpec       `json:"models,omitempty"`
}

// ProviderInfo provides read-only summary information about an initialized provider.
type ProviderInfo struct {
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Endpoint string      `json:"endpoint,omitempty"`
	Models   []ModelSpec `json:"models,omitempty"`
	Warning  string      `json:"warning,omitempty"`
}

// LLMOption configures a call to ResolveLLM.
type LLMOption func(*llmResolveOptions)

type llmResolveOptions struct {
	config     *llm.ModelConfig
	middleware []llm.Middleware
}

// WithLLMConfig specifies configuration options for the resolved llm.Model.
func WithLLMConfig(cfg *llm.ModelConfig) LLMOption {
	return func(o *llmResolveOptions) {
		o.config = cfg
	}
}

// WithLLMMiddleware appends middleware to the resolved llm.Model.
func WithLLMMiddleware(mw ...llm.Middleware) LLMOption {
	return func(o *llmResolveOptions) {
		o.middleware = append(o.middleware, mw...)
	}
}

// DecisionOption configures a call to ResolveDecision.
type DecisionOption func(*decisionResolveOptions)

type decisionResolveOptions struct {
	config     *decision.ModelConfig
	middleware []decision.Middleware
}

// WithDecisionConfig specifies configuration options for the resolved decision.Model.
func WithDecisionConfig(cfg *decision.ModelConfig) DecisionOption {
	return func(o *decisionResolveOptions) {
		o.config = cfg
	}
}

// WithDecisionMiddleware appends middleware to the resolved decision.Model.
func WithDecisionMiddleware(mw ...decision.Middleware) DecisionOption {
	return func(o *decisionResolveOptions) {
		o.middleware = append(o.middleware, mw...)
	}
}

// EmbeddingOption configures a call to ResolveEmbedding.
type EmbeddingOption func(*embeddingResolveOptions)

type embeddingResolveOptions struct {
	config     *embedding.ModelConfig
	middleware []embedding.Middleware
}

// WithEmbeddingConfig specifies configuration options for the resolved embedding.Model.
func WithEmbeddingConfig(cfg *embedding.ModelConfig) EmbeddingOption {
	return func(o *embeddingResolveOptions) {
		o.config = cfg
	}
}

// WithEmbeddingMiddleware appends middleware to the resolved embedding.Model.
func WithEmbeddingMiddleware(mw ...embedding.Middleware) EmbeddingOption {
	return func(o *embeddingResolveOptions) {
		o.middleware = append(o.middleware, mw...)
	}
}

// Gateway provides access to initialized AI models across providers.
type Gateway interface {
	// ResolveLLM instantiates a conversational/completion model (System Two) for the given provider and model ID.
	ResolveLLM(ctx context.Context, provider, modelID string, opts ...LLMOption) (*llm.Model, error)

	// ResolveDecision instantiates a probabilistic decision model (System One) for the given provider and model ID.
	// If the provider/model natively supports decisions, a native model is returned.
	// Otherwise, it automatically wraps a completion model using decision.NewFromLLM.
	ResolveDecision(ctx context.Context, provider, modelID string, opts ...DecisionOption) (*decision.Model, error)

	// ResolveEmbedding instantiates a vector embedding model (System Three) for the given provider and model ID.
	ResolveEmbedding(ctx context.Context, provider, modelID string, opts ...EmbeddingOption) (*embedding.Model, error)

	// ListProviders returns information about all registered providers and their models.
	ListProviders() []ProviderInfo

	// ModelProfile returns the discovered model profile from the matching provider or catalog, if available.
	ModelProfile(provider, modelID string) (*profile.Profile, bool)

	// Close releases all client resources and connections.
	Close() error
}
