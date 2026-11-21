package workspace

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/warp"
)

// writeProviderFile drops a ModelProvider manifest into dir and returns its path.
// initWorkspaceTier drops a minimal WORKSPACE.md so the loader treats the
// temporary directory as a configured workspace and walks .agents/.
func initWorkspaceTier(t *testing.T, root string) {
	t.Helper()
	body := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: test-ws\n---\n"
	if err := os.WriteFile(filepath.Join(root, "WORKSPACE.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeProviderFile(t *testing.T, dir, name, spec string) string {
	t.Helper()
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

const testProviderSpec = `  type: openai
  endpoint: https://api.example.test/v1
  auth:
    type: bearer
    token: ${env:TESTPROV_API_KEY}
  defaultModels:
    completion: mid-model
  models:
    - id: fast-model
      name: fast-model
      aliases: [fast]
    - id: mid-model
      name: mid-model
      aliases: [balanced, smart]
`

// openProviderTestWorkspace seeds a user-tier provider plus a workspace root
// and opens it, returning the workspace, the workspace tier dir, and the user
// manifest path.
func openProviderTestWorkspace(t *testing.T) (*Workspace, string, string) {
	t.Helper()
	xdgRoot := setupTestXDG(t)
	userDir := filepath.Join(xdgRoot, "config", "tasksmith-test", "agents", "providers")
	userPath := writeProviderFile(t, userDir, "testprov", testProviderSpec)

	root := t.TempDir()
	initWorkspaceTier(t, root)
	w, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	return w, filepath.Join(root, ".agents", "providers"), userPath
}

func TestFilterPresetAliasesNormalizesWhitespace(t *testing.T) {
	w, _, _ := openProviderTestWorkspace(t)
	wantSkipped := []api.SkippedPresetAlias{{Alias: "@smart", ModelID: "newprov/new-model", Owner: "testprov/mid-model"}}
	wantAliases := []string{"unique"}

	for _, tt := range []struct {
		name  string
		alias string
	}{
		{"bare", "smart"},
		{"surrounding whitespace", " @smart "},
		{"whitespace after prefix", "@ smart"},
		{"mixed case and whitespace", "\t@\tSmArT\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			spec := warp.ModelProviderSpec{Models: []warp.ProviderModel{{
				ID:      "new-model",
				Aliases: []string{tt.alias, "unique"},
			}}}
			skipped := w.FilterPresetAliases("newprov", "workspace", &spec, nil)
			if !slices.Equal(skipped, wantSkipped) {
				t.Errorf("skipped aliases for %q = %+v, want %+v", tt.alias, skipped, wantSkipped)
			}
			if got := spec.Models[0].Aliases; !slices.Equal(got, wantAliases) {
				t.Errorf("retained aliases = %q, want %q", got, wantAliases)
			}
		})
	}
}

func TestProviderRecordsListsUserTier(t *testing.T) {
	w, _, userPath := openProviderTestWorkspace(t)

	rec, ok := w.ProviderRecord("testprov")
	if !ok {
		t.Fatal("testprov not found in provider records")
	}
	if !rec.Enabled {
		t.Error("enabled = false, want true (spec.enabled omitted defaults true)")
	}
	if rec.Path != userPath {
		t.Errorf("path = %q, want %q", rec.Path, userPath)
	}
	if !rec.Writable {
		t.Error("writable = false, want true for a user-tier file manifest")
	}
	if !rec.Overridable {
		t.Error("overridable = false, want true for a user-tier provider")
	}
	if rec.Shadowed {
		t.Error("shadowed = true, want false with a single tier")
	}
}

