package daemon

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/loom/auth"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/env"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

type providerAuthFlow struct {
	mu       sync.Mutex
	provider string
	status   string
	authURL  string
	account  string
	err      string
	cancel   context.CancelFunc
	session  *auth.AuthSession
	settled  chan struct{}
	claimed  bool
}

// claim atomically takes exclusive ownership of completing the flow. Only the
// goroutine that claims an authorizing flow may exchange its one-time
// authorization code, which keeps a manually pasted callback and the loopback
// listener from exchanging the same code concurrently.
func (f *providerAuthFlow) claim() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.status != "authorizing" || f.claimed || f.session == nil {
		return false
	}
	f.claimed = true
	return true
}

// snapshot returns the flow's observable state under its lock.
func (f *providerAuthFlow) snapshot() (status, authURL, account, errMsg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, f.authURL, f.account, f.err
}

// settle records a status, account, and error message under the flow lock.
func (f *providerAuthFlow) settle(status, account, errMsg string) {
	f.mu.Lock()
	f.status = status
	f.account = account
	f.err = errMsg
	f.mu.Unlock()
}

// toAPI snapshots the flow into its API response representation under the lock.
func (f *providerAuthFlow) toAPI() api.ProviderAuthResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	return api.ProviderAuthResponse{
		Provider: f.provider,
		AuthURL:  f.authURL,
		Status:   f.status,
		Account:  f.account,
		Error:    f.err,
	}
}

func findPreset(name string) *warp.ModelProvider {
	for _, p := range workspace.Presets() {
		if p.Metadata.Name == name {
			return p
		}
	}
	return nil
}

func loadUserEnv() map[string]string {
	cfgDir, err := xdg.SubConfigDir()
	if err != nil {
		return nil
	}
	userEnv, _ := env.LoadFile(filepath.Join(cfgDir, ".env"))
	return userEnv
}

func resolvePresetAuth(p *warp.ModelProvider, envLayers ...map[string]string) (hasKey bool, account, status string) {
	if p == nil {
		return false, "", ""
	}
	if p.Spec.Auth != nil && p.Spec.Auth.Type == warp.AuthTypeOAuth2 {
		if tok, accountEmail, err := gateway.LoadProviderToken(p.Metadata.Name); err == nil && tok != nil {
			if accountEmail != "" {
				return true, accountEmail, fmt.Sprintf("✓ %s (OAuth)", accountEmail)
			}
			return true, "", "✓ OAuth authenticated"
		}
	}
	if authEnv := workspace.ProviderAuthPrimaryEnv(p.Spec.Auth); authEnv != "" {
		val := os.Getenv(authEnv)
		for _, layer := range envLayers {
			if strings.TrimSpace(val) != "" {
				break
			}
			if layer != nil {
				val = layer[authEnv]
			}
		}
		if strings.TrimSpace(val) != "" {
			return true, "", fmt.Sprintf("✓ %s detected", authEnv)
		}
	}
	return false, "", ""
}

func (d *Daemon) VerifyProvider(ctx context.Context, req api.VerifyProviderRequest) (*api.VerifyProviderResponse, error) {
	provName := strings.TrimSpace(req.Provider)
	if provName == "" {
		return &api.VerifyProviderResponse{
			Valid:        false,
			ErrorMessage: "provider name is required",
		}, nil
	}

	foundPreset := findPreset(provName)

	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" && req.AuthEnv != "" {
		apiKey = strings.TrimSpace(os.Getenv(req.AuthEnv))
	}

	if provName == "ollama" {
		endpoint := req.Endpoint
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(endpoint + "/api/tags")
		if err != nil {
			return &api.VerifyProviderResponse{
				Valid:        false,
				ErrorMessage: fmt.Sprintf("failed to reach Ollama at %s: %v", endpoint, err),
			}, nil
		}
		_ = resp.Body.Close()
		return &api.VerifyProviderResponse{
			Valid:       true,
			ModelsCount: 1,
		}, nil
	}

	modelsCount := 1
	if foundPreset != nil && len(foundPreset.Spec.Models) > 0 {
		modelsCount = len(foundPreset.Spec.Models)
	}

	if foundPreset != nil && foundPreset.Spec.Auth != nil {
		hasKey, _, _ := resolvePresetAuth(foundPreset)
		if foundPreset.Spec.Auth.Type == warp.AuthTypeOAuth2 {
			if !hasKey {
				return &api.VerifyProviderResponse{
					Valid:        false,
					ErrorMessage: fmt.Sprintf("%s requires OAuth authentication", provName),
				}, nil
			}
		} else if apiKey == "" && !hasKey && foundPreset.Spec.Auth.Type != "none" {
			return &api.VerifyProviderResponse{
				Valid:        false,
				ErrorMessage: fmt.Sprintf("API key or token is required for %s", provName),
			}, nil
		}
	}

	return &api.VerifyProviderResponse{
		Valid:       true,
		ModelsCount: modelsCount,
	}, nil
}

