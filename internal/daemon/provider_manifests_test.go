package daemon

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/profile"
	loomopenai "github.com/masterkeysrd/loom/providers/openai"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/warp"
)

// seedWorkspaceProvider drops a manifest into the workspace tier before any
// API call so the lazily-resolved workspace runtime loads it on first use.
func seedWorkspaceProvider(t *testing.T, wsDir, name, spec string) string {
	t.Helper()
	dir := filepath.Join(wsDir, ".agents", "providers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".provider.yaml")
	body := "apiVersion: warp/v1alpha1\nkind: ModelProvider\nmetadata:\n  name: " + name + "\nspec:\n" + spec
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const daemonTestProviderSpec = `  type: openai
  endpoint: https://api.example.test/v1
  defaultModels:
    completion: mid-model
  models:
    - id: fast-model
      name: fast-model
      aliases: [fast]
    - id: mid-model
      name: mid-model
      aliases: [balanced]
`

func findWorkspaceProvider(list []api.WorkspaceProvider, name string) (api.WorkspaceProvider, bool) {
	for _, p := range list {
		if p.Name == name {
			return p, true
		}
	}
	return api.WorkspaceProvider{}, false
}

func TestWorkspaceProvidersAPIListsAndToggles(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()
	path := seedWorkspaceProvider(t, wsDir, "provapi", daemonTestProviderSpec)

	list, err := cli.ListWorkspaceProviders(ctx)
	if err != nil {
		t.Fatalf("ListWorkspaceProviders: %v", err)
	}
	prov, ok := findWorkspaceProvider(list, "provapi")
	if !ok {
		t.Fatalf("provapi missing from listing: %+v", list)
	}
	if prov.Status != "active" || !prov.Enabled || !prov.Writable {
		t.Errorf("provapi = status:%q enabled:%v writable:%v, want active/true/true", prov.Status, prov.Enabled, prov.Writable)
	}
	if len(prov.Models) != 2 || prov.DefaultModel != "mid-model" {
		t.Errorf("provapi models=%d default=%q, want 2 models and mid-model", len(prov.Models), prov.DefaultModel)
	}

	list, err = cli.SetProviderEnabled(ctx, "provapi", false)
	if err != nil {
		t.Fatalf("SetProviderEnabled(false): %v", err)
	}
	prov, _ = findWorkspaceProvider(list, "provapi")
	if prov.Enabled || prov.Status != "disabled" {
		t.Errorf("after disable: enabled:%v status:%q, want false/disabled", prov.Enabled, prov.Status)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "enabled: false") {
		t.Fatalf("manifest not patched:\n%s", data)
	}

	if _, err := cli.SetProviderEnabled(ctx, "ghostprovider", false); err == nil {
		t.Fatal("expected error for unknown provider")
	} else {
		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
			t.Fatalf("unknown provider error = %v, want 404 APIError", err)
		}
	}
}

func TestUpdateProviderModelsAPI(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()
	seedWorkspaceProvider(t, wsDir, "provapi", daemonTestProviderSpec)

	list, err := cli.UpdateProviderModels(ctx, "provapi", api.UpdateProviderModelsRequest{
		Models: []api.ProviderModelRequest{
			{ID: "only-model", Name: "only-model", Context: 4096, Output: 512},
		},
		DefaultModel: "only-model",
		DefaultSet:   true,
	})
	if err != nil {
		t.Fatalf("UpdateProviderModels: %v", err)
	}
	prov, _ := findWorkspaceProvider(list, "provapi")
	if len(prov.Models) != 1 || prov.Models[0].ID != "only-model" || prov.Models[0].Context != 4096 {
		t.Fatalf("models after update = %+v, want single only-model with 4096 ctx", prov.Models)
	}
	if prov.DefaultModel != "only-model" {
		t.Errorf("default model = %q, want only-model", prov.DefaultModel)
	}
}

func TestAddKnownProviderModelWritesCatalogMetadata(t *testing.T) {
	knownProfile, ok := (&loomopenai.Provider{}).GetProfile("gpt-4o-mini")
	if !ok {
		t.Fatal("Loom profile for gpt-4o-mini is missing")
	}
	_, cli, wsDir := setupDaemonTest(t, &fakeProvider{
		profiles: map[string]profile.Profile{knownProfile.ID: knownProfile},
	})
	path := seedWorkspaceProvider(t, wsDir, "provapi", daemonTestProviderSpec)

	_, err := cli.UpdateProviderModels(t.Context(), "provapi", api.UpdateProviderModelsRequest{
		Models: []api.ProviderModelRequest{
			{ID: "gpt-4o-mini", Name: "gpt-4o-mini", Output: 512},
			{ID: "unlisted-local-model"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := warp.Parse(path, string(data))
	if err != nil {
		t.Fatal(err)
	}
	models := parsed.Resource.(*warp.ModelProvider).Spec.Models
	if len(models) != 2 {
		t.Fatalf("expected two models, got %+v", models)
	}
	known, unknown := models[0], models[1]
	if known.ID != "gpt-4o-mini" || known.Label == "" || known.Limits.Context <= 0 || known.Limits.Output != 512 {
		t.Fatalf("expected catalog label, context, and explicit output limit, got %+v", known)
	}
	if caps := known.Capabilities; caps == nil || caps.Tools == nil || !*caps.Tools ||
		caps.Modalities == nil || len(caps.Modalities.Input) == 0 {
		t.Fatalf("expected catalog tools and input modalities, got %+v", caps)
	}
	if known.Cost == nil || known.Cost.Input <= 0 {
		t.Fatalf("expected catalog pricing, got %+v", known.Cost)
	}
	if unknown.ID != "unlisted-local-model" || unknown.Capabilities != nil || unknown.Cost != nil || unknown.Limits.Context != 0 {
		t.Fatalf("unexpected metadata on unknown model: %+v", unknown)
	}
}

func TestCreateProviderAPIWritesSecretToEnv(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()

	created, err := cli.CreateProvider(ctx, api.CreateProviderRequest{
		Scope:    "workspace",
		Name:     "newprov",
		Type:     "openai",
		Endpoint: "https://api.example.test/v1",
		APIKey:   "sk-test-secret",
		Models:   []api.ProviderModelRequest{{ID: "m1", Name: "m1"}},
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	if created.HasKey || created.Status != "missing_auth" {
		t.Fatal("new .env was loaded before reapproval")
	}
	approveTestWorkspace(t, cli)
	providers, err := cli.ListWorkspaceProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	created, _ = findWorkspaceProvider(providers, "newprov")
	if created.Name != "newprov" || created.Status != "active" || !created.HasKey {
		t.Fatalf("created = %+v, want active newprov with key", created)
	}

	manifest, err := os.ReadFile(filepath.Join(wsDir, ".agents", "providers", "newprov.provider.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), "sk-test-secret") {
		t.Fatalf("secret leaked into manifest:\n%s", manifest)
	}
	if !strings.Contains(string(manifest), "${env:NEWPROV_API_KEY}") {
		t.Fatalf("manifest lacks env reference:\n%s", manifest)
	}
	envFile, err := os.ReadFile(filepath.Join(wsDir, ".env"))
	if err != nil {
		t.Fatalf("workspace .env not written: %v", err)
	}
	if !strings.Contains(string(envFile), "NEWPROV_API_KEY=sk-test-secret") {
		t.Fatalf(".env lacks secret:\n%s", envFile)
	}
	gitignore, err := os.ReadFile(filepath.Join(wsDir, ".gitignore"))
	if err != nil || !strings.Contains(string(gitignore), ".env") {
		t.Fatalf(".gitignore should keep .env out of git: err=%v content=%q", err, gitignore)
	}

	if _, err := cli.CreateProvider(ctx, api.CreateProviderRequest{
		Scope: "workspace", Name: "newprov", Type: "openai",
	}); err == nil {
		t.Fatal("expected already_exists conflict")
	} else {
		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict || apiErr.Code != "already_exists" {
			t.Fatalf("duplicate create error = %v, want 409 already_exists", err)
		}
	}
	if _, err := cli.CreateProvider(ctx, api.CreateProviderRequest{
		Scope: "workspace", Name: "Bad Name!", Type: "openai",
	}); err == nil {
		t.Fatal("expected bad_request for invalid slug")
	}
}

func TestAssignModelAliasesAPIMovesAtomically(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()
	seedWorkspaceProvider(t, wsDir, "prova", `  type: openai
  models:
    - id: a-fast
      name: a-fast
      aliases: [fast]
`)
	seedWorkspaceProvider(t, wsDir, "provb", `  type: openai
  models:
    - id: b-fast
      name: b-fast
      aliases: [fast, smart]
`)

	resp, err := cli.AssignModelAliases(ctx, api.AssignModelAliasesRequest{
		Provider: "prova", Model: "a-fast", Aliases: []string{"fast"},
	})
	if err != nil {
		t.Fatalf("AssignModelAliases: %v", err)
	}
	target, ok := findWorkspaceProvider(resp.Providers, "prova")
	if !ok {
		t.Fatalf("prova missing from response providers")
	}
	if len(target.Models) != 1 || len(target.Models[0].Aliases) != 1 || target.Models[0].Aliases[0] != "fast" {
		t.Fatalf("target aliases = %+v, want [fast]", target.Models[0].Aliases)
	}
	source, _ := findWorkspaceProvider(resp.Providers, "provb")
	if len(source.Models) != 1 || len(source.Models[0].Aliases) != 1 || source.Models[0].Aliases[0] != "smart" {
		t.Fatalf("source aliases = %+v, want [smart] (fast moved away)", source.Models[0].Aliases)
	}

	aliases, err := cli.ListAliasAssignments(ctx)
	if err != nil {
		t.Fatalf("ListAliasAssignments: %v", err)
	}
	var fast *api.AliasAssignment
	for i := range aliases {
		if aliases[i].Alias == "fast" {
			fast = &aliases[i]
		}
	}
	if fast == nil || fast.Provider != "prova" || fast.Model != "a-fast" || fast.Duplicate {
		t.Fatalf("fast assignment = %+v, want sole owner prova/a-fast", fast)
	}
}

func TestAliasAssignmentsAPIReportsDefault(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()
	seedWorkspaceProvider(t, wsDir, "provapi", daemonTestProviderSpec)

	aliases, err := cli.ListAliasAssignments(ctx)
	if err != nil {
		t.Fatalf("ListAliasAssignments: %v", err)
	}
	var def *api.AliasAssignment
	for i := range aliases {
		if aliases[i].Alias == "default" {
			def = &aliases[i]
		}
	}
	if def == nil || def.Provider != "provapi" || def.Model != "mid-model" {
		t.Fatalf("default assignment = %+v, want provapi/mid-model", def)
	}
}

func TestProviderCatalogAPI(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()
	seedWorkspaceProvider(t, wsDir, "catprov", `  type: anthropic
  models:
    - id: claude-sonnet-5-5
      name: claude-sonnet-5-5
`)

	entries, err := cli.ListProviderCatalog(ctx, "catprov")
	if err != nil {
		t.Fatalf("ListProviderCatalog: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("anthropic catalog returned no entries")
	}
	var listed int
	for _, e := range entries {
		if e.Listed {
			listed++
		}
	}
	if listed != 1 {
		t.Errorf("listed entries = %d, want exactly the one declared model", listed)
	}

	if _, err := cli.ListProviderCatalog(ctx, "ghostprovider"); err == nil {
		t.Fatal("expected 404 for unknown provider catalog")
	}
}

func TestProviderCatalogAPIPrefersGatewayCatalog(t *testing.T) {
	d, cli, wsDir := setupDaemonTest(t, nil)
	d.cfg.GatewayPool.(*fakeGatewayPool).gw.models = map[string][]profile.Profile{
		"live-provider":  {{ID: "endpoint-model", Name: "Endpoint Model", Limits: profile.Limits{Context: 256000}}},
		"empty-provider": {},
	}
	for _, name := range []string{"live-provider", "empty-provider"} {
		seedWorkspaceProvider(t, wsDir, name, daemonTestProviderSpec)
	}

	entries, err := cli.ListProviderCatalog(t.Context(), "live-provider")
	if err != nil || len(entries) != 1 || entries[0].ID != "endpoint-model" || entries[0].ContextLimit != 256000 {
		t.Fatalf("catalog = %+v, %v, want only the configured provider's model", entries, err)
	}
	entries, err = cli.ListProviderCatalog(t.Context(), "empty-provider")
	if err != nil || len(entries) != 0 {
		t.Fatalf("empty catalog = %+v, %v", entries, err)
	}
}

func TestCreateProviderRejectsBrowserOAuthOverridesBeforeWriting(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	seedWorkspaceProvider(t, wsDir, "existing", daemonTestProviderSpec)
	if _, err := cli.ListWorkspaceProviders(t.Context()); err != nil {
		t.Fatal(err)
	}

	userDir, err := xdg.SubConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []struct{ name, dir string }{{"workspace", wsDir}, {"user", userDir}} {
		for _, name := range []string{"codex", "claudecode", "github-copilot"} {
			for _, tc := range []struct {
				label string
				edit  func(*api.CreateProviderRequest)
			}{
				{"name", func(r *api.CreateProviderRequest) { r.Name = "renamed" }},
				{"type", func(r *api.CreateProviderRequest) { r.Type = "openai" }},
				{"endpoint", func(r *api.CreateProviderRequest) { r.Endpoint = "https://api.example.test/v1" }},
				{"auth env", func(r *api.CreateProviderRequest) { r.AuthEnv = "CUSTOM_TOKEN" }},
				{"api key", func(r *api.CreateProviderRequest) { r.APIKey = "test-key" }},
				{"auth type", func(r *api.CreateProviderRequest) { r.AuthType = "none" }},
			} {
				t.Run(scope.name+"/"+name+"/"+tc.label, func(t *testing.T) {
					req := api.CreateProviderRequest{Scope: scope.name, Name: name, Type: name}
					tc.edit(&req)
					_, err := cli.CreateProvider(t.Context(), req)
					var apiErr *api.APIError
					if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest || apiErr.Code != "bad_request" {
						t.Errorf("CreateProvider error = %v, want 400 bad_request", err)
					}
					for _, path := range []string{
						filepath.Join(scope.dir, ".env"),
						filepath.Join(scope.dir, ".agents", "providers", req.Name+".provider.yaml"),
						filepath.Join(scope.dir, "agents", "providers", req.Name+".provider.yaml"),
					} {
						if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
							t.Errorf("stat %s after rejected create = %v, want not exist", path, err)
						}
					}
				})
			}
		}
	}
}

func TestCreateProviderCopiesOAuthPresetModels(t *testing.T) {
	for _, name := range []string{"antigravity", "codex", "claudecode", "github-copilot"} {
		t.Run(name, func(t *testing.T) {
			_, cli, _ := setupDaemonTest(t, nil)
			ctx := t.Context()
			provider, err := cli.CreateProvider(ctx, api.CreateProviderRequest{Name: name})
			if err != nil {
				t.Fatal(err)
			}
			preset := findPreset(name)
			if provider.DefaultModel != preset.Spec.DefaultModels.Completion || len(provider.Models) != len(preset.Spec.Models) {
				t.Fatalf("created provider = %+v, want default and models from the preset", provider)
			}
			catalog, err := cli.ListProviderCatalog(ctx, name)
			if err != nil || len(catalog) != len(preset.Spec.Models) {
				t.Fatalf("catalog before sign-in = %+v, %v", catalog, err)
			}
		})
	}
}

func TestCreatePresetRejectsImplicitAuthAtDifferentOrigin(t *testing.T) {
	_, cli, _ := setupDaemonTest(t, nil)
	_, err := cli.CreateProvider(t.Context(), api.CreateProviderRequest{
		Scope: "workspace", Name: "openai", Type: "openai", Endpoint: "https://api.example.test/v1",
	})
	if err == nil || !strings.Contains(err.Error(), "explicit credential") {
		t.Fatalf("expected credential requirement, got %v", err)
	}
	t.Setenv("OPENAI_API_KEY", "vendor-only")
	created, err := cli.CreateProvider(t.Context(), api.CreateProviderRequest{
		Scope: "workspace", Name: "openai", Type: "openai", Endpoint: "https://api.example.test/v1",
		APIKey: "endpoint-only",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.AuthEnv == "OPENAI_API_KEY" || created.AuthEnv == "" || created.HasKey {
		t.Fatalf("custom endpoint did not isolate its credential: %+v", created)
	}
}

// TestCreateProviderCustomTypeDoesNotInheritPreset guards the secret-leak and
// dangling-default path: a custom provider named differently from its type may
// borrow the preset endpoint but must never copy the preset's auth (which
// would point an existing secret at a custom endpoint), config, default
// models, or model list.
func TestCreateProviderCustomTypeDoesNotInheritPreset(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()

	created, err := cli.CreateProvider(ctx, api.CreateProviderRequest{
		Scope:    "workspace",
		Name:     "vllm",
		Type:     "openai",
		Endpoint: "http://localhost:8000/v1",
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	if created.Name != "vllm" {
		t.Fatalf("created = %+v, want vllm", created)
	}

	manifest, err := os.ReadFile(filepath.Join(wsDir, ".agents", "providers", "vllm.provider.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(manifest)
	if !strings.Contains(got, "endpoint: http://localhost:8000/v1") {
		t.Errorf("custom manifest lost the requested endpoint:\n%s", got)
	}
	for _, forbidden := range []string{"OPENAI_API_KEY", "defaultModels", "gpt-6", "store: false"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("custom manifest inherited preset %q:\n%s", forbidden, got)
		}
	}
	if strings.Contains(got, "auth:") {
		t.Errorf("keyless custom manifest declares auth:\n%s", got)
	}
}

// TestCreateProviderHonorsNoneAuthType proves an explicit auth type "none"
// without a key or env var writes an unauthenticated manifest instead of
// silently falling through to the preset's auth.
func TestCreateProviderHonorsNoneAuthType(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()

	if _, err := cli.CreateProvider(ctx, api.CreateProviderRequest{
		Scope: "workspace", Name: "local", Type: "ollama", AuthType: "none",
	}); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(wsDir, ".agents", "providers", "local.provider.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(manifest)
	if !strings.Contains(got, "type: none") {
		t.Errorf("manifest lacks auth type none:\n%s", got)
	}
	if strings.Contains(got, "OLLAMA_API_KEY") {
		t.Errorf("none-auth manifest still references a preset secret:\n%s", got)
	}
}

// TestCreateProviderPresetKeepsAuthScheme proves a preset-backed create with a
// pasted key preserves the preset's auth scheme and header (api-key +
// X-Api-Key for anthropic) rather than rewriting it to bearer, and routes the
// secret to the preset's own env var.
func TestCreateProviderPresetKeepsAuthScheme(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()

	if _, err := cli.CreateProvider(ctx, api.CreateProviderRequest{
		Scope: "workspace", Name: "anthropic", Type: "anthropic", APIKey: "sk-ant-test",
	}); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(wsDir, ".agents", "providers", "anthropic.provider.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(manifest)
	if !strings.Contains(got, "type: api-key") || !strings.Contains(got, "header: X-Api-Key") {
		t.Errorf("preset auth scheme was clobbered:\n%s", got)
	}
	if strings.Contains(got, "type: bearer") {
		t.Errorf("preset auth rewritten to bearer:\n%s", got)
	}
	if !strings.Contains(got, "${env:ANTHROPIC_API_KEY}") {
		t.Errorf("preset env var not kept:\n%s", got)
	}
	envFile, err := os.ReadFile(filepath.Join(wsDir, ".env"))
	if err != nil {
		t.Fatalf("workspace .env not written: %v", err)
	}
	if !strings.Contains(string(envFile), "ANTHROPIC_API_KEY=sk-ant-test") {
		t.Errorf("secret not stored under the preset env var:\n%s", envFile)
	}
}

// TestCreateProviderCustomModelsDropPresetDefaults proves that replacing a
// preset's model list clears preset default-model references that no longer
// resolve instead of shipping a dangling completion default.
func TestCreateProviderCustomModelsDropPresetDefaults(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()

	if _, err := cli.CreateProvider(ctx, api.CreateProviderRequest{
		Scope:  "workspace",
		Name:   "openai",
		Type:   "openai",
		Models: []api.ProviderModelRequest{{ID: "my-model", Name: "my-model"}},
	}); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(wsDir, ".agents", "providers", "openai.provider.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(manifest)
	if strings.Contains(got, "defaultModels") {
		t.Errorf("dangling preset defaults survived the model replacement:\n%s", got)
	}
	if !strings.Contains(got, "my-model") || strings.Contains(got, "gpt-6") {
		t.Errorf("model list not replaced as requested:\n%s", got)
	}
}

func TestCreateProviderForeignOriginOAuthAndCredentialLessPresets(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()

	// Credential-less preset (ollama) at a different origin succeeds without explicit authType.
	if _, err := cli.CreateProvider(ctx, api.CreateProviderRequest{
		Scope:    "workspace",
		Name:     "ollama",
		Type:     "ollama",
		Endpoint: "http://127.0.0.1:11434",
	}); err != nil {
		t.Fatalf("CreateProvider(ollama foreign origin): %v", err)
	}

	// OAuth2 preset (antigravity) at a foreign origin must reject oauth2 auth.
	for _, authType := range []string{"oauth2", "OAuth2"} {
		_, err := cli.CreateProvider(ctx, api.CreateProviderRequest{
			Scope:    "workspace",
			Name:     "antigravity",
			Type:     "antigravity",
			Endpoint: "https://foreign.example/v1",
			AuthType: authType,
		})
		if err == nil || !strings.Contains(err.Error(), "oauth2 authentication cannot be used with a different endpoint origin") {
			t.Fatalf("foreign-origin antigravity with %q: expected OAuth2 origin error, got %v", authType, err)
		}
	}

	// OAuth2 preset at a foreign origin with an explicit API key must write bearer, not oauth2.
	if _, err := cli.CreateProvider(ctx, api.CreateProviderRequest{
		Scope:    "workspace",
		Name:     "antigravity",
		Type:     "antigravity",
		Endpoint: "https://foreign.example/v1",
		APIKey:   "endpoint-only-secret",
	}); err != nil {
		t.Fatalf("CreateProvider(antigravity foreign origin with APIKey): %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(wsDir, ".agents", "providers", "antigravity.provider.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(manifest)
	if strings.Contains(got, "type: oauth2") {
		t.Errorf("foreign-origin antigravity inherited oauth2 auth:\n%s", got)
	}
	if !strings.Contains(got, "type: bearer") || !strings.Contains(got, "ANTIGRAVITY_ENDPOINT_") {
		t.Errorf("expected bearer auth with endpoint-scoped env var:\n%s", got)
	}
}

func TestPresetInstallationPreservesAliasOwners(t *testing.T) {
	for _, installer := range []string{"setup", "providers"} {
		for _, scenario := range []string{"fresh", "workspace", "user"} {
			t.Run(installer+"/"+scenario, func(t *testing.T) {
				_, cli, dir := setupDaemonTest(t, nil)
				ctx := t.Context()
				scope := "workspace"
				var ownerPath string
				var before []byte
				wantSkipped := map[string]api.SkippedPresetAlias{}
				if scenario != "fresh" {
					const ownerManifest = `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: existing
spec:
  type: openai
  auth:
    type: none
  models:
    - id: existing-model
      name: existing-model
      aliases: [smart]
`
					var err error
					ownerPath, err = xdg.SubConfigDir("agents", "providers", "existing.provider.yaml")
					if err != nil {
						t.Fatal(err)
					}
					if err := os.MkdirAll(filepath.Dir(ownerPath), 0o755); err != nil {
						t.Fatal(err)
					}
					before = []byte(ownerManifest)
					if err := os.WriteFile(ownerPath, before, 0o644); err != nil {
						t.Fatal(err)
					}
					wantSkipped["@smart"] = api.SkippedPresetAlias{Alias: "@smart", ModelID: "openai/gpt-6.2-sol", Owner: "existing/existing-model"}
				}
				if scenario == "workspace" {
					const ws = `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: test
spec:
  projects: ['.']
  modelAliases:
    fast: existing/existing-model
---
`
					if err := os.WriteFile(filepath.Join(dir, "WORKSPACE.md"), []byte(ws), 0o644); err != nil {
						t.Fatal(err)
					}
					wantSkipped["@fast"] = api.SkippedPresetAlias{Alias: "@fast", ModelID: "openai/gpt-6-luna", Owner: "existing/existing-model", WorkspaceOverride: true}
				}
				if scenario == "user" {
					scope = "user"
					seedWorkspaceProvider(t, dir, "existing", "  type: openai\n  enabled: false\n")
				}

				var skipped []api.SkippedPresetAlias
				if installer == "setup" {
					result, err := cli.InitWorkspaceWithResult(ctx, api.InitWorkspaceRequest{CWD: dir, Scope: scope, Provider: "openai"})
					if err != nil {
						t.Fatal(err)
					}
					skipped = result.SkippedPresetAliases
				} else {
					result, err := cli.CreateProvider(ctx, api.CreateProviderRequest{Scope: scope, Name: "openai"})
					if err != nil {
						t.Fatal(err)
					}
					skipped = result.SkippedPresetAliases
				}
				if len(skipped) != len(wantSkipped) {
					t.Fatalf("skipped = %+v, want %+v", skipped, wantSkipped)
				}
				for _, warning := range skipped {
					if warning != wantSkipped[warning.Alias] {
						t.Errorf("unexpected warning: %+v", warning)
					}
				}
				providers, err := cli.ListWorkspaceProviders(ctx)
				if err != nil {
					t.Fatal(err)
				}
				installed, ok := findWorkspaceProvider(providers, "openai")
				preset := findPreset("openai")
				if !ok || len(installed.Models) != len(preset.Spec.Models) {
					t.Fatalf("installed provider = %+v; want all preset models", installed)
				}
				for i, model := range installed.Models {
					original := preset.Spec.Models[i]
					want := slices.DeleteFunc(slices.Clone(original.Aliases), func(alias string) bool {
						_, skip := wantSkipped["@"+alias]
						return skip
					})
					if model.ID != original.ID || !slices.Equal(model.Aliases, want) {
						t.Errorf("installed model = %+v; want ID %q with aliases %v", model, original.ID, want)
					}
				}
				if ownerPath != "" {
					after, err := os.ReadFile(ownerPath)
					if err != nil || !bytes.Equal(after, before) {
						t.Fatalf("existing provider manifest changed: %v", err)
					}
				}
				if scenario == "workspace" {
					assignments, err := cli.ListAliasAssignments(ctx)
					if err != nil {
						t.Fatal(err)
					}
					for _, alias := range []string{"smart", "fast"} {
						if !slices.ContainsFunc(assignments, func(a api.AliasAssignment) bool {
							return a.Alias == alias && a.Provider == "existing" && a.Model == "existing-model" && !a.Duplicate && a.WorkspaceDefault == (alias == "fast")
						}) {
							t.Errorf("alias %q lost its owner: %+v", alias, assignments)
						}
					}
					models, err := cli.ListModels(ctx)
					if err != nil {
						t.Fatal(err)
					}
					for _, id := range []string{"existing/existing-model", "openai/gpt-6.2-sol"} {
						if !slices.ContainsFunc(models, func(m api.Model) bool { return m.ID == id && m.Capabilities.SupportsCompletion() }) {
							t.Errorf("model %q is not selectable by ID", id)
						}
					}
				}
			})
		}
	}
}