func TestSetProviderEnabledPatchesOwnWorkspaceManifest(t *testing.T) {
	setupTestXDG(t)
	root := t.TempDir()
	initWorkspaceTier(t, root)
	wsDir := filepath.Join(root, ".agents", "providers")
	wsPath := writeProviderFile(t, wsDir, "testprov", testProviderSpec)

	w, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetProviderEnabled("testprov", false); err != nil {
		t.Fatal(err)
	}
	if err := w.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(wsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "enabled: false") {
		t.Fatalf("workspace manifest lacks enabled: false:\n%s", data)
	}
	rec, _ := w.ProviderRecord("testprov")
	if rec.Enabled {
		t.Error("record still reports enabled after disable")
	}

	if err := w.SetProviderEnabled("testprov", true); err != nil {
		t.Fatal(err)
	}
	if err := w.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(wsPath)
	if strings.Contains(string(data), "enabled: false") {
		t.Fatalf("enabled: false survived re-enable:\n%s", data)
	}
}

func TestSetProviderEnabledMutesUserTierViaShadow(t *testing.T) {
	w, wsDir, userPath := openProviderTestWorkspace(t)

	if err := w.SetProviderEnabled("testprov", false); err != nil {
		t.Fatal(err)
	}
	if err := w.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}

	shadowPath := filepath.Join(wsDir, "testprov.provider.yaml")
	shadow, err := os.ReadFile(shadowPath)
	if err != nil {
		t.Fatalf("expected workspace shadow override: %v", err)
	}
	if !strings.Contains(string(shadow), "enabled: false") {
		t.Fatalf("shadow lacks enabled: false:\n%s", shadow)
	}

	user, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(user), "enabled:") {
		t.Errorf("user manifest was edited when it should stay untouched:\n%s", user)
	}

	rec, ok := w.ProviderRecord("testprov")
	if !ok {
		t.Fatal("provider vanished after shadow write")
	}
	if rec.Enabled {
		t.Error("record still enabled after shadow")
	}
	if !rec.Shadowed {
		t.Error("record not flagged shadowed")
	}
	if !rec.InheritedEnabled {
		t.Error("InheritedEnabled = false, want true (user manifest is enabled)")
	}

	// Re-enabling removes the shadow entirely because the inherited manifest
	// is already enabled, restoring the user tier as the winner.
	if err := w.SetProviderEnabled("testprov", true); err != nil {
		t.Fatal(err)
	}
	if err := w.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(shadowPath); !os.IsNotExist(err) {
		t.Fatalf("shadow override still present after re-enable: err=%v", err)
	}
	rec, _ = w.ProviderRecord("testprov")
	if !rec.Enabled || rec.Shadowed {
		t.Errorf("record = enabled:%v shadowed:%v, want enabled:true shadowed:false", rec.Enabled, rec.Shadowed)
	}
}

// TestSetProviderEnabledFullOverrideNeverDeleted guards hand-authored
// workspace overrides: a workspace manifest that shares a user-tier name but
// carries real content (endpoint, auth, defaults, models) is a full override,
// not a mute-only shadow. Disabling and re-enabling it must patch the flag in
// place and must never delete the file.
func TestSetProviderEnabledFullOverrideNeverDeleted(t *testing.T) {
	xdgRoot := setupTestXDG(t)
	userDir := filepath.Join(xdgRoot, "config", "tasksmith-test", "agents", "providers")
	writeProviderFile(t, userDir, "testprov", testProviderSpec)

	root := t.TempDir()
	initWorkspaceTier(t, root)
	wsPath := writeProviderFile(t, filepath.Join(root, ".agents", "providers"), "testprov", `  type: openai
  endpoint: https://custom.example.test/v1
  models:
    - id: local-model
      name: local-model
`)
	w, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := w.ProviderRecord("testprov")
	if !ok || !rec.Shadowed {
		t.Fatalf("record = %+v, want a shadowed workspace override", rec)
	}

	if err := w.SetProviderEnabled("testprov", false); err != nil {
		t.Fatal(err)
	}
	if err := w.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(wsPath)
	if err != nil {
		t.Fatalf("full override deleted on disable: %v", err)
	}
	if !strings.Contains(string(data), "enabled: false") || !strings.Contains(string(data), "custom.example.test") {
		t.Fatalf("disable did not patch the override in place:\\n%s", data)
	}

	if err := w.SetProviderEnabled("testprov", true); err != nil {
		t.Fatal(err)
	}
	if err := w.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(wsPath)
	if err != nil {
		t.Fatalf("full override deleted on re-enable: %v", err)
	}
	got := string(data)
	if strings.Contains(got, "enabled:") {
		t.Errorf("enabled flag survived re-enable:\\n%s", got)
	}
	for _, keep := range []string{"custom.example.test", "local-model"} {
		if !strings.Contains(got, keep) {
			t.Errorf("re-enable lost override content %q:\\n%s", keep, got)
		}
	}
	rec, _ = w.ProviderRecord("testprov")
	if !rec.Enabled {
		t.Error("record disabled after re-enabling the override")
	}
}

