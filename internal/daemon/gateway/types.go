package gateway

import (
	"context"
	"errors"

	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
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

// ProviderConfig configures a single upstream backend provider.
type ProviderConfig struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"` // e.g. "ollama", "openai", "anthropic", "genai", "typesafe", "antigravity"
	Endpoint string            `json:"endpoint,omitempty"`
	Auth     *AuthConfig       `json:"auth,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Config   map[string]any    `json:"config,omitempty"`
}

// ProviderInfo provides read-only summary information about an initialized provider.
type ProviderInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Endpoint string `json:"endpoint,omitempty"`
	Warning  string `json:"warning,omitempty"`
}

// Gateway provides access to initialized AI models across providers.
type Gateway interface {
	LLM(ctx context.Context, model models.Model, cfg llm.ModelConfig) (*llm.Model, error)
	Decision(ctx context.Context, model models.Model, cfg llm.ModelConfig) (*decision.Model, error)
	Embedding(ctx context.Context, model models.Model) (*embedding.Model, error)
	ListProviders() []ProviderInfo

	GetProfile(id string) (profile.Profile, bool)

	// Close releases all client resources and connections.
	Close() error
}
