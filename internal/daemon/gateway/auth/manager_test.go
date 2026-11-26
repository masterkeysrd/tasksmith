package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/testutils"
	"golang.org/x/oauth2"
)

type managerFakeOAuthDriver struct {
	tokenURL string
	account  string
}

func (d managerFakeOAuthDriver) OAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     "fake-client",
		ClientSecret: "fake-secret",
		RedirectURL:  "http://localhost:0/callback",
		Scopes:       []string{"openid"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://authorize.invalid/authorize",
			TokenURL: d.tokenURL,
		},
	}
}

func (d managerFakeOAuthDriver) ExtractAccount(*oauth2.Token) string { return d.account }

type managerFakeBrowserDriver struct {
	label     string
	account   string
	signedIn  bool
	revision  string
	authorize func(context.Context) (func() (string, error), error)
	logout    func() (bool, error)
	closeErr  error
	closed    atomic.Int32
}

func (d *managerFakeBrowserDriver) LoginLabel() string {
	if d.label != "" {
		return d.label
	}
	return "Fake browser sign-in"
}

func (d *managerFakeBrowserDriver) Authorize(ctx context.Context) (func() (string, error), error) {
	return d.authorize(ctx)
}

func (d *managerFakeBrowserDriver) Account() (string, bool)    { return d.account, d.signedIn }
func (d *managerFakeBrowserDriver) CredentialRevision() string { return d.revision }
func (d *managerFakeBrowserDriver) Logout() (bool, error) {
	if d.logout != nil {
		return d.logout()
	}
	return false, nil
}

func (d *managerFakeBrowserDriver) Close() error {
	d.closed.Add(1)
	return d.closeErr
}

func awaitChannel[T any](t *testing.T, ch <-chan T, operation string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", operation)
		var zero T
		return zero
	}
}

func assertBrowserWriteLocked(t *testing.T, m *Manager, provider string) {
	t.Helper()
	mu := m.browserWriteLock(provider)
	if mu.TryLock() {
		mu.Unlock()
		t.Fatalf("credential write for %s did not hold the provider lock", provider)
	}
}

func TestManagerPKCELifecycleAndQueries(t *testing.T) {
	testutils.SetupXDG(t, "tasksmith-auth-manager-test")

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fresh-access","refresh_token":"fresh-refresh","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenSrv.Close()

	var authHooks atomic.Int32
	browser := &managerFakeBrowserDriver{
		label:    "Continue with Fake",
		account:  "browser@example.test",
		signedIn: true,
		revision: "rev-1",
		closeErr: errors.New("browser close failed"),
	}
	mgr := NewManager(ManagerConfig{
		OnAuthenticated: func(context.Context) { authHooks.Add(1) },
		OAuthDrivers: map[string]OAuthDriver{
			"pkce": managerFakeOAuthDriver{tokenURL: tokenSrv.URL, account: "pkce@example.test"},
		},
		BrowserDrivers: map[string]BrowserOAuthDriver{
			"browser": browser,
		},
	})

	if !mgr.HasOAuth("pkce") || !mgr.HasOAuth("browser") || mgr.HasOAuth("unknown") {
		t.Fatal("unexpected HasOAuth results")
	}
	if mgr.IsCanonicalBrowserOnly("pkce") || !mgr.IsCanonicalBrowserOnly("browser") {
		t.Fatal("unexpected IsCanonicalBrowserOnly results")
	}
	if mgr.LoginLabel("browser") != "Continue with Fake" || mgr.LoginLabel("pkce") != "" {
		t.Fatal("unexpected LoginLabel results")
	}
	if acct, ok := mgr.Account("browser", "browser"); !ok || acct != "browser@example.test" {
		t.Fatalf("browser Account = (%q, %t)", acct, ok)
	}
	if rev := mgr.CredentialRevision("browser", "browser"); rev != "rev-1" {
		t.Fatalf("browser CredentialRevision = %q", rev)
	}

	ctx := t.Context()
	if _, err := mgr.Complete(ctx, "pkce", "code=early"); !errors.Is(err, ErrNoFlowInProgress) {
		t.Fatalf("Complete before Start = %v, want ErrNoFlowInProgress", err)
	}
	if _, err := mgr.Complete(ctx, "browser", "code=x"); !errors.Is(err, ErrBrowserHostOnly) {
		t.Fatalf("Complete browser driver = %v, want ErrBrowserHostOnly", err)
	}
	if _, err := mgr.Start(ctx, "unknown"); !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("Start unknown = %v, want ErrUnsupportedProvider", err)
	}

	started, err := mgr.Start(ctx, "pkce")
	if err != nil || started.Status != "authorizing" || !strings.HasPrefix(started.AuthURL, "https://authorize.invalid/authorize?") {
		t.Fatalf("Start(pkce) = %+v, %v", started, err)
	}
	state, _, _ := strings.Cut(strings.SplitN(started.AuthURL, "state=", 2)[1], "&")
	done, err := mgr.Complete(ctx, "pkce", "http://localhost:0/callback?code=the-code&state="+state)
	if err != nil || done.Status != "authenticated" || done.Account != "pkce@example.test" {
		t.Fatalf("Complete(pkce) = %+v, %v", done, err)
	}
	if authHooks.Load() != 1 {
		t.Fatalf("OnAuthenticated calls = %d, want 1", authHooks.Load())
	}
	if acct, ok := mgr.Account("pkce", "pkce"); !ok || acct != "pkce@example.test" {
		t.Fatalf("pkce Account after Complete = (%q, %t)", acct, ok)
	}
	if rev := mgr.CredentialRevision("pkce", "pkce"); rev == "" || rev == "missing" {
		t.Fatalf("pkce CredentialRevision after Complete = %q", rev)
	}

	deleted, err := mgr.Logout("pkce", "pkce")
	if err != nil || !deleted {
		t.Fatalf("Logout(pkce) = (%t, %v), want (true, nil)", deleted, err)
	}
	if _, ok := mgr.Account("pkce", "pkce"); ok {
		t.Fatal("pkce account still present after Logout")
	}

	if err := mgr.Close(); !errors.Is(err, browser.closeErr) || browser.closed.Load() != 1 {
		t.Fatalf("Close() = %v (closed=%d), want %v", err, browser.closed.Load(), browser.closeErr)
	}
}

