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
	"slices"
	"strings"
	"time"

	"github.com/masterkeysrd/loom/profile"
	loomollama "github.com/masterkeysrd/loom/providers/ollama"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/env"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
	ollamaapi "github.com/ollama/ollama/api"
)

// providerStatusFor resolves the browser-facing status of one configured
// provider: a disabled manifest is always "disabled", a local endpoint needs no
// credential, an OAuth provider reports "missing_auth" until a token is stored,
// and everything else reports whether its configured secret resolves.
func providerStatusFor(rec workspace.ProviderRecord, wsEnv map[string]string) (status string, hasKey bool, account string) {
	if !rec.Enabled {
		return "disabled", false, ""
	}
	if isLocalEndpoint(rec.Def) || rec.Name == "ollama" {
		return "local", true, ""
	}
	auth := rec.Def.Spec.Auth
	if auth != nil && auth.Type == warp.AuthTypeOAuth2 {
		if tok, email, err := gateway.LoadProviderToken(rec.Name); err == nil && tok != nil {
			return "active", true, email
		}
		return "missing_auth", false, ""
	}
	if auth == nil || auth.Type == warp.AuthTypeNone || (auth.Token == "" && auth.Password == "") {
		return "active", true, ""
	}
	hasKey = providerSecretAvailable(auth, wsEnv)
	if !hasKey {
		return "missing_auth", false, ""
	}
	return "active", true, ""
}

func isLocalEndpoint(def *warp.ModelProvider) bool {
	if def == nil {
		return false
	}
	endpoint := strings.ToLower(strings.TrimSpace(def.Spec.Endpoint))
	return strings.HasPrefix(endpoint, "http://localhost") ||
		strings.HasPrefix(endpoint, "http://127.0.0.1") ||
		strings.HasPrefix(endpoint, "http://[::1]")
}

// providerSecretAvailable reports whether the manifest's auth secret resolves
// from the workspace environment (host env plus the user and project .env
// layers the daemon already loaded), or a ${file:...} secret exists on disk.
func providerSecretAvailable(auth *warp.ProviderAuth, wsEnv map[string]string) bool {
	if auth == nil {
		return false
	}
	for _, name := range workspace.ProviderAuthEnvNames(auth) {
		if strings.TrimSpace(wsEnv[name]) != "" || strings.TrimSpace(os.Getenv(name)) != "" {
			return true
		}
	}
	if expr, ok := warp.ParseExpression(strings.TrimSpace(auth.Token)); ok && expr.Scheme == "file" {
		if _, err := os.Stat(os.ExpandEnv(expr.Target)); err == nil {
			return true
		}
	}
	return false
}

func providerModelInfos(def *warp.ModelProvider) []api.ProviderModelInfo {
	if def == nil || len(def.Spec.Models) == 0 {
		return nil
	}
	out := make([]api.ProviderModelInfo, 0, len(def.Spec.Models))
	for _, m := range def.Spec.Models {
		out = append(out, api.ProviderModelInfo{
			ID:      m.ID,
			Name:    m.Name,
			Label:   m.Label,
			Aliases: slices.Clone(m.Aliases),
			Context: m.Limits.Context,
			Output:  m.Limits.Output,
		})
	}
	return out
}

func providerDefaultModel(def *warp.ModelProvider) string {
	if def == nil {
		return ""
	}
	return def.Spec.DefaultModels.ForRole(warp.ModelRoleCompletion)
}

