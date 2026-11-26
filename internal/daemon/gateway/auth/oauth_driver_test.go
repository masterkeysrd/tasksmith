package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/testutils"
	"golang.org/x/oauth2"
)

type browserOAuthTestCloser struct {
	BrowserOAuthDriver
	closeFunc func() error
}

func (d *browserOAuthTestCloser) Close() error { return d.closeFunc() }

func TestCloseBrowserOAuthDrivers(t *testing.T) {
	oauthDriversMu.Lock()
	original := browserOAuthDrivers
	browserOAuthDrivers = make(map[string]BrowserOAuthDriver)
	oauthDriversMu.Unlock()
	t.Cleanup(func() {
		oauthDriversMu.Lock()
		browserOAuthDrivers = original
		oauthDriversMu.Unlock()
	})

	firstErr := errors.New("first driver close failed")
	secondErr := errors.New("second driver close failed")
	calls := make(map[error]int)
	newCloser := func(err error) *browserOAuthTestCloser {
		return &browserOAuthTestCloser{closeFunc: func() error {
			calls[err]++
			if oauthDriversMu.TryLock() {
				oauthDriversMu.Unlock()
			} else {
				t.Error("driver Close called while the registry was locked")
			}
			return err
		}}
	}
	drivers := map[string]BrowserOAuthDriver{
		"first":  newCloser(firstErr),
		"second": newCloser(secondErr),
		"plain":  struct{ BrowserOAuthDriver }{},
	}
	for name, driver := range drivers {
		RegisterBrowserOAuthDriver(name, driver)
	}

	err := CloseBrowserOAuthDrivers()
	for _, want := range []error{firstErr, secondErr} {
		if calls[want] != 1 || !errors.Is(err, want) {
			t.Errorf("driver error %v: got %d closes and joined error %v", want, calls[want], err)
		}
	}
	for name, want := range drivers {
		if got, ok := GetBrowserOAuthDriver(name); !ok || got != want {
			t.Errorf("registration %q changed after shutdown", name)
		}
	}
}

type refreshOAuthTestDriver struct {
	tokenURL string
}

func (d refreshOAuthTestDriver) OAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID: "test-client",
		Endpoint: oauth2.Endpoint{
			TokenURL:  d.tokenURL,
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
}

func (d refreshOAuthTestDriver) AuthCodeOptions() []oauth2.AuthCodeOption { return nil }
func (d refreshOAuthTestDriver) ExtractAccount(*oauth2.Token) string      { return "" }

func TestRefreshableOAuthTokenSourcePreservesAccountWhenIDTokenOmitted(t *testing.T) {
	testutils.SetupXDG(t, "oauth-refresh-account")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fresh-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	const provider = "refresh-acct-test"
	expired := &oauth2.Token{
		AccessToken:  "stale-access-token",
		RefreshToken: "keep-refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour),
	}
	if err := SaveProviderToken(provider, expired, "alice@example.com"); err != nil {
		t.Fatalf("SaveProviderToken: %v", err)
	}

	ts := newRefreshableOAuthTokenSource(context.Background(), provider, refreshOAuthTestDriver{tokenURL: srv.URL})
	gotTok, err := ts.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if gotTok.AccessToken != "fresh-access-token" {
		t.Fatalf("AccessToken = %q, want fresh-access-token", gotTok.AccessToken)
	}

	reloaded, account, err := LoadProviderToken(provider)
	if err != nil {
		t.Fatalf("LoadProviderToken: %v", err)
	}
	if account != "alice@example.com" {
		t.Fatalf("stored account = %q, want alice@example.com", account)
	}
	if reloaded.AccessToken != "fresh-access-token" || reloaded.RefreshToken != "keep-refresh-token" {
		t.Fatalf("unexpected reloaded token: %+v", reloaded)
	}
}