// TestSetProviderEnabledPromotesDisabledInheritedTier covers re-enabling a
// muted provider whose inherited manifest is itself disabled: the mute-only
// shadow must be replaced by a full copy of the inherited definition (a shadow
// can mute but never un-mute), not by an empty {type, enabled} stub.
func TestSetProviderEnabledPromotesDisabledInheritedTier(t *testing.T) {
	xdgRoot := setupTestXDG(t)
	userDir := filepath.Join(xdgRoot, "config", "tasksmith-test", "agents", "providers")
	writeProviderFile(t, userDir, "testprov", `  enabled: false
  type: openai
  endpoint: https://api.example.test/v1
  models:
    - id: fast-model
      name: fast-model
`)

	root := t.TempDir()
	initWorkspaceTier(t, root)
	wsDir := filepath.Join(root, ".agents", "providers")
	writeProviderFile(t, wsDir, "testprov", `  type: openai
  enabled: false
`)
	w, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	pre, ok := w.ProviderRecord("testprov")
	if !ok || !pre.Shadowed || pre.InheritedEnabled {
		t.Fatalf("record = shadowed:%v inheritedEnabled:%v, want shadowed over a disabled tier", pre.Shadowed, pre.InheritedEnabled)
	}
	if err := w.SetProviderEnabled("testprov", true); err != nil {
		t.Fatal(err)
	}
	if err := w.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}

	promotedPath := filepath.Join(wsDir, "testprov.provider.yaml")
	data, err := os.ReadFile(promotedPath)
	if err != nil {
		t.Fatalf("promoted workspace manifest missing: %v", err)
	}
	got := string(data)
	for _, keep := range []string{"endpoint: https://api.example.test/v1", "fast-model"} {
		if !strings.Contains(got, keep) {
			t.Errorf("promoted manifest lost inherited content %q:\\n%s", keep, got)
		}
	}
	if strings.Contains(got, "enabled: false") {
		t.Errorf("promoted manifest still disabled:\\n%s", got)
	}
	rec, ok := w.ProviderRecord("testprov")
	if !ok || !rec.Enabled {
		t.Fatalf("record = %+v ok:%v, want an enabled promoted provider", rec, ok)
	}
}