// ListWorkspaceProviders returns every ModelProvider the workspace resolved
// across tiers — including disabled and shadowed ones the effective listing
// hides — with the manifest path, scope badge, and status the /providers
// browser renders.
func (d *Daemon) ListWorkspaceProviders(ctx context.Context, rawWS string) ([]api.WorkspaceProvider, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}
	ws := rt.Workspace()
	wsEnv := ws.Env()

	records := ws.ProviderRecords()
	out := make([]api.WorkspaceProvider, 0, len(records))
	for _, rec := range records {
		status, hasKey, account := providerStatusFor(rec, wsEnv)
		authEnv := workspace.ProviderAuthPrimaryEnv(rec.Def.Spec.Auth)
		authType := ""
		if rec.Def.Spec.Auth != nil {
			authType = rec.Def.Spec.Auth.Type
		}
		out = append(out, api.WorkspaceProvider{
			Name:         rec.Name,
			DisplayName:  cmp.Or(rec.Def.Metadata.DisplayName, rec.Name),
			Type:         rec.Def.Spec.Type,
			Endpoint:     rec.Def.Spec.Endpoint,
			Scope:        strings.ToLower(rec.Namespace),
			Path:         ws.PortablePath(rec.Path),
			Enabled:      rec.Enabled,
			Shadowed:     rec.Shadowed,
			Status:       status,
			HasKey:       hasKey,
			AuthType:     authType,
			AuthEnv:      authEnv,
			Account:      account,
			DefaultModel: providerDefaultModel(rec.Def),
			Models:       providerModelInfos(rec.Def),
			Writable:     rec.Writable,
			Overridable:  rec.Overridable,
		})
	}
	return out, nil
}

// providerAPIError maps the workspace patch layer's sentinel errors onto HTTP
// semantics so clients can distinguish "no such provider" from "read-only
// manifest" instead of parsing message text.
func providerAPIError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, workspace.ErrProviderNotFound):
		return &api.APIError{StatusCode: http.StatusNotFound, Code: "not_found", Message: err.Error()}
	case errors.Is(err, workspace.ErrProviderReadOnly):
		return &api.APIError{StatusCode: http.StatusConflict, Code: "manifest_read_only", Message: err.Error()}
	default:
		return err
	}
}

// SetProviderEnabled patches spec.enabled on the named provider's manifest and
// hot-reloads the workspace so the gateway and model catalog agree immediately.
func (d *Daemon) SetProviderEnabled(ctx context.Context, rawWS, name string, enabled bool) ([]api.WorkspaceProvider, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		return nil, &api.APIError{StatusCode: http.StatusBadRequest, Code: "bad_request", Message: "provider name is required"}
	}
	if err := providerAPIError(rt.Workspace().SetProviderEnabled(name, enabled)); err != nil {
		return nil, err
	}
	if err := rt.Reload(ctx); err != nil {
		return nil, fmt.Errorf("daemon: reload workspace after provider update: %w", err)
	}
	return d.ListWorkspaceProviders(ctx, rawWS)
}

// UpdateProviderModels replaces spec.models on the manifest owning the named
// provider, then hot-reloads the workspace.
func (d *Daemon) UpdateProviderModels(ctx context.Context, rawWS, name string, req api.UpdateProviderModelsRequest) ([]api.WorkspaceProvider, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		return nil, &api.APIError{StatusCode: http.StatusBadRequest, Code: "bad_request", Message: "provider name is required"}
	}
	edits := make([]workspace.ProviderModelEdit, 0, len(req.Models))
	for _, m := range req.Models {
		edits = append(edits, workspace.ProviderModelEdit{
			ID:      m.ID,
			Name:    m.Name,
			Label:   m.Label,
			Aliases: m.Aliases,
			Context: m.Context,
			Output:  m.Output,
		})
	}
	if err := providerAPIError(rt.Workspace().UpdateProviderModels(name, edits, req.DefaultModel, req.DefaultSet)); err != nil {
		return nil, err
	}
	if err := rt.Reload(ctx); err != nil {
		return nil, fmt.Errorf("daemon: reload workspace after provider update: %w", err)
	}
	return d.ListWorkspaceProviders(ctx, rawWS)
}

