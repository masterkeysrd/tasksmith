package daemon

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
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
	_, cli, wsDir := setupDaemonTest(t, nil)
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
    - id: claude-sonnet-4-5
      name: claude-sonnet-4-5
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