func (d *Daemon) StartProviderAuth(ctx context.Context, provider string) (*api.ProviderAuthResponse, error) {
	prov := strings.TrimSpace(strings.ToLower(provider))
	driver, ok := gateway.GetOAuthDriver(prov)
	if !ok {
		return nil, providerAuthUnsupported(provider)
	}
	return d.beginProviderAuthFlow(prov, driver)
}

// providerAuthUnsupported is the 400 clients see for a provider without a
// registered OAuth driver, matching the documented bad_request contract.
func providerAuthUnsupported(provider string) error {
	return &api.APIError{
		StatusCode: http.StatusBadRequest,
		Code:       "bad_request",
		Message:    fmt.Sprintf("oauth not supported for provider %q", provider),
	}
}

// providerAuthBadRequest wraps a client-facing auth validation failure in the
// documented bad_request envelope.
func providerAuthBadRequest(format string, args ...any) error {
	return &api.APIError{
		StatusCode: http.StatusBadRequest,
		Code:       "bad_request",
		Message:    fmt.Sprintf(format, args...),
	}
}

// beginProviderAuthFlow opens a PKCE flow for driver and holds it open on a
// background goroutine until the loopback callback lands or the flow times out.
func (d *Daemon) beginProviderAuthFlow(prov string, driver gateway.OAuthDriver) (*api.ProviderAuthResponse, error) {
	d.providerAuthMu.Lock()
	if existing, ok := d.providerAuths[prov]; ok && existing != nil {
		existing.mu.Lock()
		if existing.cancel != nil {
			existing.cancel()
		}
		existing.mu.Unlock()
	}

	flowCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	flow := &providerAuthFlow{
		provider: prov,
		status:   "authorizing",
		cancel:   cancel,
		settled:  make(chan struct{}),
	}
	d.providerAuths[prov] = flow
	d.providerAuthMu.Unlock()

	oauthCfg := driver.OAuthConfig()
	session, err := auth.BeginAuth(oauthCfg)
	if err != nil {
		flow.settle("error", "", err.Error())
		close(flow.settled)
		return nil, fmt.Errorf("begin oauth: %w", err)
	}

	flow.mu.Lock()
	flow.authURL = session.AuthURL
	flow.session = session
	flow.mu.Unlock()

	go func() {
		defer cancel()
		defer close(flow.settled)

		res, err := auth.ReceiveCallback(flowCtx, oauthCfg.RedirectURL, session.AuthURL, auth.LoopbackOptions{
			Template: oauthCallbackTemplate,
		})
		if err != nil {
			d.failProviderAuthFlow(flow, err)
			return
		}
		if !flow.claim() {
			return
		}
		d.finishProviderAuth(flowCtx, flow, driver, res.Code, res.State)
	}()

	resp := flow.toAPI()
	return &resp, nil
}

// failProviderAuthFlow records a terminal error on the flow. A flow that
// already settled keeps its outcome, and a flow whose completion has been
// claimed (a pasted callback cancels the loopback listener while it exchanges
// the code, and that cancellation surfaces here as an error) belongs to its
// completer, not to the listener.
func (d *Daemon) failProviderAuthFlow(flow *providerAuthFlow, err error) {
	flow.mu.Lock()
	defer flow.mu.Unlock()
	if flow.status != "authorizing" || flow.claimed {
		return
	}
	flow.status = "error"
	flow.err = err.Error()
}