// AssignModelAliases saves the exact alias set one model should own, stripping
// each alias from other models across writable manifests first (the atomic
// move), then hot-reloads the workspace and returns the refreshed alias report.
func (d *Daemon) AssignModelAliases(ctx context.Context, rawWS string, req api.AssignModelAliasesRequest) (*api.AssignModelAliasesResponse, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Provider) == "" || strings.TrimSpace(req.Model) == "" {
		return nil, &api.APIError{StatusCode: http.StatusBadRequest, Code: "bad_request", Message: "provider and model are required"}
	}
	if err := providerAPIError(rt.Workspace().AssignModelAliases(req.Provider, req.Model, req.Aliases, req.DefaultHere)); err != nil {
		return nil, err
	}
	if err := rt.Reload(ctx); err != nil {
		return nil, fmt.Errorf("daemon: reload workspace after alias update: %w", err)
	}

	providers, err := d.ListWorkspaceProviders(ctx, rawWS)
	if err != nil {
		return nil, err
	}
	return &api.AssignModelAliasesResponse{
		Providers: providers,
		Aliases:   aliasAssignmentsAPI(rt.Workspace()),
	}, nil
}

func aliasAssignmentsAPI(ws *workspace.Workspace) []api.AliasAssignment {
	records := ws.AliasAssignments()
	out := make([]api.AliasAssignment, 0, len(records))
	for _, rec := range records {
		item := api.AliasAssignment{Alias: rec.Alias, WorkspaceDefault: rec.Workspace}
		if len(rec.Owners) > 0 {
			item.Provider = rec.Owners[0].Provider
			item.Model = rec.Owners[0].Model
		}
		if len(rec.Duplicates) > 0 {
			item.Duplicate = true
			for _, dup := range rec.Duplicates {
				item.Duplicates = append(item.Duplicates, dup.Provider+"/"+dup.Model)
			}
		}
		out = append(out, item)
	}
	return out
}

// ListAliasAssignments returns the workspace-wide bare-alias report used by the
// shared model alias editor.
func (d *Daemon) ListAliasAssignments(ctx context.Context, rawWS string) ([]api.AliasAssignment, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}
	return aliasAssignmentsAPI(rt.Workspace()), nil
}

// providerEnvPath resolves the .env file a scope's secrets land in: the user
// config file for the user scope, the workspace root .env otherwise.
func providerEnvPath(scope, rootDir string) (string, error) {
	if strings.EqualFold(strings.TrimSpace(scope), "user") {
		return xdg.SubConfigDir(".env")
	}
	return filepath.Join(rootDir, ".env"), nil
}