func TestUpdateProviderModelsRewritesManifest(t *testing.T) {
	root := t.TempDir()
	initWorkspaceTier(t, root)
	wsPath := writeProviderFile(t, filepath.Join(root, ".agents", "providers"), "testprov", testProviderSpec)
	w, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	err = w.UpdateProviderModels("testprov", []ProviderModelEdit{
		{ID: "new-model", Name: "new-model", Context: 1000, Output: 200},
		{ID: "other", Name: "other", Aliases: []string{"fast"}},
	}, "new-model", true)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(wsPath)
	got := string(data)
	for _, want := range []string{"new-model", "other", "completion: new-model", "context: 1000"} {
		if !strings.Contains(got, want) {
			t.Errorf("manifest lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "mid-model") {
		t.Errorf("old model survived replacement:\n%s", got)
	}
}

// richProviderSpec declares per-model capabilities, cost tiers, and embedding
// dimensions plus all three default-model roles — the manifest content the
// models editor never surfaces and must therefore never rewrite.
const richProviderSpec = `  type: openai
  defaultModels:
    completion: fast-model
    decision: mid-model
    embedding: embed-model
  models:
    - id: fast-model
      name: fast-model
      aliases: [fast]
      limits:
        context: 200000
        output: 64000
        dimensions: 1536
      capabilities:
        tools: true
        thinking:
          - type: enabled
      cost:
        input: 3
        output: 15
    - id: mid-model
      name: mid-model
    - id: embed-model
      name: embed-model
`

// TestUpdateProviderModelsPreservesUneditedFields proves adding a model keeps
// capabilities, cost, and dimension limits on every surviving model, and that
// zero-valued edits never overwrite declared limits.
func TestUpdateProviderModelsPreservesUneditedFields(t *testing.T) {
	root := t.TempDir()
	initWorkspaceTier(t, root)
	wsPath := writeProviderFile(t, filepath.Join(root, ".agents", "providers"), "testprov", richProviderSpec)
	w, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	// Keep every declared model (by id) and add new-model: nothing the
	// defaults point at is removed, so all roles must survive untouched.
	err = w.UpdateProviderModels("testprov", []ProviderModelEdit{
		{ID: "fast-model", Name: "fast-model"},
		{ID: "mid-model", Name: "mid-model"},
		{ID: "embed-model", Name: "embed-model"},
		{ID: "new-model", Name: "new-model", Context: 4096},
	}, "", false)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(wsPath)
	got := string(data)
	for _, keep := range []string{
		"dimensions: 1536", "input: 3", "output: 15", "tools: true", "thinking:",
		"decision: mid-model", "embedding: embed-model",
	} {
		if !strings.Contains(got, keep) {
			t.Errorf("model edit lost %q in\n%s", keep, got)
		}
	}
	// The completion default pointed at a model that survived, so it stays;
	// zero-valued limits from the edit must not overwrite the manifest's.
	if !strings.Contains(got, "completion: fast-model") {
		t.Errorf("surviving model's completion default vanished in\n%s", got)
	}
	if !strings.Contains(got, "context: 200000") {
		t.Errorf("existing context limit overwritten by a zero-value edit in\n%s", got)
	}
	if !strings.Contains(got, "context: 4096") {
		t.Errorf("new model context missing in\n%s", got)
	}
}

// TestUpdateProviderModelsRemovalClearsDanglingCompletion proves that marking
// the current completion default for removal (without a defaultSet flag) drops
// the dangling spec.defaultModels.completion while decision and embedding
// defaults survive, and that clearing the last role drops the whole block.
func TestUpdateProviderModelsRemovalClearsDanglingCompletion(t *testing.T) {
	root := t.TempDir()
	initWorkspaceTier(t, root)
	wsPath := writeProviderFile(t, filepath.Join(root, ".agents", "providers"), "testprov", richProviderSpec)
	w, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	err = w.UpdateProviderModels("testprov", []ProviderModelEdit{
		{ID: "mid-model", Name: "mid-model"},
		{ID: "embed-model", Name: "embed-model"},
	}, "", false)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(wsPath)
	got := string(data)
	if strings.Contains(got, "completion:") {
		t.Errorf("dangling completion default survived removing its model in\n%s", got)
	}
	if !strings.Contains(got, "decision: mid-model") || !strings.Contains(got, "embedding: embed-model") {
		t.Errorf("other default roles wiped with the completion default in\n%s", got)
	}

	// Removing every model the remaining default roles point at must drop the
	// whole defaults block rather than leave empty references behind.
	err = w.UpdateProviderModels("testprov", []ProviderModelEdit{
		{ID: "other", Name: "other"},
	}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(wsPath)
	if strings.Contains(string(data), "defaultModels:") {
		t.Errorf("empty defaultModels block left behind in\n%s", data)
	}
}

func TestAssignModelAliasesMovesAtomicallyAcrossManifests(t *testing.T) {
	root := t.TempDir()
	initWorkspaceTier(t, root)
	dir := filepath.Join(root, ".agents", "providers")
	pathA := writeProviderFile(t, dir, "prova", `  type: openai
  models:
    - id: a-fast
      name: a-fast
      aliases: [fast]
`)
	pathB := writeProviderFile(t, dir, "provb", `  type: openai
  models:
    - id: b-fast
      name: b-fast
      aliases: [fast, smart]
`)
	w, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	// Assigning @fast to prova/a-fast must strip it from provb/b-fast while
	// leaving @smart there untouched.
	if err := w.AssignModelAliases("prova", "a-fast", []string{"fast"}, false); err != nil {
		t.Fatal(err)
	}
	if err := w.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}

	aData, _ := os.ReadFile(pathA)
	if !strings.Contains(string(aData), "fast") {
		t.Fatalf("target manifest lost @fast:\n%s", aData)
	}
	bData, _ := os.ReadFile(pathB)
	if !strings.Contains(string(bData), "smart") {
		t.Fatalf("source manifest lost @smart too (whole alias list replaced?):\n%s", bData)
	}
	// b-fast keeps smart but must no longer own fast: no aliases line in B
	// may mention it (the model id "b-fast" itself is allowed to).
	for line := range strings.SplitSeq(string(bData), "\n") {
		if strings.Contains(line, "aliases:") && strings.Contains(line, "fast") {
			t.Fatalf("source manifest still declares @fast on b-fast:\n%s", bData)
		}
	}

	assignments := w.AliasAssignments()
	var fast *AliasAssignment
	for i := range assignments {
		if assignments[i].Alias == "fast" {
			fast = &assignments[i]
		}
	}
	if fast == nil || len(fast.Owners) != 1 || fast.Owners[0].Provider != "prova" {
		t.Fatalf("fast owners = %+v, want exactly prova/a-fast", fast)
	}
}

func TestAssignModelAliasesDefaultRoutesToCompletion(t *testing.T) {
	root := t.TempDir()
	initWorkspaceTier(t, root)
	path := writeProviderFile(t, filepath.Join(root, ".agents", "providers"), "testprov", testProviderSpec)
	w, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	if err := w.AssignModelAliases("testprov", "fast-model", []string{"fast"}, true); err != nil {
		t.Fatal(err)
	}
	if err := w.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "completion: fast-model") {
		t.Fatalf("defaultModels.completion not moved to fast-model:\n%s", data)
	}

	// The alias report must show @default owned by the new winner so the
	// editor can render "assigned to this model".
	var def *AliasAssignment
	assignments := w.AliasAssignments()
	for i := range assignments {
		if assignments[i].Alias == "default" {
			def = &assignments[i]
		}
	}
	if def == nil || len(def.Owners) != 1 || def.Owners[0].Model != "fast-model" {
		t.Fatalf("default owners = %+v, want testprov/fast-model", def)
	}
}

func TestAssignModelAliasesRejectsReservedInherit(t *testing.T) {
	root := t.TempDir()
	writeProviderFile(t, filepath.Join(root, ".agents", "providers"), "testprov", testProviderSpec)
	w, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.AssignModelAliases("testprov", "fast-model", []string{"inherit"}, false); err == nil {
		t.Fatal("assigning reserved alias @inherit should fail")
	}
}

// TestAliasAssignmentsHonorsEffectiveListing proves muted providers drop out
// of the report: a user-tier manifest disabled in its own file, and one muted
// by a workspace shadow, must not contribute aliases or own @default.
func TestAliasAssignmentsHonorsEffectiveListing(t *testing.T) {
	xdgRoot := setupTestXDG(t)
	userDir := filepath.Join(xdgRoot, "config", "tasksmith-test", "agents", "providers")
	writeProviderFile(t, userDir, "offprov", `  type: openai
  enabled: false
  models:
    - id: o1
      name: o1
      aliases: [ghosttier]
`)
	writeProviderFile(t, userDir, "muted", `  type: openai
  models:
    - id: m1
      name: m1
      aliases: [stolen]
  defaultModels:
    completion: m1
`)

	root := t.TempDir()
	initWorkspaceTier(t, root)
	wsDir := filepath.Join(root, ".agents", "providers")
	writeProviderFile(t, wsDir, "muted", `  type: openai
  enabled: false
`)
	writeProviderFile(t, wsDir, "live", `  type: openai
  models:
    - id: l1
      name: l1
`)
	w, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]AliasAssignment{}
	for _, a := range w.AliasAssignments() {
		got[a.Alias] = a
	}
	if _, ok := got["ghosttier"]; ok {
		t.Errorf("alias from a disabled manifest leaked into the report: %+v", got["ghosttier"])
	}
	if _, ok := got["stolen"]; ok {
		t.Errorf("alias from a shadow-muted manifest leaked into the report: %+v", got["stolen"])
	}
	if def, ok := got["default"]; ok {
		t.Errorf("@default resolved to %+v, want unassigned (only muted manifests declare defaults)", def)
	}
}