// finishProviderAuth exchanges the authorization code, persists the token, and
// reloads workspaces so pooled gateways pick up the new credentials. It is the
// shared completion path for both the loopback listener and a manually pasted
// callback URL, and only runs for the goroutine that claimed the flow, so it
// owns the recorded outcome outright.
func (d *Daemon) finishProviderAuth(ctx context.Context, flow *providerAuthFlow, driver gateway.OAuthDriver, code, state string) {
	tok, err := auth.CompleteAuth(ctx, driver.OAuthConfig(), flow.session, code, state)
	if err != nil {
		flow.settle("error", "", err.Error())
		return
	}

	account := driver.ExtractAccount(tok)
	if err := gateway.SaveProviderToken(flow.provider, tok, account); err != nil {
		flow.settle("error", "", err.Error())
		return
	}

	d.reloadAllWorkspaces(ctx)

	flow.settle("authenticated", account, "")
}

// CompleteProviderAuth finishes an in-flight provider OAuth flow with a
// callback URL the user copied from their browser, which is the fallback when
// the browser runs on a different machine (SSH or headless hosts) and cannot
// reach the daemon's loopback listener.
func (d *Daemon) CompleteProviderAuth(ctx context.Context, provider string, req api.ProviderAuthCallbackRequest) (*api.ProviderAuthResponse, error) {
	prov := strings.TrimSpace(strings.ToLower(provider))
	driver, ok := gateway.GetOAuthDriver(prov)
	if !ok {
		return nil, providerAuthUnsupported(provider)
	}

	d.providerAuthMu.Lock()
	flow := d.providerAuths[prov]
	d.providerAuthMu.Unlock()

	if flow == nil {
		return nil, providerAuthBadRequest("no provider auth flow in progress for %q; start it with auth login", prov)
	}

	code, state, err := parseProviderCallback(req.CallbackURL)
	if err != nil {
		return nil, err
	}

	// Claiming under the flow lock makes this paste the single completer:
	// the loopback listener can no longer exchange the same one-time code,
	// and a duplicate paste just reads the current status.
	if !flow.claim() {
		return d.GetProviderAuthStatus(ctx, prov)
	}
	flow.mu.Lock()
	cancel := flow.cancel
	flow.mu.Unlock()
	cancel()

	d.finishProviderAuth(ctx, flow, driver, code, state)

	resp := flow.toAPI()
	if resp.Status == "error" {
		return nil, fmt.Errorf("complete oauth: %s", resp.Error)
	}

	return &resp, nil
}

// parseProviderCallback extracts the authorization code and state from a
// callback URL the user pasted by hand. It accepts a full callback URL, a
// query string, or a bare code.
func parseProviderCallback(raw string) (code, state string, err error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", "", providerAuthBadRequest("callback url is required")
	}
	if strings.Contains(value, "://") && !strings.Contains(value, "?") && !strings.Contains(value, "#") {
		return "", "", providerAuthBadRequest("callback url carries no authorization code")
	}

	query := value
	if idx := strings.IndexAny(value, "?#"); idx >= 0 {
		query = value[idx+1:]
	}
	if hash := strings.LastIndex(query, "#"); hash >= 0 {
		query = query[:hash]
	}

	params, parseErr := url.ParseQuery(query)
	if parseErr == nil && (params.Get("code") != "" || params.Get("error") != "" || strings.Contains(value, "=")) {
		code, state = params.Get("code"), params.Get("state")
		if oauthErr := params.Get("error"); code == "" && oauthErr != "" {
			return "", "", providerAuthBadRequest("authorization failed: %s", oauthErr)
		}
		if code == "" {
			return "", "", providerAuthBadRequest("callback url carries no authorization code")
		}
		return code, state, nil
	}

	// A bare code pasted without any query decoration.
	if strings.Contains(value, "://") || strings.Contains(value, "=") {
		return "", "", providerAuthBadRequest("callback url carries no authorization code")
	}
	return value, "", nil
}

func (d *Daemon) GetProviderAuthStatus(ctx context.Context, provider string) (*api.ProviderAuthResponse, error) {
	prov := strings.TrimSpace(strings.ToLower(provider))
	if !gateway.HasOAuthDriver(prov) {
		return nil, providerAuthUnsupported(provider)
	}

	d.providerAuthMu.Lock()
	flow, ok := d.providerAuths[prov]
	d.providerAuthMu.Unlock()

	if ok && flow != nil {
		resp := flow.toAPI()
		if resp.Status == "authorizing" || resp.Status == "error" || (resp.Status == "authenticated" && resp.Error == "") {
			return &resp, nil
		}
	}

	if tok, email, err := gateway.LoadProviderToken(prov); err == nil && tok != nil {
		return &api.ProviderAuthResponse{
			Provider: prov,
			Status:   "authenticated",
			Account:  email,
		}, nil
	}

	return &api.ProviderAuthResponse{
		Provider: prov,
		Status:   "idle",
	}, nil
}

