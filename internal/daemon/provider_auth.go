package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/env"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	providerauth "github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

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

func (d *Daemon) resolvePresetAuth(p *warp.ModelProvider, envLayers ...map[string]string) (hasKey bool, account, status string) {
	if p == nil {
		return false, "", ""
	}
	if p.Spec.Auth != nil && p.Spec.Auth.Type == warp.AuthTypeOAuth2 {
		provType := cmp.Or(p.Spec.Type, p.Metadata.Name)
		if email, ok := d.auth.Account(provType, p.Metadata.Name); ok {
			if _, expired := d.auth.OAuthHealth(provType, p.Metadata.Name); expired {
				if email != "" {
					return false, email, fmt.Sprintf("⚠ %s (session expired)", email)
				}
				return false, "", "⚠ session expired"
			}
			if email != "" {
				return true, email, fmt.Sprintf("✓ %s (OAuth)", email)
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
			if h, failed := d.auth.Health(p.Metadata.Name, providerauth.SecretRevision(val)); failed && h.State == providerauth.HealthInvalid {
				return false, "", fmt.Sprintf("⚠ %s rejected (invalid key)", authEnv)
			}
			return true, "", fmt.Sprintf("✓ %s detected", authEnv)
		}
	}
	return false, "", ""
}

func flowSnapshotToAPI(snap *providerauth.FlowSnapshot) *api.ProviderAuthResponse {
	if snap == nil {
		return nil
	}
	return &api.ProviderAuthResponse{
		Provider: snap.Provider,
		AuthURL:  snap.AuthURL,
		Status:   snap.Status,
		Account:  snap.Account,
		Error:    snap.Error,
	}
}

// providerAuthAPIError maps auth.Manager sentinel errors onto HTTP 400
// bad_request APIError envelopes so clients distinguish validation failures
// from internal errors without parsing message text.
func providerAuthAPIError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, providerauth.ErrUnsupportedProvider),
		errors.Is(err, providerauth.ErrInvalidCallback),
		errors.Is(err, providerauth.ErrNoFlowInProgress),
		errors.Is(err, providerauth.ErrBrowserHostOnly),
		errors.Is(err, providerauth.ErrInvalidProviderName):
		return &api.APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "bad_request",
			Message:    err.Error(),
		}
	default:
		return err
	}
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
		u, err := url.Parse(endpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
			return &api.VerifyProviderResponse{ErrorMessage: "invalid Ollama endpoint"}, nil
		}
		u.RawQuery, u.Fragment, u.ForceQuery = "", "", false
		probe, err := http.NewRequestWithContext(ctx, http.MethodGet, u.JoinPath("/api/tags").String(), nil)
		if err != nil {
			return &api.VerifyProviderResponse{ErrorMessage: "invalid Ollama request"}, nil
		}

		client := &http.Client{
			Timeout:       3 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
		resp, err := client.Do(probe)
		if err != nil {
			return &api.VerifyProviderResponse{
				Valid:        false,
				ErrorMessage: fmt.Sprintf("failed to reach Ollama at %s: %v", endpoint, err),
			}, nil
		}
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return &api.VerifyProviderResponse{ErrorMessage: "Ollama probe returned " + resp.Status}, nil
		}
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
		hasKey, _, _ := d.resolvePresetAuth(foundPreset)
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
	snap, err := d.auth.Start(ctx, provider)
	if err != nil {
		return nil, providerAuthAPIError(err)
	}
	return flowSnapshotToAPI(snap), nil
}

// CompleteProviderAuth finishes an in-flight provider OAuth flow with a
// callback URL the user copied from their browser, which is the fallback when
// the browser runs on a different machine (SSH or headless hosts) and cannot
// reach the daemon's loopback listener.
func (d *Daemon) CompleteProviderAuth(ctx context.Context, provider string, req api.ProviderAuthCallbackRequest) (*api.ProviderAuthResponse, error) {
	snap, err := d.auth.Complete(ctx, provider, req.CallbackURL)
	if err != nil {
		return nil, providerAuthAPIError(err)
	}
	return flowSnapshotToAPI(snap), nil
}

func (d *Daemon) GetProviderAuthStatus(_ context.Context, provider string) (*api.ProviderAuthResponse, error) {
	snap, err := d.auth.Status(provider)
	if err != nil {
		return nil, providerAuthAPIError(err)
	}
	return flowSnapshotToAPI(snap), nil
}

// LogoutProviderAuth cancels any in-flight OAuth flow for provider and removes
// its persisted credentials, then reloads workspaces so pooled gateways drop
// the signed-in client. It reports whether credential material was deleted.
func (d *Daemon) LogoutProviderAuth(ctx context.Context, provider string) (*api.ProviderAuthResponse, error) {
	prov := strings.TrimSpace(strings.ToLower(provider))
	deleted, err := d.auth.Logout(prov, prov)
	if err != nil {
		return nil, providerAuthAPIError(err)
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
func (d *Daemon) ListProviders(_ context.Context) ([]api.ProviderSummary, error) {
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
		loginLabel := d.auth.LoginLabel(cmp.Or(p.Spec.Type, p.Metadata.Name))

		hasKey, account, status := d.resolvePresetAuth(p, userEnv)
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
			Name:             p.Metadata.Name,
			DisplayName:      p.Metadata.Name,
			Status:           status,
			HasKey:           hasKey,
			AuthType:         authType,
			AuthEnv:          authEnv,
			BrowserAuthLabel: loginLabel,
			Account:          account,
			DefaultModel:     defaultModel,
			Models:           modelNames,
			Scope:            "system",
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
	if hasKey, _, _ := d.resolvePresetAuth(p, localEnv, userEnv); !hasKey {
		if envVar := workspace.ProviderAuthPrimaryEnv(p.Spec.Auth); envVar != "" {
			missing = append(missing, envVar)
		}
	}

	return missing
}
