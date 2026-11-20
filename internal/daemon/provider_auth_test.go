package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/providers"
	"golang.org/x/oauth2"
)

// fakeOAuthDriver drives a complete PKCE flow against an httptest token
// endpoint so provider auth endpoints can be exercised without a browser.
type fakeOAuthDriver struct {
	tokenURL string
	account  string
}

func (d fakeOAuthDriver) OAuthConfig() *oauth2.Config {
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

func (d fakeOAuthDriver) ExtractAccount(*oauth2.Token) string { return d.account }

func TestBrowserProviderAuthDiscardsResultAfterLogout(t *testing.T) {
	d, cli, _ := setupDaemonTest(t, nil)
	release := make(chan struct{})
	committed := false
	resp := d.beginBrowserAuthFlow(providers.CodexName, func(context.Context) (func() (string, error), error) {
		<-release
		return func() (string, error) {
			committed = true
			return "signed-in@example.test", nil
		}, nil
	})
	d.providerAuthMu.Lock()
	flow := d.providerAuths[providers.CodexName]
	d.providerAuthMu.Unlock()
	done, err := cli.LogoutProviderAuth(t.Context(), providers.CodexName)
	close(release)
	<-flow.settled
	if resp.Status != "authorizing" || resp.AuthURL != "" {
		t.Fatalf("start = %+v", resp)
	}
	if err != nil || done.Status != "idle" {
		t.Fatalf("logout = %+v, %v", done, err)
	}
	if committed {
		t.Fatal("a canceled browser flow restored the registration")
	}
}

// TestProviderAuthCallbackCompletesFlowEndToEnd drives the daemon's provider
// OAuth flow the way the SSH/headless path does: begin a flow, paste the
// redirect URL back, and see the token land on disk through the client API.
func TestProviderAuthCallbackCompletesFlowEndToEnd(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if r.FormValue("code") != "the-code" {
			http.Error(w, "unexpected code "+r.FormValue("code"), http.StatusBadRequest)
			return
		}
		if r.FormValue("code_verifier") == "" {
			http.Error(w, "missing PKCE verifier", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fresh-access","refresh_token":"fresh-refresh","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenSrv.Close()

	const prov = "authtest"
	auth.RegisterOAuthDriver(prov, fakeOAuthDriver{tokenURL: tokenSrv.URL, account: "dev@example.com"})

	_, cli, _ := setupDaemonTest(t, nil)
	ctx := context.Background()

	// A callback pasted before any flow exists must be refused, not silently
	// accepted.
	if _, err := cli.CompleteProviderAuth(ctx, prov, api.ProviderAuthCallbackRequest{CallbackURL: "code=early"}); err == nil {
		t.Fatal("expected completing a non-existent flow to fail")
	}

	start, err := cli.StartProviderAuth(ctx, prov)
	if err != nil {
		t.Fatalf("StartProviderAuth: %v", err)
	}
	if start.Status != "authorizing" || !strings.HasPrefix(start.AuthURL, "https://authorize.invalid/authorize?") {
		t.Fatalf("unexpected start response: %+v", start)
	}

	status, err := cli.GetProviderAuthStatus(ctx, prov)
	if err != nil {
		t.Fatalf("GetProviderAuthStatus: %v", err)
	}
	if status.Status != "authorizing" || status.AuthURL != start.AuthURL {
		t.Fatalf("authorizing status = %+v, want the started flow", status)
	}

	// A paste without a code is rejected by the daemon.
	if _, err := cli.CompleteProviderAuth(ctx, prov, api.ProviderAuthCallbackRequest{CallbackURL: "http://localhost:0/callback"}); err == nil {
		t.Error("expected a callback URL without a code to fail")
	}

	// The flow's state is the daemon's, so the paste must carry it. Read it
	// from the auth URL the daemon just published.
	state, _, _ := strings.Cut(strings.SplitN(start.AuthURL, "state=", 2)[1], "&")
	done, err := cli.CompleteProviderAuth(ctx, prov, api.ProviderAuthCallbackRequest{
		CallbackURL: "http://localhost:0/callback?code=the-code&state=" + state,
	})
	if err != nil {
		t.Fatalf("CompleteProviderAuth: %v", err)
	}
	if done.Status != "authenticated" || done.Account != "dev@example.com" {
		t.Fatalf("completion response = %+v, want authenticated as dev@example.com", done)
	}

	// The token landed on disk and the status endpoint now reports it.
	tok, email, err := auth.LoadProviderToken(prov)
	if err != nil {
		t.Fatalf("LoadProviderToken after completion: %v", err)
	}
	if tok.AccessToken != "fresh-access" || email != "dev@example.com" {
		t.Fatalf("persisted token = %+v email=%q, want fresh-access/dev@example.com", tok, email)
	}

	// Signing out removes the credentials.
	out, err := cli.LogoutProviderAuth(ctx, prov)
	if err != nil {
		t.Fatalf("LogoutProviderAuth: %v", err)
	}
	if out.Status != "signed-out" {
		t.Fatalf("logout status = %q, want signed-out", out.Status)
	}
	if auth.HasProviderToken(prov) {
		t.Error("credentials survived logout")
	}

	after, err := cli.GetProviderAuthStatus(ctx, prov)
	if err != nil {
		t.Fatalf("GetProviderAuthStatus after logout: %v", err)
	}
	if after.Status == "authenticated" {
		t.Errorf("status after logout = %+v, want not authenticated", after)
	}

	// A second logout is a no-op, not an error.
	again, err := cli.LogoutProviderAuth(ctx, prov)
	if err != nil {
		t.Fatalf("second LogoutProviderAuth: %v", err)
	}
	if again.Status != "idle" {
		t.Errorf("second logout status = %q, want idle", again.Status)
	}
}

// TestProviderAuthCallbackRejections covers the paste formats the daemon must
// refuse and the ones it must tolerate.
func TestProviderAuthCallbackRejections(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "full callback url", raw: "http://localhost:51121/callback?code=abc&state=xyz", wantErr: false},
		{name: "bare query", raw: "code=abc&state=xyz", wantErr: false},
		{name: "fragment params", raw: "http://localhost:51121/callback#code=abc&state=xyz", wantErr: false},
		{name: "bare code", raw: "abc", wantErr: false},
		{name: "empty", raw: "", wantErr: true},
		{name: "url without code", raw: "http://localhost:51121/callback", wantErr: true},
		{name: "query without code", raw: "state=xyz", wantErr: true},
		{name: "provider error", raw: "error=access_denied&state=xyz", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, err := parseProviderCallback(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseProviderCallback(%q) = (%q, nil), want an error", tt.raw, code)
				}
				var apiErr *api.APIError
				if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest || apiErr.Code != "bad_request" {
					t.Errorf("parseProviderCallback(%q) error = %v, want a 400 bad_request APIError", tt.raw, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProviderCallback(%q): %v", tt.raw, err)
			}
			if !tt.wantErr && tt.name == "bare code" && code != "abc" {
				t.Fatalf("bare code = %q, want abc", code)
			}
		})
	}
}

// TestListProvidersSummarizesPresets proves the endpoint the CLI auth status
// table and unnamed /login rely on.
func TestListProvidersSummarizesPresets(t *testing.T) {
	_, cli, _ := setupDaemonTest(t, nil)

	summaries, err := cli.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if len(summaries) == 0 {
		t.Fatal("ListProviders returned no presets")
	}
	var sawOAuth, sawBrowserOAuth bool
	for _, p := range summaries {
		if p.Name == "antigravity" {
			sawOAuth = true
			if p.AuthType != "oauth2" {
				t.Errorf("antigravity auth type = %q, want oauth2", p.AuthType)
			}
		}
		if p.Name == providers.CodexName {
			sawBrowserOAuth = true
			if p.AuthType != "oauth2" || p.BrowserAuthLabel != "Continue with ChatGPT" {
				t.Errorf("unexpected browser sign-in metadata: %+v", p)
			}
		}
	}
	if !sawOAuth {
		t.Error("antigravity missing from the provider listing")
	}
	if !sawBrowserOAuth {
		t.Error("codex missing from the provider listing")
	}
}

// TestLogoutProviderAuthUnknownProvider ensures logout is scoped to real
// providers without requiring an OAuth driver.
func TestLogoutProviderAuthUnknownProvider(t *testing.T) {
	d, cli, _ := setupDaemonTest(t, nil)
	ctx := context.Background()

	if _, err := d.LogoutProviderAuth(ctx, "   "); err == nil {
		t.Error("expected logout of a blank provider name to fail")
	}

	// A provider with no driver and no credentials logs out as a no-op.
	resp, err := cli.LogoutProviderAuth(ctx, "never-signed-in")
	if err != nil {
		t.Fatalf("LogoutProviderAuth(never-signed-in): %v", err)
	}
	if resp.Status != "idle" {
		t.Errorf("status = %q, want idle", resp.Status)
	}
}

// TestProviderAuthFlowCancellationKeepsOutcome proves a manually completed
// flow keeps its authenticated outcome when the loopback listener is
// afterwards cancelled by logout.
func TestProviderAuthFlowCancellationKeepsOutcome(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"a","refresh_token":"r","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenSrv.Close()

	const prov = "keepoutcome"
	auth.RegisterOAuthDriver(prov, fakeOAuthDriver{tokenURL: tokenSrv.URL, account: "keep@example.com"})

	d, _, _ := setupDaemonTest(t, nil)
	ctx := context.Background()

	resp, err := d.beginProviderAuthFlow(prov, fakeOAuthDriver{tokenURL: tokenSrv.URL, account: "keep@example.com"})
	if err != nil {
		t.Fatalf("beginProviderAuthFlow: %v", err)
	}
	state, _, _ := strings.Cut(strings.SplitN(resp.AuthURL, "state=", 2)[1], "&")

	d.providerAuthMu.Lock()
	flow := d.providerAuths[prov]
	d.providerAuthMu.Unlock()
	if flow == nil {
		t.Fatal("no flow registered for the provider")
	}

	if !flow.claim() {
		t.Fatal("first completion should claim the flow")
	}
	if flow.claim() {
		t.Fatal("a claimed flow must not be claimable again")
	}
	d.finishProviderAuth(ctx, flow, fakeOAuthDriver{tokenURL: tokenSrv.URL, account: "keep@example.com"}, "code-not-needed", state)
	d.failProviderAuthFlow(flow, context.Canceled)

	status, _, account, errMsg := flow.snapshot()
	if status != "authenticated" || account != "keep@example.com" || errMsg != "" {
		t.Fatalf("flow after late listener failure = (%q, %q, %q), want (authenticated, keep@example.com, \"\")", status, account, errMsg)
	}

	// Logout cancels the still-open listener and removes the credentials the
	// completed flow wrote.
	logoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := d.LogoutProviderAuth(logoutCtx, prov)
	if err != nil {
		t.Fatalf("LogoutProviderAuth: %v", err)
	}
	if out.Status != "signed-out" {
		t.Errorf("logout status = %q, want signed-out", out.Status)
	}
}