// LogoutProviderAuth cancels any in-flight OAuth flow for provider and removes
// its persisted credentials, then reloads workspaces so pooled gateways drop
// the signed-in client. It reports whether credential material was deleted.
func (d *Daemon) LogoutProviderAuth(ctx context.Context, provider string) (*api.ProviderAuthResponse, error) {
	prov := strings.TrimSpace(strings.ToLower(provider))
	if prov == "" {
		return nil, providerAuthBadRequest("provider name is required")
	}

	d.providerAuthMu.Lock()
	flow := d.providerAuths[prov]
	if flow != nil {
		delete(d.providerAuths, prov)
	}
	d.providerAuthMu.Unlock()

	if flow != nil {
		flow.mu.Lock()
		cancel, settled := flow.cancel, flow.settled
		flow.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		// Wait briefly so a flow completing concurrently cannot re-save its
		// token after the credential file is removed.
		if settled != nil {
			select {
			case <-settled:
			case <-time.After(5 * time.Second):
			}
		}
	}

	deleted, err := gateway.DeleteProviderToken(prov, "")
	if err != nil {
		return nil, err
	}

	d.reloadAllWorkspaces(ctx)

	status := "idle"
	if deleted {
		status = "signed-out"
	}
	return &api.ProviderAuthResponse{Provider: prov, Status: status}, nil
}

// ListProviders returns the built-in provider presets with their current
// credential availability, independent of any workspace.
func (d *Daemon) ListProviders(ctx context.Context) ([]api.ProviderSummary, error) {
	return d.scanGlobalProviders(), nil
}

func (d *Daemon) scanGlobalProviders() []api.ProviderSummary {
	userEnv := loadUserEnv()
	presets := workspace.Presets()
	summaries := make([]api.ProviderSummary, 0, len(presets))

	for _, p := range presets {
		authEnv := workspace.ProviderAuthPrimaryEnv(p.Spec.Auth)

		authType := "bearer"
		if p.Spec.Auth != nil && p.Spec.Auth.Type != "" {
			authType = string(p.Spec.Auth.Type)
		}

		hasKey, account, status := resolvePresetAuth(p, userEnv)
		if p.Metadata.Name == "ollama" {
			status = "local"
		}

		var modelNames []string
		for _, m := range p.Spec.Models {
			modelNames = append(modelNames, m.ID)
		}

		defaultModel := ""
		if p.Spec.DefaultModels != nil {
			defaultModel = p.Spec.DefaultModels.Completion
		}

		summaries = append(summaries, api.ProviderSummary{
			Name:         p.Metadata.Name,
			DisplayName:  p.Metadata.Name,
			Status:       status,
			HasKey:       hasKey,
			AuthType:     authType,
			AuthEnv:      authEnv,
			Account:      account,
			DefaultModel: defaultModel,
			Models:       modelNames,
			Scope:        "system",
		})
	}

	return summaries
}

func (d *Daemon) checkMissingCredentials(cleanDir string) []string {
	var missing []string
	localEnv, _ := env.LoadFile(filepath.Join(cleanDir, ".env"))
	userEnv := loadUserEnv()

	wsFile := filepath.Join(cleanDir, warp.WorkspaceFileName)
	data, err := os.ReadFile(wsFile)
	if err != nil {
		return missing
	}
	res, err := warp.Parse(wsFile, string(data))
	if err != nil {
		return missing
	}
	wsDef, ok := res.Resource.(*warp.WorkspaceDef)
	if !ok || wsDef.Spec.DefaultProvider == "" {
		return missing
	}
	p := findPreset(wsDef.Spec.DefaultProvider)
	if p == nil {
		return missing
	}
	if hasKey, _, _ := resolvePresetAuth(p, localEnv, userEnv); !hasKey {
		if envVar := workspace.ProviderAuthPrimaryEnv(p.Spec.Auth); envVar != "" {
			missing = append(missing, envVar)
		}
	}

	return missing
}