// CreateProvider authors a new ModelProvider manifest in the user or workspace
// scope. A supplied API key is written exclusively to the scope's .env file and
// referenced from the manifest through a ${env:...} expression, so no secret is
// ever stored in YAML.
func (d *Daemon) CreateProvider(ctx context.Context, rawWS string, req api.CreateProviderRequest) (*api.WorkspaceProvider, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}
	ws := rt.Workspace()

	name := strings.TrimSpace(req.Name)
	provType := strings.TrimSpace(req.Type)
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	if scope == "" {
		scope = "workspace"
	}
	if scope != "user" && scope != "workspace" {
		return nil, &api.APIError{StatusCode: http.StatusBadRequest, Code: "bad_request", Message: `scope must be "user" or "workspace"`}
	}
	if name == "" {
		return nil, &api.APIError{StatusCode: http.StatusBadRequest, Code: "bad_request", Message: "provider name is required"}
	}
	if !isValidProviderName(name) {
		return nil, &api.APIError{StatusCode: http.StatusBadRequest, Code: "bad_request", Message: fmt.Sprintf("provider name %q must be a lowercase slug (letters, digits, '-' or '_')", name)}
	}
	if provType == "" {
		if preset := findPreset(name); preset != nil {
			provType = preset.Spec.Type
		}
	}
	if provType == "" {
		return nil, &api.APIError{StatusCode: http.StatusBadRequest, Code: "bad_request", Message: "provider type is required"}
	}
	if _, exists := ws.ProviderRecord(name); exists {
		return nil, &api.APIError{StatusCode: http.StatusConflict, Code: "already_exists", Message: fmt.Sprintf("provider %q is already configured for this workspace", name)}
	}

	spec := warp.ModelProviderSpec{Type: provType, Endpoint: strings.TrimSpace(req.Endpoint)}
	preset := findPreset(name)
	if preset == nil {
		preset = findPreset(provType)
	}
	// Only a preset-backed creation (name matches the preset) inherits the
	// preset manifest. A custom provider borrows just the endpoint, so it can
	// never silently adopt the preset's auth (which would leak an existing
	// secret toward a custom endpoint), config, defaults, or model list.
	isPreset := preset != nil && preset.Metadata.Name == name
	if isPreset {
		spec = *preset.Spec.DeepCopy()
		spec.Enabled = nil
		if endpoint := strings.TrimSpace(req.Endpoint); endpoint != "" {
			spec.Endpoint = endpoint
		}
	} else if preset != nil && spec.Endpoint == "" {
		spec.Endpoint = strings.TrimSpace(preset.Spec.Endpoint)
	}

	if len(req.Models) > 0 {
		spec.Models = nil
		for _, m := range req.Models {
			pm := warp.ProviderModel{
				ID:      strings.TrimSpace(m.ID),
				Name:    cmp.Or(strings.TrimSpace(m.Name), strings.TrimSpace(m.ID)),
				Label:   strings.TrimSpace(m.Label),
				Aliases: slices.Clone(m.Aliases),
			}
			if m.Context > 0 || m.Output > 0 {
				pm.Limits = warp.ProviderModelLimits{Context: m.Context, Output: m.Output}
			}
			spec.Models = append(spec.Models, pm)
		}
	}
	if dm := strings.TrimSpace(req.DefaultModel); dm != "" {
		if spec.DefaultModels == nil {
			spec.DefaultModels = &warp.ModelDefaults{}
		}
		spec.DefaultModels.Completion = dm
	}
	// Replacing the preset's model list invalidates any preset default that no
	// longer resolves, so the manifest never ships a dangling reference.
	if len(req.Models) > 0 && spec.DefaultModels != nil {
		for _, role := range []warp.ModelRole{warp.ModelRoleCompletion, warp.ModelRoleDecision, warp.ModelRoleEmbedding} {
			ref := spec.DefaultModels.ForRole(role)
			if ref == "" || modelRefDeclared(spec.Models, ref) {
				continue
			}
			switch role {
			case warp.ModelRoleCompletion:
				spec.DefaultModels.Completion = ""
			case warp.ModelRoleDecision:
				spec.DefaultModels.Decision = ""
			case warp.ModelRoleEmbedding:
				spec.DefaultModels.Embedding = ""
			}
		}
		if *spec.DefaultModels == (warp.ModelDefaults{}) {
			spec.DefaultModels = nil
		}
	}

	authEnv := strings.TrimSpace(req.AuthEnv)
	apiKey := strings.TrimSpace(req.APIKey)
	authType := strings.TrimSpace(req.AuthType)
	if authType == warp.AuthTypeNone && apiKey == "" && authEnv == "" {
		spec.Auth = &warp.ProviderAuth{Type: warp.AuthTypeNone}
	} else if apiKey != "" || authEnv != "" {
		if authEnv == "" {
			// A preset-backed provider keeps its preset secret variable (e.g.
			// GOOGLE_API_KEY); a custom one derives <NAME>_API_KEY.
			if isPreset {
				authEnv = workspace.ProviderAuthPrimaryEnv(spec.Auth)
			}
			if authEnv == "" {
				authEnv = strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_API_KEY"
			}
		}
		if authType == "" {
			authType = warp.AuthTypeBearer
			if spec.Auth != nil && spec.Auth.Type != "" {
				authType = spec.Auth.Type
			}
		}
		auth := &warp.ProviderAuth{Type: authType, Token: fmt.Sprintf("${env:%s}", authEnv)}
		// The preset's header (e.g. X-Api-Key) only pairs with its own scheme.
		if spec.Auth != nil && spec.Auth.Header != "" && spec.Auth.Type == authType {
			auth.Header = spec.Auth.Header
		}
		spec.Auth = auth
		if apiKey != "" {
			envPath, envErr := providerEnvPath(scope, ws.RootDir())
			if envErr != nil {
				return nil, fmt.Errorf("daemon: resolve provider env path: %w", envErr)
			}
			if err := env.SetInFile(envPath, authEnv, apiKey); err != nil {
				return nil, fmt.Errorf("daemon: write provider secret: %w", err)
			}
			if scope != "user" {
				if err := workspace.EnsureGitignoreEntry(ws.RootDir(), ".env"); err != nil {
					return nil, fmt.Errorf("daemon: keep .env out of git: %w", err)
				}
			}
		}
	}

	dir := filepath.Join(ws.RootDir(), warp.DefaultAgentsDir, "providers")
	if scope == "user" {
		userDir, dirErr := xdg.SubConfigDir("agents", "providers")
		if dirErr != nil {
			return nil, fmt.Errorf("daemon: resolve user providers dir: %w", dirErr)
		}
		dir = userDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("daemon: create providers directory: %w", err)
	}

	manifest := &warp.ModelProvider{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindModelProvider,
			Metadata:   warp.Metadata{Name: name},
		},
		Spec: spec,
	}
	data, err := warp.Format(manifest)
	if err != nil {
		return nil, fmt.Errorf("daemon: format provider manifest: %w", err)
	}
	if _, err := warp.Parse(name+".provider.yaml", string(data)); err != nil {
		return nil, fmt.Errorf("daemon: new provider manifest is invalid: %w", err)
	}
	path := filepath.Join(dir, name+".provider.yaml")
	if _, err := os.Stat(path); err == nil {
		return nil, &api.APIError{
			StatusCode: http.StatusConflict,
			Code:       "already_exists",
			Message:    "provider manifest already exists: " + fsutils.PortablePath(path, ws.RootDir(), os.Getenv("HOME")),
		}
	}
	if err := fsutils.WriteFileAtomic(path, data, 0o644); err != nil {
		return nil, fmt.Errorf("daemon: write provider manifest: %w", err)
	}

	if err := rt.Reload(ctx); err != nil {
		return nil, fmt.Errorf("daemon: reload workspace after provider create: %w", err)
	}

	providers, err := d.ListWorkspaceProviders(ctx, rawWS)
	if err != nil {
		return nil, err
	}
	for _, p := range providers {
		if p.Name == name {
			return &p, nil
		}
	}
	return nil, &api.APIError{StatusCode: http.StatusInternalServerError, Code: "error", Message: fmt.Sprintf("provider %q was written but is not visible after reload", name)}
}

