package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	loomauth "github.com/masterkeysrd/loom/auth"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"golang.org/x/oauth2"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRefreshOnUnauthorizedTransport(t *testing.T) {
	t.Run("401_refreshes_once_and_replays_body", func(t *testing.T) {
		var attempts atomic.Int32
		var refreshCalls atomic.Int32
		var gotAuths []string
		var gotBodies []string

		base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			gotAuths = append(gotAuths, req.Header.Get("Authorization"))
			gotBodies = append(gotBodies, string(body))
			if attempts.Add(1) == 1 {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Body:       io.NopCloser(strings.NewReader(`{"error":"expired"}`)),
					Header:     make(http.Header),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Header:     make(http.Header),
			}, nil
		})

		rt := NewRefreshOnUnauthorizedTransport(base, func(_ context.Context, failed string) (*oauth2.Token, error) {
			refreshCalls.Add(1)
			if failed != "stale-token" {
				t.Errorf("failedAccessToken = %q, want stale-token", failed)
			}
			return &oauth2.Token{AccessToken: "fresh-token"}, nil
		})

		// Pass an io.ReadCloser with nil GetBody to exercise buffering.
		req, err := http.NewRequest(http.MethodPost, "https://api.example.test/v1/responses", io.NopCloser(strings.NewReader(`{"prompt":"hello"}`)))
		if err != nil {
			t.Fatal(err)
		}
		req.GetBody = nil
		req.Header.Set("Authorization", "Bearer stale-token")

		resp, err := rt.RoundTrip(req)
		if err != nil {
			t.Fatalf("RoundTrip error: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if refreshCalls.Load() != 1 {
			t.Fatalf("refresh calls = %d, want 1", refreshCalls.Load())
		}
		if attempts.Load() != 2 {
			t.Fatalf("attempts = %d, want 2", attempts.Load())
		}
		if len(gotAuths) != 2 || gotAuths[0] != "Bearer stale-token" || gotAuths[1] != "Bearer fresh-token" {
			t.Fatalf("Authorization headers = %v", gotAuths)
		}
		if len(gotBodies) != 2 || gotBodies[0] != `{"prompt":"hello"}` || gotBodies[1] != `{"prompt":"hello"}` {
			t.Fatalf("replayed bodies = %v", gotBodies)
		}
	})

	t.Run("401_refresh_auth_required_does_not_replay", func(t *testing.T) {
		var attempts atomic.Int32
		base := roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts.Add(1)
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(strings.NewReader(`unauthorized`)),
				Header:     make(http.Header),
			}, nil
		})

		rt := NewRefreshOnUnauthorizedTransport(base, func(context.Context, string) (*oauth2.Token, error) {
			return nil, loomauth.ErrAuthRequired
		})

		req, _ := http.NewRequest(http.MethodPost, "https://api.example.test/v1/responses", strings.NewReader("payload"))
		req.Header.Set("Authorization", "Bearer dead-token")

		resp, err := rt.RoundTrip(req)
		if !errors.Is(err, loomauth.ErrAuthRequired) {
			t.Fatalf("RoundTrip error = %v, want ErrAuthRequired", err)
		}
		if resp != nil {
			t.Fatalf("expected nil response on ErrAuthRequired, got %+v", resp)
		}
		if attempts.Load() != 1 {
			t.Fatalf("attempts = %d, want 1 (must not replay)", attempts.Load())
		}
	})

	t.Run("second_401_does_not_loop", func(t *testing.T) {
		var attempts atomic.Int32
		var refreshCalls atomic.Int32
		base := roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts.Add(1)
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(strings.NewReader(`still unauthorized`)),
				Header:     make(http.Header),
			}, nil
		})

		rt := NewRefreshOnUnauthorizedTransport(base, func(context.Context, string) (*oauth2.Token, error) {
			refreshCalls.Add(1)
			return &oauth2.Token{AccessToken: "still-rejected"}, nil
		})

		req, _ := http.NewRequest(http.MethodGet, "https://api.example.test/v1/models", nil)
		req.Header.Set("Authorization", "Bearer stale-token")

		resp, err := rt.RoundTrip(req)
		if err != nil {
			t.Fatalf("RoundTrip error: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
		if refreshCalls.Load() != 1 || attempts.Load() != 2 {
			t.Fatalf("refreshCalls=%d attempts=%d, want 1 and 2", refreshCalls.Load(), attempts.Load())
		}
	})
}

