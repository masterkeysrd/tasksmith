package gateway

import (
	"context"
	"fmt"
	"sync"

	"github.com/masterkeysrd/loom/auth"
	loomantigravity "github.com/masterkeysrd/loom/providers/antigravity"
	"golang.org/x/oauth2"
)

// OAuthDriver provides provider-specific OAuth2 configuration and account extraction.
type OAuthDriver interface {
	OAuthConfig() *oauth2.Config
	ExtractAccount(tok *oauth2.Token) string
}

var (
	oauthDriversMu sync.RWMutex
	oauthDrivers   = make(map[string]OAuthDriver)
)

// RegisterOAuthDriver registers an OAuth driver for a provider name.
func RegisterOAuthDriver(provider string, driver OAuthDriver) {
	oauthDriversMu.Lock()
	defer oauthDriversMu.Unlock()
	oauthDrivers[provider] = driver
}

// GetOAuthDriver looks up the OAuth driver for a provider name.
func GetOAuthDriver(provider string) (OAuthDriver, bool) {
	oauthDriversMu.RLock()
	defer oauthDriversMu.RUnlock()
	d, ok := oauthDrivers[provider]
	return d, ok
}

// HasOAuthDriver reports whether an OAuth driver is registered for a provider.
func HasOAuthDriver(provider string) bool {
	_, ok := GetOAuthDriver(provider)
	return ok
}

// antigravityOAuthDriver implements OAuthDriver for Antigravity (Google Cloud Code).
type antigravityOAuthDriver struct{}

func (d antigravityOAuthDriver) OAuthConfig() *oauth2.Config {
	return loomantigravity.NewOAuthConfig(loomantigravity.OAuthOptions{})
}

func (d antigravityOAuthDriver) ExtractAccount(tok *oauth2.Token) string {
	return ExtractEmailFromToken(tok)
}

func init() {
	RegisterOAuthDriver("antigravity", antigravityOAuthDriver{})
}

// NewProviderTokenSource creates an automatic background-refreshing TokenSource for an OAuth provider.
func NewProviderTokenSource(ctx context.Context, provider string, driver OAuthDriver) (oauth2.TokenSource, error) {
	if driver == nil {
		return nil, fmt.Errorf("gateway: nil oauth driver for provider %q", provider)
	}
	oauthCfg := driver.OAuthConfig()
	return auth.NewPersistentTokenSource(ctx, oauthCfg, auth.PersistentConfig{
		Load: func() (*oauth2.Token, error) {
			tok, _, err := LoadProviderToken(provider)
			return tok, err
		},
		Save: func(tok *oauth2.Token) error {
			account := driver.ExtractAccount(tok)
			return SaveProviderToken(provider, tok, account)
		},
	}), nil
}
