package auth

import (
	"context"
	"fmt"
	"strings"
	"sync"

	loomauth "github.com/masterkeysrd/loom/auth"
	loomantigravity "github.com/masterkeysrd/loom/providers/antigravity"
	"golang.org/x/oauth2"
)

// OAuthDriver provides provider-specific OAuth2 configuration and account extraction.
type OAuthDriver interface {
	OAuthConfig() *oauth2.Config
	ExtractAccount(tok *oauth2.Token) string
}

// BrowserOAuthDriver handles a provider-owned browser callback and credential store.
type BrowserOAuthDriver interface {
	LoginLabel() string
	Authorize(context.Context) (commit func() (account string, err error), err error)
	Account() (string, bool)
	Logout() (bool, error)
	CredentialRevision() string
}

var (
	oauthDriversMu      sync.RWMutex
	oauthDrivers        = make(map[string]OAuthDriver)
	browserOAuthDrivers = make(map[string]BrowserOAuthDriver)
)

// RegisterBrowserOAuthDriver registers a driver for a provider name.
func RegisterBrowserOAuthDriver(provider string, driver BrowserOAuthDriver) {
	oauthDriversMu.Lock()
	defer oauthDriversMu.Unlock()
	browserOAuthDrivers[provider] = driver
}

// GetBrowserOAuthDriver looks up a driver that owns its browser callback.
func GetBrowserOAuthDriver(provider string) (BrowserOAuthDriver, bool) {
	oauthDriversMu.RLock()
	defer oauthDriversMu.RUnlock()
	driver, ok := browserOAuthDrivers[provider]
	return driver, ok
}

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
	if _, ok := GetBrowserOAuthDriver(provider); ok {
		return true
	}
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

// ProviderTokenSource uses an explicit token or builds a source that refreshes
// stored OAuth credentials. The boolean indicates whether credentials exist.
func ProviderTokenSource(ctx context.Context, provider, providerType, token string) (oauth2.TokenSource, bool, error) {
	if strings.TrimSpace(token) != "" {
		return loomauth.StaticTokenSource(token), true, nil
	}
	driver, ok := GetOAuthDriver(providerType)
	if !ok {
		return nil, false, fmt.Errorf("gateway: no oauth driver registered for %q", providerType)
	}
	if driver == nil {
		return nil, false, fmt.Errorf("gateway: nil oauth driver for provider %q", provider)
	}
	source := loomauth.NewPersistentTokenSource(ctx, driver.OAuthConfig(), loomauth.PersistentConfig{
		Load: func() (*oauth2.Token, error) {
			tok, _, err := LoadProviderToken(provider)
			return tok, err
		},
		Save: func(tok *oauth2.Token) error {
			account := driver.ExtractAccount(tok)
			return SaveProviderToken(provider, tok, account)
		},
	})
	return source, HasProviderToken(provider), nil
}
