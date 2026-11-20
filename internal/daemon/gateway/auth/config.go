package auth

import "strings"

// Type describes the HTTP authentication scheme.
type Type string

const (
	// TypeBearer sends "Authorization: Bearer <token>".
	// It is the default scheme when auth.type is omitted.
	TypeBearer Type = "bearer"
	// TypeAPIKey sends the raw token, un-prefixed.
	TypeAPIKey Type = "api-key"
	// TypeBasic sends "Basic base64(<token>)", where the token is typically "user:pass".
	TypeBasic Type = "basic"
	// TypeNone disables credential lookup and authentication headers entirely.
	TypeNone Type = "none"
	// TypeOAuth2 indicates an interactive OAuth2 flow (e.g. Antigravity PKCE).
	TypeOAuth2 Type = "oauth2"
)

// normalized returns the canonical, lower-cased scheme. An empty scheme maps to
// the default "bearer" scheme; any other unknown value is returned as-is so
// callers reject it rather than guessing an intent.
func (t Type) normalized() Type {
	scheme := Type(strings.ToLower(strings.TrimSpace(string(t))))
	if scheme == "" {
		return TypeBearer
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

// Config defines authentication parameters for a provider.
type Config struct {
	Type     Type              `json:"type,omitempty"`
	Header   string            `json:"header,omitempty"` // Custom HTTP header (defaults to Authorization)
	Token    *CredentialSource `json:"token,omitempty"`
	Username string            `json:"username,omitempty"`
	Password *CredentialSource `json:"password,omitempty"`
}