// modelRefDeclared reports whether a defaultModels reference (raw id, model
// name, or "@alias") resolves against the given manifest model list.
func modelRefDeclared(models []warp.ProviderModel, ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	if alias, isAlias := strings.CutPrefix(ref, "@"); isAlias {
		alias = strings.TrimSpace(alias)
		for _, m := range models {
			for _, a := range m.Aliases {
				if strings.EqualFold(strings.TrimPrefix(a, "@"), alias) {
					return true
				}
			}
		}
		return false
	}
	for _, m := range models {
		if m.ID == ref || (m.Name != "" && m.Name == ref) {
			return true
		}
	}
	return false
}

func isValidProviderName(name string) bool {
	if name == "" || name != strings.ToLower(name) {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// ListProviderCatalog returns the models a provider type offers, so the
// /providers model manager can offer a curated list (context window, pricing,
// capabilities) instead of asking for raw IDs. Types with a static loom catalog
// answer from it; ollama answers from its live endpoint when reachable; other
// types fall back to the built-in preset's model list. Entries already declared
// in the manifest are flagged Listed.
func (d *Daemon) ListProviderCatalog(ctx context.Context, rawWS, name string) ([]api.ProviderCatalogEntry, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	rec, ok := rt.Workspace().ProviderRecord(name)
	if !ok {
		return nil, &api.APIError{StatusCode: http.StatusNotFound, Code: "not_found", Message: fmt.Sprintf("provider %q not found", name)}
	}

	var profiles []profile.Profile
	static := false
	if list, hasStatic := gateway.TypeCatalog(rec.Def.Spec.Type); hasStatic {
		profiles, static = list, true
	}
	if !static && rec.Def.Spec.Type == "ollama" {
		profiles = ollamaCatalogProfiles(ctx, rec.Def.Spec.Endpoint)
		static = len(profiles) > 0
	}
	if !static {
		if preset := findPreset(cmp.Or(rec.Def.Spec.Type, name)); preset != nil {
			for _, m := range preset.Spec.Models {
				profiles = append(profiles, profile.Profile{
					ID:     m.ID,
					Name:   cmp.Or(m.Name, m.ID),
					Limits: profile.Limits{Context: m.Limits.Context, Output: m.Limits.Output},
				})
			}
		}
	}

	declared := make(map[string]bool)
	for _, m := range rec.Def.Spec.Models {
		declared[m.ID] = true
	}

	out := make([]api.ProviderCatalogEntry, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, api.ProviderCatalogEntry{
			ID:           p.ID,
			Name:         cmp.Or(p.Name, p.ID),
			Description:  p.Description,
			ContextLimit: p.Limits.Context,
			OutputLimit:  p.Limits.Output,
			Capabilities: catalogCapabilities(p),
			Pricing:      catalogPricing(p),
			Listed:       declared[p.ID],
		})
	}
	slices.SortFunc(out, func(a, b api.ProviderCatalogEntry) int {
		return cmp.Compare(a.ID, b.ID)
	})
	return out, nil
}

// ollamaCatalogProfiles queries a local Ollama endpoint for installed models.
// A dead endpoint is a normal state (server not running), so it yields no
// profiles rather than an error.
func ollamaCatalogProfiles(ctx context.Context, endpoint string) []profile.Profile {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil
	}
	client := ollamaapi.NewClient(u, &http.Client{Timeout: 3 * time.Second})
	prov := (&loomollama.Provider{}).NewProvider(client, u)
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	profiles, err := prov.ListModels(queryCtx)
	if err != nil {
		return nil
	}
	return profiles
}

