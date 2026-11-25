package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

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

// ExpiryChecker is optionally implemented by [BrowserOAuthDriver] values that
// can report whether stored credentials on disk are statically expired with no
// refresh path.
type ExpiryChecker interface {
	IsExpired() bool
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

func CloseBrowserOAuthDrivers() error {
	oauthDriversMu.RLock()
	drivers := maps.Clone(browserOAuthDrivers)
	oauthDriversMu.RUnlock()

	var errs []error
	for provider, driver := range drivers {
		if closer, ok := driver.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close browser OAuth driver %q: %w", provider, err))
			}
		}
	}
	return errors.Join(errs...)
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
	source := newRefreshableOAuthTokenSource(ctx, provider, driver)
	return source, HasProviderToken(provider), nil
}

type refreshableOAuthTokenSource struct {
	ctx      context.Context
	provider string
	driver   OAuthDriver

	mu    sync.Mutex
	inner oauth2.TokenSource
}

var _ RefreshableTokenSource = (*refreshableOAuthTokenSource)(nil)

func newRefreshableOAuthTokenSource(ctx context.Context, provider string, driver OAuthDriver) *refreshableOAuthTokenSource {
	s := &refreshableOAuthTokenSource{
		ctx:      ctx,
		provider: provider,
		driver:   driver,
	}
	s.inner = s.buildInner()
	return s
}

func (s *refreshableOAuthTokenSource) buildInner() oauth2.TokenSource {
	return loomauth.NewPersistentTokenSource(s.ctx, s.driver.OAuthConfig(), loomauth.PersistentConfig{
		Load: func() (*oauth2.Token, error) {
			tok, _, err := LoadProviderToken(s.provider)
			return tok, err
		},
		Save: func(tok *oauth2.Token) error {
			account := s.driver.ExtractAccount(tok)
			return SaveProviderToken(s.provider, tok, account)
		},
	})
}

func (s *refreshableOAuthTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Token()
}

func (s *refreshableOAuthTokenSource) ForceRefresh(ctx context.Context, failedAccessToken string) (*oauth2.Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stored, account, err := LoadProviderToken(s.provider)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, loomauth.ErrAuthRequired
		}
		return nil, err
	}
	if stored != nil && stored.AccessToken != "" && failedAccessToken != "" && stored.AccessToken != failedAccessToken {
		s.inner = s.buildInner()
		return stored, nil
	}
	if stored == nil || strings.TrimSpace(stored.RefreshToken) == "" {
		return nil, loomauth.ErrAuthRequired
	}

	refreshCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	tok, err := s.driver.OAuthConfig().TokenSource(refreshCtx, &oauth2.Token{
		RefreshToken: stored.RefreshToken,
	}).Token()
	if err != nil {
		var rErr *oauth2.RetrieveError
		if errors.As(err, &rErr) && rErr != nil &&
			(strings.EqualFold(rErr.ErrorCode, "invalid_grant") ||
				(rErr.Response != nil && (rErr.Response.StatusCode == http.StatusBadRequest || rErr.Response.StatusCode == http.StatusUnauthorized))) {
			return nil, fmt.Errorf("%w: %w", loomauth.ErrAuthRequired, err)
		}
		return nil, err
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = stored.RefreshToken
	}
	if extracted := s.driver.ExtractAccount(tok); extracted != "" {
		account = extracted
	}
	if err := SaveProviderToken(s.provider, tok, account); err != nil {
		return nil, err
	}
	s.inner = s.buildInner()
	return tok, nil
}