func TestManagerFlowCancellationKeepsOutcome(t *testing.T) {
	testutils.SetupXDG(t, "tasksmith-auth-keepoutcome-test")

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"a","refresh_token":"r","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenSrv.Close()

	driver := managerFakeOAuthDriver{tokenURL: tokenSrv.URL, account: "keep@example.com"}
	mgr := NewManager(ManagerConfig{
		OAuthDrivers:   map[string]OAuthDriver{"keepoutcome": driver},
		BrowserDrivers: map[string]BrowserOAuthDriver{},
	})
	t.Cleanup(func() { _ = mgr.Close() })

	ctx := t.Context()
	snap, err := mgr.beginPKCEFlow("keepoutcome", driver)
	if err != nil {
		t.Fatalf("beginPKCEFlow: %v", err)
	}
	state, _, _ := strings.Cut(strings.SplitN(snap.AuthURL, "state=", 2)[1], "&")

	mgr.mu.Lock()
	flow := mgr.flows["keepoutcome"]
	mgr.mu.Unlock()

	if !flow.claim() {
		t.Fatal("first completion should claim the flow")
	}
	if flow.claim() {
		t.Fatal("a claimed flow must not be claimable again")
	}
	mgr.finishPKCEFlow(ctx, flow, driver, "code", state)
	mgr.failFlow(flow, context.Canceled)

	got := flow.snapshot()
	if got.Status != "authenticated" || got.Account != "keep@example.com" || got.Error != "" {
		t.Fatalf("flow after late listener failure = %+v, want authenticated/keep@example.com", got)
	}

	deleted, err := mgr.Logout("keepoutcome", "keepoutcome")
	if err != nil || !deleted {
		t.Fatalf("Logout = (%t, %v), want (true, nil)", deleted, err)
	}
}

func TestManagerBrowserAuthBlockedCommit(t *testing.T) {
	const provider, other = "blocked-browser-commit", "other-browser-auth"
	ctx := t.Context()
	firstCtx := make(chan context.Context, 1)
	commitEntered := make(chan struct{})
	commitFinished := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)

	var callCount atomic.Int32
	mgr := NewManager(ManagerConfig{
		OAuthDrivers: map[string]OAuthDriver{},
		BrowserDrivers: map[string]BrowserOAuthDriver{
			provider: &managerFakeBrowserDriver{
				authorize: func(ctx context.Context) (func() (string, error), error) {
					if callCount.Add(1) == 1 {
						firstCtx <- ctx
						return func() (string, error) {
							defer close(commitFinished)
							close(commitEntered)
							<-release
							return "old@example.test", nil
						}, nil
					}
					return func() (string, error) {
						select {
						case <-commitFinished:
							return "new@example.test", nil
						default:
							return "", errors.New("replacement commit overlapped the first commit")
						}
					}, nil
				},
			},
			other: &managerFakeBrowserDriver{
				authorize: func(context.Context) (func() (string, error), error) {
					return func() (string, error) { return "other@example.test", nil }, nil
				},
				logout: func() (bool, error) { return true, nil },
			},
		},
	})
	t.Cleanup(func() { _ = mgr.Close() })

	if snap, err := mgr.Start(ctx, provider); err != nil || snap.Status != "authorizing" {
		t.Fatalf("Start(provider) = %+v, %v", snap, err)
	}
	awaitChannel(t, commitEntered, "first commit entry")
	if snap, err := mgr.Status(provider); err != nil || snap.Status != "authorizing" {
		t.Fatalf("Status(provider) = %+v, %v", snap, err)
	}
	assertBrowserWriteLocked(t, mgr, provider)

	mgr.mu.Lock()
	first := mgr.flows[provider]
	mgr.mu.Unlock()

	if snap, err := mgr.Start(ctx, other); err != nil || snap.Status != "authorizing" {
		t.Fatalf("Start(other) = %+v, %v", snap, err)
	}
	mgr.mu.Lock()
	otherFlow := mgr.flows[other]
	mgr.mu.Unlock()
	awaitChannel(t, otherFlow.settled, "other provider commit")
	if snap, err := mgr.Status(other); err != nil || snap.Status != "authenticated" {
		t.Fatalf("Status(other) = %+v, %v", snap, err)
	}
	if deleted, err := mgr.Logout(other, other); err != nil || !deleted {
		t.Fatalf("Logout(other) = %t, %v", deleted, err)
	}

	if snap, err := mgr.Start(ctx, provider); err != nil || snap.Status != "authorizing" {
		t.Fatalf("replacement Start(provider) = %+v, %v", snap, err)
	}
	mgr.mu.Lock()
	second := mgr.flows[provider]
	mgr.mu.Unlock()

	awaitChannel(t, (<-firstCtx).Done(), "replaced flow cancellation")
	unblock()
	awaitChannel(t, first.settled, "replaced flow settlement")
	awaitChannel(t, second.settled, "replacement flow settlement")
	if out := first.snapshot(); out.Status != "error" || out.Error != context.Canceled.Error() {
		t.Fatalf("replaced flow = %+v; want canceled", out)
	}
	if out := second.snapshot(); out.Status != "authenticated" || out.Account != "new@example.test" {
		t.Fatalf("replacement flow = %+v; want authenticated as new@example.test", out)
	}
}

