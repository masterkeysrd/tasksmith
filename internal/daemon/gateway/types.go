package gateway

import (
	"context"
	"errors"
	"strings"

	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
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
	// AuthTypeBearer sends "Authorization: Bearer <token>".
	// It is the default scheme when auth.type is omitted.
	AuthTypeBearer AuthType = "bearer"
	// AuthTypeAPIKey sends the raw token, un-prefixed.
	AuthTypeAPIKey AuthType = "api-key"
	// AuthTypeBasic sends "Basic base64(<token>)", where the token is typically "user:pass".
	AuthTypeBasic AuthType = "basic"
	// AuthTypeNone disables credential lookup and authentication headers entirely.
	AuthTypeNone AuthType = "none"
	// AuthTypeOAuth2 indicates an interactive OAuth2 flow (e.g. Antigravity PKCE).
	AuthTypeOAuth2 AuthType = "oauth2"
)

// normalized returns the canonical, lower-cased scheme. An empty scheme maps to
// the default "bearer" scheme; any other unknown value is returned as-is so
// callers reject it rather than guessing an intent.
func (t AuthType) normalized() AuthType {
	scheme := AuthType(strings.ToLower(strings.TrimSpace(string(t))))
	if scheme == "" {
		return AuthTypeBearer
	}
	return scheme
}

// CredentialSource locates a static secret value in an environment variable
// or in a file on disk.
type CredentialSource struct {
	Env  string `json:"env,omitempty"`  // Key name in the injected environment map
	File string `json:"file,omitempty"` // Explicit path to credential file
}

// IsZero reports whether the source declares neither an env key nor a file path.
func (c *CredentialSource) IsZero() bool {
	return c == nil || (strings.TrimSpace(c.Env) == "" && strings.TrimSpace(c.File) == "")
}

// AuthConfig defines authentication parameters for a provider.
type AuthConfig struct {
	Type     AuthType          `json:"type,omitempty"`
	Header   string            `json:"header,omitempty"` // Custom HTTP header (defaults to Authorization)
	Token    *CredentialSource `json:"token,omitempty"`
	Username string            `json:"username,omitempty"`
	Password *CredentialSource `json:"password,omitempty"`
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

	// Quota returns the remaining quota for a quota-billed model, or (nil, nil)
	// when the model's provider does not report quotas or has no quota data.
	Quota(ctx context.Context, model models.Model) (*api.ModelQuota, error)

	// QuotaCapable reports whether the model's provider can report quota at
	// all. It is a cheap capability check (no network), used to label models
	// as subscription-billed without querying the provider.
	QuotaCapable(model models.Model) bool

	// Close releases all client resources and connections.
	Close() error
}
