package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	loomauth "github.com/masterkeysrd/loom/auth"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/providers"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
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

func TestBrowserProviderAuthDiscardsResultAfterLogout(t *testing.T) {
	const provider = "stale-browser-flow"
	_, cli, _ := setupDaemonTest(t, nil)
	release := make(chan struct{})
	authorizeDone := make(chan struct{})
	committed := false
	auth.RegisterBrowserOAuthDriver(provider, fakeBrowserAuthDriver{
		authorize: func(context.Context) (func() (string, error), error) {
			defer close(authorizeDone)
			<-release
			return func() (string, error) {
				committed = true
				return "signed-in@example.test", nil
			}, nil
		},
		logout: func() (bool, error) { return false, nil },
	})
	resp, err := cli.StartProviderAuth(t.Context(), provider)
	if err != nil {
		close(release)
		t.Fatalf("start: %v", err)
	}
	done, err := cli.LogoutProviderAuth(t.Context(), provider)
	close(release)
	select {
	case <-authorizeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for canceled authorize to exit")
	}
	if resp.Status != "authorizing" || resp.AuthURL != "" {
		t.Fatalf("start = %+v", resp)
	}
	if err != nil || done.Status != "idle" {
		t.Fatalf("logout = %+v, %v", done, err)
	}
	// Allow the goroutine to check cancellation after authorize returns.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, err := cli.GetProviderAuthStatus(t.Context(), provider)
		if err == nil && status.Status == "idle" {
			break
		}
		time.Sleep(10 * time.Millisecond)
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

func TestProviderAuthAPIErrorMapping(t *testing.T) {
	for _, sentinel := range []error{
		auth.ErrUnsupportedProvider,
		auth.ErrInvalidCallback,
		auth.ErrNoFlowInProgress,
		auth.ErrBrowserHostOnly,
		auth.ErrInvalidProviderName,
	} {
		err := providerAuthAPIError(sentinel)
		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest || apiErr.Code != "bad_request" {
			t.Errorf("providerAuthAPIError(%v) = %v, want 400 bad_request APIError", sentinel, err)
		}
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

type healthTestBrowserDriver struct {
	mu       sync.Mutex
	account  string
	signedIn bool
	revision string
}

func (d *healthTestBrowserDriver) LoginLabel() string { return "Continue with ChatGPT" }
func (d *healthTestBrowserDriver) Account() (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.account, d.signedIn
}
func (d *healthTestBrowserDriver) CredentialRevision() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.revision
}
func (d *healthTestBrowserDriver) Logout() (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	was := d.signedIn
	d.signedIn = false
	d.revision = ""
	return was, nil
}
func (d *healthTestBrowserDriver) Authorize(context.Context) (func() (string, error), error) {
	return func() (string, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.signedIn = true
		d.account = "dev@example.test"
		d.revision = "rev-relogged"
		return d.account, nil
	}, nil
}

// authFailingGateway wraps a real gateway or fake provider to inject a
// ProviderAuthError and notify cfg.AuthObserver just like gateway middleware.
type authFailingPool struct {
	prov *fakeProvider
}

func (p *authFailingPool) Acquire(_ context.Context, cfg gateway.Config) (gateway.Gateway, error) {
	return &authFailingGateway{prov: p.prov, observer: cfg.AuthObserver}, nil
}
func (p *authFailingPool) Close() error { return nil }

type authFailingGateway struct {
	fakeGateway
	prov     *fakeProvider
	observer gateway.AuthObserver
}

func (g *authFailingGateway) LLM(_ context.Context, m models.Model, cfg llm.ModelConfig) (*llm.Model, error) {
	if g.observer != nil && m.ID.Provider() == providers.CodexName {
		rev := auth.CredentialRevision(providers.CodexName, providers.CodexName)
		if rev == "rev-initial" {
			g.observer.RecordAuthFailure(providers.CodexName, providers.CodexName, auth.HealthExpired, rev, "token expired")
			return nil, &gateway.ProviderAuthError{
				Provider:     providers.CodexName,
				ProviderType: providers.CodexName,
				Kind:         gateway.AuthErrorExpired,
				OAuth:        true,
				Cause:        loomauth.ErrAuthRequired,
			}
		}
	}
	return llm.NewModel(g.prov, m.ID.Name(), &cfg)
}

func TestDaemonLiveAuthHealthTurnFailureAndReloginRecovery(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-daemon-auth-health-test")
	t.Cleanup(xdg.ClearCache)

	origCodex, hadCodex := auth.GetBrowserOAuthDriver(providers.CodexName)
	driver := &healthTestBrowserDriver{
		account:  "dev@example.test",
		signedIn: true,
		revision: "rev-initial",
	}
	auth.RegisterBrowserOAuthDriver(providers.CodexName, driver)
	t.Cleanup(func() {
		if hadCodex {
			auth.RegisterBrowserOAuthDriver(providers.CodexName, origCodex)
		}
	})

	runDir := shortTempDir(t)
	sockPath := filepath.Join(runDir, "b.sock")
	lockPath := filepath.Join(runDir, "b.lock")

	srv, err := New(Config{
		SocketPath:            sockPath,
		LockPath:              lockPath,
		GatewayPool:           &authFailingPool{prov: &fakeProvider{}},
		Logger:                log.Default(),
		DisableMCPAutoConnect: true,
	})
	if err != nil {
		t.Fatalf("daemon.New: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("srv.Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close(context.Background()) })

	wsDir := shortTempDir(t)
	if err := workspace.TrustFolder(wsDir, workspace.TrustScopeFolder); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wsDir, ".agents", "providers"), 0o755); err != nil {
		t.Fatal(err)
	}
	wsManifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: auth-health\nspec:\n  defaultProvider: codex\n  defaultModels:\n    completion: gpt-5.3-codex\n---\n"
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	codexManifest := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: codex
spec:
  type: codex
  auth:
    type: oauth2
  defaultModels:
    completion: gpt-5.3-codex
  models:
    - id: gpt-5.3-codex
      name: gpt-5.3-codex
`
	if err := os.WriteFile(filepath.Join(wsDir, ".agents", "providers", "codex.provider.yaml"), []byte(codexManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	cli, err := client.New(sockPath, client.WithCWD(wsDir))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	ctx := context.Background()

	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{Title: "auth-health"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	gotErrEv := sendAndAwaitErrorEvent(t, cli, sess.ID, "hello")
	if gotErrEv.Code != api.ErrCodeProviderAuthExpired || gotErrEv.Provider != providers.CodexName {
		t.Fatalf("ErrorEvent = %+v, want code=%q provider=%q", gotErrEv, api.ErrCodeProviderAuthExpired, providers.CodexName)
	}
	if !strings.Contains(gotErrEv.Error, "/login codex") {
		t.Fatalf("ErrorEvent.Error = %q, want /login codex guidance", gotErrEv.Error)
	}

	// ListWorkspaceProviders reports expired_auth while keeping account email.
	wsProvs, err := cli.ListWorkspaceProviders(ctx)
	if err != nil {
		t.Fatalf("ListWorkspaceProviders: %v", err)
	}
	var codexRow *api.WorkspaceProvider
	for i := range wsProvs {
		if wsProvs[i].Name == providers.CodexName {
			codexRow = &wsProvs[i]
		}
	}
	if codexRow == nil || codexRow.Status != "expired_auth" || codexRow.Account != "dev@example.test" || codexRow.HasKey {
		t.Fatalf("codex workspace provider = %+v, want status=expired_auth account=dev@example.test hasKey=false", codexRow)
	}

	// GetProviderAuthStatus reports expired_auth with account preserved.
	authSt, err := cli.GetProviderAuthStatus(ctx, providers.CodexName)
	if err != nil {
		t.Fatalf("GetProviderAuthStatus: %v", err)
	}
	if authSt.Status != "expired_auth" || authSt.Account != "dev@example.test" {
		t.Fatalf("GetProviderAuthStatus = %+v, want expired_auth + account", authSt)
	}

	// ListProviders reports expired session status.
	presets, err := cli.ListProviders(ctx)
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	var codexPreset *api.ProviderSummary
	for i := range presets {
		if presets[i].Name == providers.CodexName {
			codexPreset = &presets[i]
		}
	}
	if codexPreset == nil || !strings.Contains(codexPreset.Status, "session expired") || codexPreset.HasKey {
		t.Fatalf("ListProviders codex = %+v, want session expired", codexPreset)
	}

	// GetWorkspace warnings include runtime expired_auth diagnostic.
	wsInfo, err := cli.GetWorkspace(ctx)
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	foundWarn := false
	for _, w := range wsInfo.Warnings {
		if strings.Contains(w, `provider "codex"`) && strings.Contains(w, "/login codex") {
			foundWarn = true
			break
		}
	}
	if !foundWarn {
		t.Fatalf("expected codex expired_auth warning in GetWorkspace.Warnings, got %v", wsInfo.Warnings)
	}

	// Completing re-login clears the warning and restores active status.
	if _, err := cli.StartProviderAuth(ctx, providers.CodexName); err != nil {
		t.Fatalf("StartProviderAuth: %v", err)
	}
	pollDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(pollDeadline) {
		st, err := cli.GetProviderAuthStatus(ctx, providers.CodexName)
		if err == nil && st.Status == "authenticated" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	wsProvsAfter, err := cli.ListWorkspaceProviders(ctx)
	if err != nil {
		t.Fatalf("ListWorkspaceProviders after re-login: %v", err)
	}
	for _, p := range wsProvsAfter {
		if p.Name == providers.CodexName && (p.Status != "active" || !p.HasKey) {
			t.Fatalf("codex after re-login = %+v, want active", p)
		}
	}
	wsInfoAfter, err := cli.GetWorkspace(ctx)
	if err != nil {
		t.Fatalf("GetWorkspace after re-login: %v", err)
	}
	for _, w := range wsInfoAfter.Warnings {
		if strings.Contains(w, `provider "codex"`) && strings.Contains(w, "expired") {
			t.Fatalf("stale codex expired warning remained after re-login: %v", wsInfoAfter.Warnings)
		}
	}
}

func TestDaemonInvalidAPIKeyReportedInProvidersAndWarnings(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-daemon-invalid-key-test")
	t.Cleanup(xdg.ClearCache)

	// Fake upstream OpenAI server returning 401 Unauthorized.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided","type":"invalid_request_error","code":"invalid_api_key"}}`))
	}))
	defer upstream.Close()

	t.Setenv("OPENAI_API_KEY", "sk-bad-key")

	runDir := shortTempDir(t)
	sockPath := filepath.Join(runDir, "b.sock")
	lockPath := filepath.Join(runDir, "b.lock")

	pool := gateway.NewPool()
	t.Cleanup(func() { _ = pool.Close() })

	srv, err := New(Config{
		SocketPath:            sockPath,
		LockPath:              lockPath,
		GatewayPool:           pool,
		Logger:                log.Default(),
		DisableMCPAutoConnect: true,
	})
	if err != nil {
		t.Fatalf("daemon.New: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("srv.Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close(context.Background()) })

	wsDir := shortTempDir(t)
	if err := workspace.TrustFolder(wsDir, workspace.TrustScopeFolder); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wsDir, ".agents", "providers"), 0o755); err != nil {
		t.Fatal(err)
	}
	wsManifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: invalid-key\nspec:\n  defaultProvider: openai\n  defaultModels:\n    completion: gpt-4o-mini\n---\n"
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	openaiManifest := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: openai
spec:
  type: openai
  endpoint: ` + upstream.URL + `/v1
  auth:
    type: bearer
    token: ${env:OPENAI_API_KEY}
  defaultModels:
    completion: gpt-4o-mini
  models:
    - id: gpt-4o-mini
      name: gpt-4o-mini