func TestManagerBrowserAuthStartDuringLogout(t *testing.T) {
	const provider = "blocked-browser-logout"
	ctx := t.Context()
	logoutEntered := make(chan struct{})
	logoutFinished := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)

	mgr := NewManager(ManagerConfig{
		OAuthDrivers: map[string]OAuthDriver{},
		BrowserDrivers: map[string]BrowserOAuthDriver{
			provider: &managerFakeBrowserDriver{
				authorize: func(context.Context) (func() (string, error), error) {
					return func() (string, error) {
						select {
						case <-logoutFinished:
							return "new@example.test", nil
						default:
							return "", errors.New("commit overlapped credential deletion")
						}
					}, nil
				},
				logout: func() (bool, error) {
					defer close(logoutFinished)
					close(logoutEntered)
					<-release
					return true, nil
				},
			},
		},
	})
	t.Cleanup(func() { _ = mgr.Close() })

	logoutDone := make(chan error, 1)
	go func() {
		_, err := mgr.Logout(provider, provider)
		logoutDone <- err
	}()
	awaitChannel(t, logoutEntered, "logout entry")
	if snap, err := mgr.Status(provider); err != nil || snap.Status != "idle" {
		t.Fatalf("Status during logout = %+v, %v", snap, err)
	}
	assertBrowserWriteLocked(t, mgr, provider)
	if snap, err := mgr.Start(ctx, provider); err != nil || snap.Status != "authorizing" {
		t.Fatalf("Start during logout = %+v, %v", snap, err)
	}
	mgr.mu.Lock()
	flow := mgr.flows[provider]
	mgr.mu.Unlock()
	if snap, err := mgr.Status(provider); err != nil || snap.Status != "authorizing" {
		t.Fatalf("Status after Start = %+v, %v", snap, err)
	}
	unblock()
	if err := awaitChannel(t, logoutDone, "logout completion"); err != nil {
		t.Fatalf("logout: %v", err)
	}
	awaitChannel(t, flow.settled, "new flow settlement")
	if out := flow.snapshot(); out.Status != "authenticated" || out.Account != "new@example.test" {
		t.Fatalf("flow started during logout = %+v; want authenticated as new@example.test", out)
	}
}

func TestManagerBrowserAuthDiscardsResultAfterLogout(t *testing.T) {
	const provider = "stale-browser-flow"
	release := make(chan struct{})
	committed := false
	mgr := NewManager(ManagerConfig{
		OAuthDrivers: map[string]OAuthDriver{},
		BrowserDrivers: map[string]BrowserOAuthDriver{
			provider: &managerFakeBrowserDriver{
				authorize: func(context.Context) (func() (string, error), error) {
					<-release
					return func() (string, error) {
						committed = true
						return "signed-in@example.test", nil
					}, nil
				},
			},
		},
	})
	t.Cleanup(func() { _ = mgr.Close() })

	resp, err := mgr.Start(t.Context(), provider)
	if err != nil || resp.Status != "authorizing" || resp.AuthURL != "" {
		close(release)
		t.Fatalf("Start = %+v, %v", resp, err)
	}
	mgr.mu.Lock()
	flow := mgr.flows[provider]
	mgr.mu.Unlock()

	deleted, err := mgr.Logout(provider, provider)
	close(release)
	awaitChannel(t, flow.settled, "canceled flow settlement")
	if err != nil || deleted {
		t.Fatalf("Logout = %t, %v; want false, nil", deleted, err)
	}
	if committed {
		t.Fatal("a canceled browser flow committed the registration")
	}
}
