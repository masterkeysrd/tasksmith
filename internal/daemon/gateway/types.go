package gateway

import (
	"context"
	"errors"

	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/providers"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
)

// Common errors returned by the gateway package.
var (
	ErrProviderNotFound      = errors.New("gateway: provider not found")
	ErrModelNotFound         = errors.New("gateway: model not found")
	ErrUnsupportedCapability = errors.New("gateway: model does not support requested capability")
	ErrMissingCredential     = auth.ErrMissingCredential
	ErrInvalidConfig         = providers.ErrInvalidConfig
	ErrGatewayClosed         = errors.New("gateway: gateway is closed")
)

// ProviderConfig configures a single upstream backend provider.
type ProviderConfig struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"` // e.g. "ollama", "openai", "anthropic", "genai", "typesafe", "antigravity"
	Endpoint string            `json:"endpoint,omitempty"`
	Auth     *auth.Config      `json:"auth,omitempty"`
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

	// ListModels returns the configured provider's live models or known profiles.
	// The boolean distinguishes an empty live catalog from an unavailable catalog.
	ListModels(ctx context.Context, provider string) ([]profile.Profile, bool, error)

	GetProfile(id string) (profile.Profile, bool)

	// Quota returns the remaining quota buckets for a quota-billed model, or
	// (nil, nil) when the model's provider does not report quotas or has no
	// quota data.
	Quota(ctx context.Context, model models.Model) (*api.ModelQuota, error)

	// QuotaSnapshots returns the full multi-bucket quota snapshots across all
	// quota-capable workspace providers.
	QuotaSnapshots(ctx context.Context) (*api.QuotaSnapshotResponse, error)

	// QuotaCapable reports whether the model's provider can report quota at
	// all. It is a cheap capability check (no network), used to label models
	// as subscription-billed without querying the provider.
	QuotaCapable(model models.Model) bool

	// Close releases all client resources and connections.
	Close() error
}