`
	if err := os.WriteFile(filepath.Join(wsDir, ".agents", "providers", "openai.provider.yaml"), []byte(openaiManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	cli, err := client.New(sockPath, client.WithCWD(wsDir))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	ctx := context.Background()

	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{Title: "bad-key"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	gotErrEv := sendAndAwaitErrorEvent(t, cli, sess.ID, "hello")
	if gotErrEv.Code != api.ErrCodeProviderAuthInvalid || gotErrEv.Provider != "openai" {
		t.Fatalf("ErrorEvent = %+v, want code=%q provider=openai", gotErrEv, api.ErrCodeProviderAuthInvalid)
	}

	wsProvs, err := cli.ListWorkspaceProviders(ctx)
	if err != nil {
		t.Fatalf("ListWorkspaceProviders: %v", err)
	}
	var openaiRow *api.WorkspaceProvider
	for i := range wsProvs {
		if wsProvs[i].Name == "openai" {
			openaiRow = &wsProvs[i]
		}
	}
	if openaiRow == nil || openaiRow.Status != "invalid_auth" || openaiRow.HasKey {
		t.Fatalf("openai workspace provider = %+v, want invalid_auth", openaiRow)
	}

	wsInfo, err := cli.GetWorkspace(ctx)
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	foundWarn := false
	for _, w := range wsInfo.Warnings {
		if strings.Contains(w, `provider "openai"`) && strings.Contains(w, "401 Unauthorized") {
			foundWarn = true
			break
		}
	}
	if !foundWarn {
		t.Fatalf("expected openai invalid_auth warning in GetWorkspace.Warnings, got %v", wsInfo.Warnings)
	}
}

func sendAndAwaitErrorEvent(t *testing.T, cli *client.Client, sessionID, content string) api.ErrorEvent {
	t.Helper()
	evCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ready := make(chan struct{}, 1)
	errCh := make(chan api.ErrorEvent, 1)
	streamErrCh := make(chan error, 1)

	go func() {
		for ev, err := range cli.EventsWithReady(evCtx, "", "", func(string) {
			select {
			case ready <- struct{}{}:
			default:
			}
		}, api.SessionStreamTopic(sessionID)) {
			if err != nil {
				streamErrCh <- err
				return
			}
			if ev.Type == api.EventError {
				if p, ok := ev.Payload.(api.ErrorEvent); ok {
					errCh <- p
					return
				}
			}
		}
	}()

	select {
	case <-ready:
	case err := <-streamErrCh:
		t.Fatalf("event stream failed before ready: %v", err)
	case <-evCtx.Done():
		t.Fatal("timed out waiting for event stream ready")
	}

	if err := cli.Send(context.Background(), sessionID, api.SendRequest{Content: content}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case p := <-errCh:
		return p
	case err := <-streamErrCh:
		t.Fatalf("event stream error before ErrorEvent: %v", err)
	case <-evCtx.Done():
		t.Fatal("timed out waiting for ErrorEvent")
	}
	return api.ErrorEvent{}
}