type testBrowserCreds struct {
	Email        string
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

func TestBrowserSessionStoreForceRefreshConcurrentDedupAndExpiry(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-browser-force-refresh-test")
	t.Cleanup(xdg.ClearCache)

	var exchanges atomic.Int32
	store := NewBrowserSessionStore(
		"dedup-provider",
		func(c testBrowserCreds) string { return c.Email },
		func(_ context.Context, c *testBrowserCreds, save func(testBrowserCreds) error) (oauth2.TokenSource, error) {
			current := *c
			return browserTestTokenSource(func() (*oauth2.Token, error) {
				if time.Until(current.Expiry) <= 30*time.Second {
					if current.RefreshToken == "" {
						return nil, loomauth.ErrAuthRequired
					}
					n := exchanges.Add(1)
					time.Sleep(20 * time.Millisecond)
					current.AccessToken = "rotated-access-" + string(rune('0'+n))
					current.Expiry = time.Now().Add(time.Hour)
					if err := save(current); err != nil {
						return nil, err
					}
				}
				return &oauth2.Token{
					AccessToken:  current.AccessToken,
					RefreshToken: current.RefreshToken,
					Expiry:       current.Expiry,
				}, nil
			}), nil
		},
		WithBrowserSessionExpire(func(c *testBrowserCreds) {
			c.Expiry = time.Unix(1, 0).UTC()
		}),
		WithBrowserSessionExpiredCheck(func(c testBrowserCreds) bool {
			return !c.Expiry.IsZero() && c.Expiry.Before(time.Now()) && c.RefreshToken == ""
		}),
	)

	if err := store.Save(testBrowserCreds{
		Email:        "user@example.test",
		AccessToken:  "stale-access",
		RefreshToken: "refresh-1",
		Expiry:       time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if store.IsExpired() {
		t.Fatal("fresh refreshable credentials reported IsExpired=true")
	}

	first, err := store.TokenSource()
	if err != nil {
		t.Fatal(err)
	}
	tok, err := first.Token()
	if err != nil || tok.AccessToken != "stale-access" {
		t.Fatalf("initial Token() = %+v, %v", tok, err)
	}

	const workers = 4
	results := make([]string, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range workers {
		wg.Go(func() {
			<-start
			tkn, rErr := store.ForceRefresh(context.Background(), "stale-access")
			errs[i] = rErr
			if tkn != nil {
				results[i] = tkn.AccessToken
			}
		})
	}
	close(start)
	wg.Wait()

	if got := exchanges.Load(); got != 1 {
		t.Fatalf("concurrent ForceRefresh triggered %d exchanges, want 1", got)
	}
	for i := range workers {
		if errs[i] != nil {
			t.Fatalf("worker %d failed: %v", i, errs[i])
		}
		if results[i] != "rotated-access-1" {
			t.Fatalf("worker %d token = %q, want rotated-access-1", i, results[i])
		}
	}

	second, err := store.TokenSource()
	if err != nil || second != first {
		t.Fatalf("TokenSource pointer identity changed after ForceRefresh: err=%v same=%v", err, second == first)
	}
	after, err := first.Token()
	if err != nil || after.AccessToken != "rotated-access-1" {
		t.Fatalf("existing source Token() after ForceRefresh = %+v, %v", after, err)
	}

	// Unrefreshable expired credentials report IsExpired=true and fail ForceRefresh
	// without wiping Account() email.
	if err := store.Save(testBrowserCreds{
		Email:       "user@example.test",
		AccessToken: "dead-access",
		Expiry:      time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if !store.IsExpired() {
		t.Fatal("expired credentials without refresh token should report IsExpired=true")
	}
	if _, err := store.ForceRefresh(context.Background(), "dead-access"); !errors.Is(err, loomauth.ErrAuthRequired) {
		t.Fatalf("ForceRefresh without refresh token = %v, want ErrAuthRequired", err)
	}
	if email, ok := store.Account(); !ok || email != "user@example.test" {
		t.Fatalf("Account() after failed ForceRefresh = %q, %v", email, ok)
	}
}

func TestManagerAuthHealthLifecycle(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-auth-health-test")
	t.Cleanup(xdg.ClearCache)

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-1","refresh_token":"ref-1","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenSrv.Close()

	var browserDriver *managerFakeBrowserDriver
	browserDriver = &managerFakeBrowserDriver{
		account:  "dev@example.test",
		signedIn: true,
		revision: "rev-1",
		authorize: func(context.Context) (func() (string, error), error) {
			return func() (string, error) {
				browserDriver.signedIn = true
				browserDriver.revision = "rev-2"
				return "dev@example.test", nil
			}, nil
		},
		logout: func() (bool, error) {
			browserDriver.signedIn = false
			browserDriver.revision = ""
			return true, nil
		},
	}

	m := NewManager(ManagerConfig{
		OAuthDrivers:   map[string]OAuthDriver{"pkce": managerFakeOAuthDriver{tokenURL: tokenSrv.URL, account: "pkce@example.test"}},
		BrowserDrivers: map[string]BrowserOAuthDriver{"codex": browserDriver},
	})
	defer func() { _ = m.Close() }()

	// Simulate a previously settled browser flow followed by a runtime 401 expiry.
	_, err := m.Start(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := m.Status("codex"); err == nil && st.Status == "authenticated" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	m.RecordFailure("codex", HealthExpired, browserDriver.revision, "session expired on 401")
	st, err := m.Status("codex")
	if err != nil {
		t.Fatalf("Status(codex): %v", err)
	}
	if st.Status != string(HealthExpired) || st.Account != "dev@example.test" || st.Error != "session expired on 401" {
		t.Fatalf("Status(codex) after RecordFailure = %+v, want expired_auth with account preserved", st)
	}

	// Rotating the credential revision automatically clears the stale failure.
	browserDriver.revision = "rev-3"
	st, err = m.Status("codex")
	if err != nil {
		t.Fatalf("Status(codex) after revision bump: %v", err)
	}
	if st.Status != "authenticated" {
		t.Fatalf("Status(codex) after revision bump = %+v, want authenticated", st)
	}

	// API-key invalid_auth tracks SecretRevision and auto-clears on rotation.
	rev1 := SecretRevision("sk-old")
	rev2 := SecretRevision("sk-new")
	m.RecordFailure("openai", HealthInvalid, rev1, "401 Unauthorized")
	if h, ok := m.Health("openai", rev1); !ok || h.State != HealthInvalid {
		t.Fatalf("Health(openai, rev1) = %+v, %v, want invalid_auth", h, ok)
	}
	if _, ok := m.Health("openai", rev2); ok {
		t.Fatal("Health(openai, rev2) should auto-clear on key rotation")
	}

	// Static unrefreshable PKCE token reports expired_auth.
	if err := SaveProviderToken("pkce", &oauth2.Token{
		AccessToken: "old-access",
		Expiry:      time.Now().Add(-time.Hour),
	}, "pkce@example.test"); err != nil {
		t.Fatal(err)
	}
	pkceStatus, err := m.Status("pkce")
	if err != nil {
		t.Fatalf("Status(pkce): %v", err)
	}
	if pkceStatus.Status != string(HealthExpired) || pkceStatus.Account != "pkce@example.test" {
		t.Fatalf("Status(pkce) = %+v, want expired_auth", pkceStatus)
	}

	// Logout clears health.
	m.RecordFailure("codex", HealthExpired, browserDriver.revision, "expired")
	if _, err := m.Logout("codex", "codex"); err != nil {
		t.Fatalf("Logout(codex): %v", err)
	}
	if _, ok := m.Health("codex", ""); ok {
		t.Fatal("Logout should clear health entry")
	}
}