func boolTrue() *bool {
	t := true
	return &t
}

func catalogCapabilities(p profile.Profile) api.ModelCapabilities {
	c := api.ModelCapabilities{}
	if p.Capabilities.Completion {
		c.Completion = boolTrue()
	}
	if p.Capabilities.Decision {
		c.Decision = boolTrue()
	}
	if p.Capabilities.Embedding {
		c.Embedding = boolTrue()
	}
	if p.Capabilities.ToolCall {
		c.Tools = boolTrue()
	}
	if p.Capabilities.Attachment {
		c.Attachment = boolTrue()
	}
	for _, m := range p.Modalities.Inputs {
		c.InputModalities = append(c.InputModalities, string(m))
	}
	for _, m := range p.Modalities.Outputs {
		c.OutputModalities = append(c.OutputModalities, string(m))
	}
	return c
}

func catalogPricing(p profile.Profile) *api.ModelPricing {
	if p.Pricing.IsZero() {
		return nil
	}
	out := &api.ModelPricing{
		Input:      p.Pricing.Input,
		Output:     p.Pricing.Output,
		CacheRead:  p.Pricing.CacheRead,
		CacheWrite: p.Pricing.CacheWrite,
		Reasoning:  p.Pricing.Reasoning,
	}
	for _, tier := range p.Pricing.TieredLimits {
		out.Tiers = append(out.Tiers, api.ModelPricingTier{
			Label:         tier.Label,
			ConditionType: string(tier.Condition.Type),
			ConditionSize: int(tier.Condition.Size),
			Input:         tier.Input,
			Output:        tier.Output,
			CacheRead:     tier.CacheRead,
			CacheWrite:    tier.CacheWrite,
		})
	}
	return out
}