// TestAliasAssignmentsHonorsWorkspaceSpec proves WORKSPACE.md wins: a
// modelAliases entry naming an alias no provider declares still appears (with
// Workspace set), its target resolves, and spec.defaultModels.completion
// overrides per-provider completion defaults for @default.
func TestAliasAssignmentsHonorsWorkspaceSpec(t *testing.T) {
	setupTestXDG(t)
	root := t.TempDir()
	body := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: test-ws\n" +
		"spec:\n  defaultModels:\n    completion: b1\n  modelAliases:\n    teamlead: prova/a1\n---\n"
	if err := os.WriteFile(filepath.Join(root, "WORKSPACE.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".agents", "providers")
	writeProviderFile(t, dir, "prova", `  type: openai
  defaultModels:
    completion: a1
  models:
    - id: a1
      name: a1
`)
	writeProviderFile(t, dir, "provb", `  type: openai
  models:
    - id: b1
      name: b1
`)
	w, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]AliasAssignment{}
	for _, a := range w.AliasAssignments() {
		got[a.Alias] = a
	}
	lead, ok := got["teamlead"]
	if !ok {
		t.Fatalf("WORKSPACE.md-only alias missing from report: %+v", got)
	}
	if !lead.Workspace || len(lead.Owners) != 1 || lead.Owners[0].Provider != "prova" || lead.Owners[0].Model != "a1" {
		t.Errorf("teamlead = %+v, want workspace-owned prova/a1", lead)
	}
	def, ok := got["default"]
	if !ok || len(def.Owners) != 1 || def.Owners[0].Provider != "provb" || def.Owners[0].Model != "b1" {
		t.Errorf("@default = %+v, want provb/b1 (WORKSPACE.md completion default wins)", got["default"])
	}
	// A WORKSPACE.md completion default outranks every manifest default, so
	// the editor must present @default as workspace-owned (saving a manifest
	// cannot move it).
	if !def.Workspace {
		t.Errorf("@default Workspace = false, want true (WORKSPACE.md completion default)")
	}
}

// TestAliasAssignmentsWorkspaceShadowsManifestAlias proves that when an alias
// is declared in BOTH a provider manifest and WORKSPACE.md modelAliases, the
// report names the winning WORKSPACE.md target (not the shadowed manifest
// owner) and marks the assignment workspace-owned without a duplicate flag.
func TestAliasAssignmentsWorkspaceShadowsManifestAlias(t *testing.T) {
	setupTestXDG(t)
	root := t.TempDir()
	body := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: test-ws\n" +
		"spec:\n  modelAliases:\n    teamlead: provb/b1\n---\n"
	if err := os.WriteFile(filepath.Join(root, "WORKSPACE.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".agents", "providers")
	writeProviderFile(t, dir, "prova", `  type: openai
  models:
    - id: a1
      name: a1
      aliases: [teamlead]
`)
	writeProviderFile(t, dir, "provb", `  type: openai
  models:
    - id: b1
      name: b1
`)
	w, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]AliasAssignment{}
	for _, a := range w.AliasAssignments() {
		got[a.Alias] = a
	}
	lead, ok := got["teamlead"]
	if !ok {
		t.Fatalf("teamlead missing from report: %+v", got)
	}
	if !lead.Workspace || len(lead.Owners) != 1 || lead.Owners[0].Provider != "provb" || lead.Owners[0].Model != "b1" {
		t.Errorf("teamlead = %+v, want workspace-owned provb/b1 (WORKSPACE.md shadows the manifest)", lead)
	}
	if len(lead.Duplicates) > 0 {
		t.Errorf("workspace-shadowed alias flagged as duplicate: %+v", lead)
	}
}

func TestAliasAssignmentsReportsDuplicates(t *testing.T) {
	root := t.TempDir()
	initWorkspaceTier(t, root)
	dir := filepath.Join(root, ".agents", "providers")
	writeProviderFile(t, dir, "prova", `  type: openai
  models:
    - id: a1
      name: a1
      aliases: [dup]
`)
	writeProviderFile(t, dir, "provb", `  type: openai
  models:
    - id: b1
      name: b1
      aliases: [dup]
`)
	w, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	var dup *AliasAssignment
	assignments := w.AliasAssignments()
	for i := range assignments {
		if assignments[i].Alias == "dup" {
			dup = &assignments[i]
		}
	}
	if dup == nil {
		t.Fatal("dup alias missing from report")
	}
	if len(dup.Owners) != 2 || len(dup.Duplicates) != 1 {
		t.Fatalf("dup owners=%d duplicates=%d, want 2/1", len(dup.Owners), len(dup.Duplicates))
	}
}

// TestProviderAuthEnvScrubsDisabledProviders guards the secret-scrubbing
// invariant: env vars referenced by a disabled or shadow-muted provider's
// auth must stay out of SubprocessEnv (agent bash and custom tools), because
// the provider can be re-enabled at any time.
func TestProviderAuthEnvScrubsDisabledProviders(t *testing.T) {
	xdgRoot := setupTestXDG(t)
	userDir := filepath.Join(xdgRoot, "config", "tasksmith-test", "agents", "providers")
	writeProviderFile(t, userDir, "offprov", `  type: openai
  enabled: false
  auth:
    type: bearer
    token: ${env:OFFPROV_KEY}
  models:
    - id: o1
      name: o1
`)
	writeProviderFile(t, userDir, "muted", `  type: openai
  auth:
    type: bearer
    token: ${env:MUTEDPROV_KEY}
  models:
    - id: m1
      name: m1
`)

	t.Setenv("OFFPROV_KEY", "off-secret")
	t.Setenv("MUTEDPROV_KEY", "muted-secret")

	root := t.TempDir()
	initWorkspaceTier(t, root)
	writeProviderFile(t, filepath.Join(root, ".agents", "providers"), "muted", `  type: openai
  enabled: false
`)
	w, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	sub := w.SubprocessEnv()
	for _, blocked := range []string{"OFFPROV_KEY", "MUTEDPROV_KEY"} {
		if got, ok := sub[blocked]; ok {
			t.Errorf("SubprocessEnv leaked muted-provider secret %s=%q", blocked, got)
		}
	}
}

func TestSetProviderEnabledUnknown(t *testing.T) {
	root := t.TempDir()
	initWorkspaceTier(t, root)
	w, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetProviderEnabled("ghost", false); err == nil {
		t.Fatal("expected ErrProviderNotFound for unknown provider")
	}
}

func TestProviderAuthEnvNames(t *testing.T) {
	auth := &warp.ProviderAuth{Type: warp.AuthTypeBearer, Token: "${env:MY_KEY}"}
	names := ProviderAuthEnvNames(auth)
	if len(names) != 1 || names[0] != "MY_KEY" {
		t.Fatalf("ProviderAuthEnvNames = %v, want [MY_KEY]", names)
	}
}
