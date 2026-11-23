package daemon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
)

type fakeBrowserAuthDriver struct {
	authorize func(context.Context) (func() (string, error), error)
	logout    func() (bool, error)
}

func (d fakeBrowserAuthDriver) Authorize(ctx context.Context) (func() (string, error), error) {
	return d.authorize(ctx)
}

func (d fakeBrowserAuthDriver) Logout() (bool, error)    { return d.logout() }
func (fakeBrowserAuthDriver) LoginLabel() string         { return "Fake browser sign-in" }
func (fakeBrowserAuthDriver) Account() (string, bool)    { return "", false }
func (fakeBrowserAuthDriver) CredentialRevision() string { return "" }

func newBrowserAuthTestDaemon() *Daemon {
	return &Daemon{
		providerAuths:      make(map[string]*providerAuthFlow),
		browserAuthWriteMu: make(map[string]*sync.Mutex),
	}
}

func awaitBrowserAuth[T any](t *testing.T, ch <-chan T, operation string) T {
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

func browserAuthCall(t *testing.T, call func() (*api.ProviderAuthResponse, error), wantStatus string) {
	t.Helper()
	type result struct {
		resp *api.ProviderAuthResponse
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := call()
		done <- result{resp, err}
	}()
	out := awaitBrowserAuth(t, done, wantStatus+" response")
	if out.err != nil || out.resp.Status != wantStatus {
		t.Fatalf("auth response = %+v, %v; want %s", out.resp, out.err, wantStatus)
	}
}

func browserAuthTestFlow(d *Daemon, provider string) *providerAuthFlow {
	d.providerAuthMu.Lock()
	defer d.providerAuthMu.Unlock()
	return d.providerAuths[provider]
}

func assertBrowserAuthWriteLocked(t *testing.T, d *Daemon, provider string) {
	t.Helper()
	mu := d.browserAuthWriteLock(provider)
	if mu.TryLock() {
		mu.Unlock()
		t.Fatalf("credential write for %s did not hold the provider lock", provider)
	}
}

func TestBrowserProviderAuthBlockedCommit(t *testing.T) {
	const provider, other = "blocked-browser-commit", "other-browser-auth"
	d := newBrowserAuthTestDaemon()
	ctx := t.Context()
	firstCtx := make(chan context.Context, 1)
	commitEntered := make(chan struct{})
	commitFinished := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)

	auth.RegisterBrowserOAuthDriver(provider, fakeBrowserAuthDriver{
		authorize: func(ctx context.Context) (func() (string, error), error) {
			firstCtx <- ctx
			return func() (string, error) {
				defer close(commitFinished)
				close(commitEntered)
				<-release
				return "old@example.test", nil
			}, nil
		},
	})
	auth.RegisterBrowserOAuthDriver(other, fakeBrowserAuthDriver{
		authorize: func(context.Context) (func() (string, error), error) {
			return func() (string, error) { return "other@example.test", nil }, nil
		},
		logout: func() (bool, error) { return true, nil },
	})

	browserAuthCall(t, func() (*api.ProviderAuthResponse, error) { return d.StartProviderAuth(ctx, provider) }, "authorizing")
	awaitBrowserAuth(t, commitEntered, "first commit entry")
	browserAuthCall(t, func() (*api.ProviderAuthResponse, error) { return d.GetProviderAuthStatus(ctx, provider) }, "authorizing")
	assertBrowserAuthWriteLocked(t, d, provider)
	first := browserAuthTestFlow(d, provider)
	browserAuthCall(t, func() (*api.ProviderAuthResponse, error) { return d.StartProviderAuth(ctx, other) }, "authorizing")
	awaitBrowserAuth(t, browserAuthTestFlow(d, other).settled, "other provider commit")
	browserAuthCall(t, func() (*api.ProviderAuthResponse, error) { return d.GetProviderAuthStatus(ctx, other) }, "authenticated")
	browserAuthCall(t, func() (*api.ProviderAuthResponse, error) { return d.LogoutProviderAuth(ctx, other) }, "signed-out")

	auth.RegisterBrowserOAuthDriver(provider, fakeBrowserAuthDriver{
		authorize: func(context.Context) (func() (string, error), error) {
			return func() (string, error) {
				select {
				case <-commitFinished:
					return "new@example.test", nil
				default:
					return "", errors.New("replacement commit overlapped the first commit")
				}
			}, nil
		},
	})
	browserAuthCall(t, func() (*api.ProviderAuthResponse, error) { return d.StartProviderAuth(ctx, provider) }, "authorizing")
	second := browserAuthTestFlow(d, provider)
	awaitBrowserAuth(t, (<-firstCtx).Done(), "replaced flow cancellation")
	unblock()
	awaitBrowserAuth(t, first.settled, "replaced flow settlement")
	awaitBrowserAuth(t, second.settled, "replacement flow settlement")
	if out := first.toAPI(); out.Status != "error" || out.Error != context.Canceled.Error() {
		t.Fatalf("replaced flow = %+v; want canceled", out)
	}
	if out := second.toAPI(); out.Status != "authenticated" || out.Account != "new@example.test" {
		t.Fatalf("replacement flow = %+v; want authenticated as new@example.test", out)
	}
}

func TestBrowserProviderAuthStartDuringLogout(t *testing.T) {
	const provider = "blocked-browser-logout"
	d := newBrowserAuthTestDaemon()
	ctx := t.Context()
	logoutEntered := make(chan struct{})
	logoutFinished := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)

	auth.RegisterBrowserOAuthDriver(provider, fakeBrowserAuthDriver{
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
	})

	logoutDone := make(chan error, 1)
	go func() {
		_, err := d.LogoutProviderAuth(ctx, provider)
		logoutDone <- err
	}()
	awaitBrowserAuth(t, logoutEntered, "logout entry")
	browserAuthCall(t, func() (*api.ProviderAuthResponse, error) { return d.GetProviderAuthStatus(ctx, provider) }, "idle")
	assertBrowserAuthWriteLocked(t, d, provider)
	browserAuthCall(t, func() (*api.ProviderAuthResponse, error) { return d.StartProviderAuth(ctx, provider) }, "authorizing")
	flow := browserAuthTestFlow(d, provider)
	browserAuthCall(t, func() (*api.ProviderAuthResponse, error) { return d.GetProviderAuthStatus(ctx, provider) }, "authorizing")
	unblock()
	if err := awaitBrowserAuth(t, logoutDone, "logout completion"); err != nil {
		t.Fatalf("logout: %v", err)
	}
	awaitBrowserAuth(t, flow.settled, "new flow settlement")
	if out := flow.toAPI(); out.Status != "authenticated" || out.Account != "new@example.test" {
		t.Fatalf("flow started during logout = %+v; want authenticated as new@example.test", out)
	}
}
