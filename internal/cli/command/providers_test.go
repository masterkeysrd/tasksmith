package command

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

// providerFixture serves the daemon's workspace-provider endpoints from memory
// so the /providers panes can be exercised without a running daemon. Mutations
// update the in-memory listing, mirroring the daemon's patch-then-reload flow.
type providerFixture struct {
	t         *testing.T
	mu        sync.Mutex
	providers []api.WorkspaceProvider
	aliases   []api.AliasAssignment
	presets   []api.ProviderSummary
	catalog   []api.ProviderCatalogEntry
	created   []api.CreateProviderRequest
	assigned  []api.AssignModelAliasesRequest
}

func newProviderFixture(t *testing.T, providers []api.WorkspaceProvider) *providerFixture {
	t.Helper()
	f := &providerFixture{
		t:         t,
		providers: providers,
		aliases: []api.AliasAssignment{
			{Alias: "default", Provider: "prova", Model: "a-fast"},
			{Alias: "fast", Provider: "provb", Model: "b-fast"},
		},
		presets: []api.ProviderSummary{
			{Name: "openai", DisplayName: "OpenAI", AuthEnv: "OPENAI_API_KEY"},
			{Name: "anthropic", DisplayName: "Anthropic", AuthEnv: "ANTHROPIC_API_KEY"},
		},
		catalog: []api.ProviderCatalogEntry{
			{ID: "gpt-5.5", Name: "GPT-5.5", ContextLimit: 400000},
			{ID: "gpt-5.5-mini", Name: "GPT-5.5 Mini", ContextLimit: 400000},
		},
	}
	c, err := client.New("test.sock", client.WithCWD("/workspace"), client.WithHTTPClient(&http.Client{Transport: f}))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	store.ResetSession()
	store.ResetApp()
	store.ClearSlot()
	store.ClearTranscript()
	t.Cleanup(func() {
		mcpBackground.Wait()
		store.ResetSession()
		store.ResetApp()
		store.ClearSlot()
		store.ClearTranscript()
	})
	store.InitSession(context.Background(), c, store.InitSessionOptions{})
	store.SetDaemonStatus(store.DaemonConnected)
	return f
}

func testWorkspaceProviders() []api.WorkspaceProvider {
	return []api.WorkspaceProvider{
		{
			Name: "prova", Type: "openai", Scope: "workspace",
			Path:    "~/proj/.agents/providers/prova.provider.yaml",
			Enabled: true, Status: "active", Writable: true,
			DefaultModel: "a-fast",
			Models: []api.ProviderModelInfo{
				{ID: "a-fast", Name: "a-fast", Label: "Fast model", Context: 1000000, Aliases: []string{"default"}},
				{ID: "a-mid", Name: "a-mid"},
			},
		},
		{
			Name: "provb", Type: "anthropic", Scope: "user",
			Path:    "~/.config/tasksmith/agents/providers/provb.provider.yaml",
			Enabled: true, Status: "missing_auth", Writable: true, Overridable: true,
			AuthEnv: "PROVB_API_KEY",
			Models: []api.ProviderModelInfo{
				{ID: "b-fast", Name: "b-fast", Aliases: []string{"fast"}},
			},
		},
	}
}

func (f *providerFixture) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodGet && r.URL.Path == "/v1/events" {
		<-r.Context().Done()
		return nil, r.Context().Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/workspace/providers":
		return mcpJSONResponse(http.StatusOK, f.providers)

	case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/enabled"):
		name := providerNameInPath(r.URL.Path)
		var req api.SetProviderEnabledRequest
		if err := decodeJSONBody(r, &req); err != nil {
			return mcpJSONResponse(http.StatusBadRequest, api.APIError{Code: "bad_request", Message: err.Error()})
		}
		found := false
		for i := range f.providers {
			if f.providers[i].Name == name {
				f.providers[i].Enabled = req.Enabled
				f.providers[i].Status = "active"
				if !req.Enabled {
					f.providers[i].Status = "disabled"
				}
				found = true
			}
		}
		if !found {
			return mcpJSONResponse(http.StatusNotFound, api.APIError{Code: "not_found", Message: "unknown provider " + name})
		}
		return mcpJSONResponse(http.StatusOK, f.providers)

	case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/models"):
		name := providerNameInPath(r.URL.Path)
		var req api.UpdateProviderModelsRequest
		if err := decodeJSONBody(r, &req); err != nil {
			return mcpJSONResponse(http.StatusBadRequest, api.APIError{Code: "bad_request", Message: err.Error()})
		}
		for i := range f.providers {
			if f.providers[i].Name != name {
				continue
			}
			models := make([]api.ProviderModelInfo, 0, len(req.Models))
			for _, m := range req.Models {
				models = append(models, api.ProviderModelInfo(m))
			}
			f.providers[i].Models = models
			if req.DefaultSet {
				f.providers[i].DefaultModel = req.DefaultModel
			}
		}
		return mcpJSONResponse(http.StatusOK, f.providers)

	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/catalog"):
		return mcpJSONResponse(http.StatusOK, f.catalog)

	case r.Method == http.MethodPost && r.URL.Path == "/v1/workspace/providers":
		var req api.CreateProviderRequest
		if err := decodeJSONBody(r, &req); err != nil {
			return mcpJSONResponse(http.StatusBadRequest, api.APIError{Code: "bad_request", Message: err.Error()})
		}
		f.created = append(f.created, req)
		return mcpJSONResponse(http.StatusOK, api.WorkspaceProvider{
			Name: req.Name, Type: req.Type, Scope: req.Scope, Enabled: true, Status: "active", Writable: true,
		})

	case r.Method == http.MethodGet && r.URL.Path == "/v1/workspace/model-aliases":
		return mcpJSONResponse(http.StatusOK, f.aliases)

	case r.Method == http.MethodPost && r.URL.Path == "/v1/workspace/model-aliases/assign":
		var req api.AssignModelAliasesRequest
		if err := decodeJSONBody(r, &req); err != nil {
			return mcpJSONResponse(http.StatusBadRequest, api.APIError{Code: "bad_request", Message: err.Error()})
		}
		f.assigned = append(f.assigned, req)
		return mcpJSONResponse(http.StatusOK, api.AssignModelAliasesResponse{
			Providers: f.providers,
			Aliases:   f.aliases,
		})

	case r.Method == http.MethodGet && r.URL.Path == "/v1/providers":
		return mcpJSONResponse(http.StatusOK, f.presets)

	case r.Method == http.MethodGet && r.URL.Path == "/v1/workspace":
		return mcpJSONResponse(http.StatusOK, api.Workspace{Name: "test-ws"})

	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/providers/") && strings.HasSuffix(r.URL.Path, "/auth"):
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/providers/"), "/auth")
		return mcpJSONResponse(http.StatusOK, api.ProviderAuthResponse{
			Provider: name,
			Status:   "authorizing",
		})

	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/providers/") && strings.HasSuffix(r.URL.Path, "/auth"):
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/providers/"), "/auth")
		return mcpJSONResponse(http.StatusOK, api.ProviderAuthResponse{
			Provider: name,
			Status:   "authorizing",
		})
	}

	return mcpJSONResponse(http.StatusNotFound, api.APIError{Code: "not_found", Message: "unhandled path " + r.URL.Path})
}

func (f *providerFixture) enabledState(name string) (bool, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.providers {
		if p.Name == name {
			return p.Enabled, p.Status
		}
	}
	return false, ""
}

func (f *providerFixture) assignCalls() []api.AssignModelAliasesRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]api.AssignModelAliasesRequest, len(f.assigned))
	copy(out, f.assigned)
	return out
}

func providerNameInPath(p string) string {
	trimmed := strings.TrimPrefix(p, "/v1/workspace/providers/")
	if before, _, ok := strings.Cut(trimmed, "/"); ok {
		return before
	}
	return trimmed
}

func decodeJSONBody(r *http.Request, target any) error {
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(r.Body)
	return dec.Decode(target)
}

// mountProvidersCommand runs /providers and mounts whatever slot it enqueued so
// the browser renders against the fixture client.
func mountProvidersCommand(t *testing.T) *providerFixture {
	t.Helper()
	f := newProviderFixture(t, testWorkspaceProviders())
	reg := NewRegistry()
	if err := reg.Execute("/providers"); err != nil {
		t.Fatalf("Execute(/providers): %v", err)
	}
	return f
}

func TestProvidersCommandEnqueuesBrowserSlot(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()
	newProviderFixture(t, testWorkspaceProviders())

	reg := NewRegistry()
	if err := reg.Execute("/providers"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active := store.GetSlot().Active; active == nil || active.Kind != store.SlotKindPicker {
		t.Fatalf("expected picker slot, got %+v", active)
	}
	store.ClearSlot()

	if err := reg.Execute("/provider"); err != nil {
		t.Fatalf("alias should work: %v", err)
	}
	if active := store.GetSlot().Active; active == nil {
		t.Fatal("expected the alias to enqueue the browser slot")
	}
}

func TestProvidersBrowserListsGroupsAndToggles(t *testing.T) {
	f := mountProvidersCommand(t)
	env := mcpMountActiveSlot(t)

	frame := env.DumpText()
	if !strings.Contains(frame, "Workspace · ~/proj/.agents/providers") || !strings.Contains(frame, "User · ~/.config/tasksmith/agents/providers") {
		t.Fatalf("scope group headings missing:\n%s", frame)
	}
	for _, want := range []string{"❯ ✔ prova Ready 2 models", "⚠ provb Needs sign-in 1 model"} {
		found := false
		for line := range strings.Lines(frame) {
			if strings.Join(strings.Fields(line), " ") == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing row %q in frame:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "2 providers") {
		t.Fatalf("unexpected provider row decoration:\n%s", frame)
	}

	// Space on the highlighted row (prova sorts first) disables it through the
	// daemon endpoint, and the subtitle confirms the outcome.
	env.SendKey(key.Key{Code: ' ', Text: " "})
	if _, ok := waitForText(env, 2*time.Second, func(s string) bool {
		return strings.Contains(s, "Provider prova disabled")
	}); !ok {
		t.Logf("toggle confirmation notice not visible in frame")
	}
	mcpSettle(env)
	if enabled, status := f.enabledState("prova"); enabled || status != "disabled" {
		t.Fatalf("fixture state after space = enabled:%v status:%q, want disabled", enabled, status)
	}

	// Esc closes the browser and reports the exit in the timeline.
	env.SendKey(key.Key{Code: key.KeyEscape})
	env.Flush()
	if active := store.GetSlot().Active; active != nil {
		t.Fatalf("slot still active after esc: %+v", active)
	}
	if !strings.Contains(transcriptText(), "Exited /providers command") {
		t.Fatalf("exit notice missing from transcript: %q", transcriptText())
	}
}

// waitForText pumps the render loop until cond accepts the frame text.
func waitForText(env interface {
	Flush()
	DumpText() string
}, timeout time.Duration, cond func(string) bool) (string, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		env.Flush()
		if text := env.DumpText(); cond(text) {
			return text, true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return env.DumpText(), false
}

func TestProvidersBrowserEnterOpensModelsPane(t *testing.T) {
	mountProvidersCommand(t)
	env := mcpMountActiveSlot(t)

	env.SendKey(key.Key{Code: key.KeyEnter})
	text, ok := waitForText(env, 2*time.Second, func(s string) bool { return strings.Contains(s, "Models · prova") })
	if !ok {
		t.Fatalf("enter did not open the models pane:\n%s", text)
	}
	if !strings.Contains(text, "a-fast") || !strings.Contains(text, "a-mid") {
		t.Fatalf("models pane missing declared models:\n%s", text)
	}

	frame := strings.Join(strings.Fields(text), " ")
	if !strings.Contains(frame, "Fast model (default) a-fast 1.0M ctx · @default") ||
		strings.Contains(text, "★") || strings.Contains(text, "|") {
		t.Fatalf("unexpected model columns:\n%s", text)
	}
	env.SendKey(key.Key{Code: key.KeyDown})
	if frame, moved := waitForText(env, time.Second, func(s string) bool { return strings.Contains(s, "❯ a-mid") }); !moved {
		t.Fatalf("cursor did not move to the second model:\n%s", frame)
	}
	env.SendKey(key.Key{Code: 'd', Text: "d"})
	text, ok = waitForText(env, 2*time.Second, func(s string) bool {
		return strings.Contains(s, "a-mid (default)") && !strings.Contains(s, "Fast model (default)")
	})
	if !ok {
		t.Fatalf("staged default marker did not move:\n%s", text)
	}

	// Esc returns to the browser rather than closing the whole command.
	env.SendKey(key.Key{Code: key.KeyEscape})
	text, ok = waitForText(env, 2*time.Second, func(s string) bool { return strings.Contains(s, "Manage model providers") })
	if !ok {
		t.Fatalf("esc did not return to the browser:\n%s", text)
	}
	if store.GetSlot().Active == nil {
		t.Fatal("esc from the models pane must keep the command open")
	}
}

func TestProvidersModelsPaneMarksAndSaves(t *testing.T) {
	f := mountProvidersCommand(t)
	env := mcpMountActiveSlot(t)
	env.SendKey(key.Key{Code: 'm', Text: "m"})
	if _, ok := waitForText(env, 2*time.Second, func(s string) bool { return strings.Contains(s, "Models · prova") }); !ok {
		t.Fatal("models pane did not open")
	}

	// space marks the highlighted model (a-fast) for removal; s commits.
	env.SendKey(key.Key{Code: ' ', Text: " "})
	text, ok := waitForText(env, time.Second, func(s string) bool { return strings.Contains(s, "will remove") })
	if !ok {
		t.Fatalf("space did not mark the model:\n%s", text)
	}
	env.SendKey(key.Key{Code: 's', Text: "s"})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mcpSettle(env)
		_, models := fixtureModels(f, "prova")
		if len(models) == 1 && models[0].ID == "a-mid" {
			return
		}
	}
	_, models := fixtureModels(f, "prova")
	t.Fatalf("save did not remove the marked model: %+v", models)
}

func fixtureModels(f *providerFixture, name string) (bool, []api.ProviderModelInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.providers {
		if p.Name == name {
			return true, append([]api.ProviderModelInfo(nil), p.Models...)
		}
	}
	return false, nil
}

func TestProvidersBrowserAddPaneWalksPresetAndForm(t *testing.T) {
	f := mountProvidersCommand(t)
	env := mcpMountActiveSlot(t)

	env.SendKey(key.Key{Code: 'a', Text: "a"})
	text, ok := waitForText(env, 2*time.Second, func(s string) bool { return strings.Contains(s, "Custom provider") })
	if !ok {
		t.Fatalf("add pane preset list missing:\n%s", text)
	}
	if !strings.Contains(text, "OpenAI") {
		t.Fatalf("presets missing from the add pane:\n%s", text)
	}

	// Enter on the highlighted preset (openai sorts first) walks into the field
	// form: every manifest field row plus the submit row must render, with the
	// preset's name and auth env pre-filled.
	env.SendKey(key.Key{Code: key.KeyEnter})
	env.Flush()
	text, ok = waitForText(env, 2*time.Second, func(s string) bool { return strings.Contains(s, "Create provider") })
	if !ok {
		t.Fatalf("enter on a preset did not open the field form:\\n%s", text)
	}
	for _, want := range []string{"Scope", "Name", "openai", "Endpoint", "Auth env var", "OPENAI_API_KEY", "API key", "e Edit field"} {
		if !strings.Contains(text, want) {
			t.Fatalf("form missing %q:\\n%s", want, text)
		}
	}

	// Leaving the form again must not have written anything to the daemon:
	// one esc returns to the preset step, another to the browser.
	env.SendKey(key.Key{Code: key.KeyEscape})
	env.Flush()
	env.SendKey(key.Key{Code: key.KeyEscape})
	env.Flush()
	waitForText(env, time.Second, func(s string) bool { return strings.Contains(s, "Manage model providers") })
	mcpSettle(env)
	if len(f.created) != 0 {
		t.Fatalf("no provider should have been created yet: %+v", f.created)
	}
}

// TestProvidersCatalogPaneFilterTypingAndAdd walks the model catalog: typing a
// query that contains action-bound letters (like "s") must reach the filter
// instead of committing, enter toggles the highlighted entry, and ctrl+s adds
// the selection through the models endpoint.
func TestProvidersCatalogPaneFilterTypingAndAdd(t *testing.T) {
	f := newProviderFixture(t, testWorkspaceProviders())
	f.mu.Lock()
	f.catalog = []api.ProviderCatalogEntry{
		{ID: "claude-sonnet-5", Name: "Claude Sonnet 5", ContextLimit: 200000},
		{ID: "claude-opus-5", Name: "Claude Opus 5", ContextLimit: 200000},
	}
	f.mu.Unlock()
	reg := NewRegistry()
	if err := reg.Execute("/providers"); err != nil {
		t.Fatalf("Execute(/providers): %v", err)
	}
	env := mcpMountActiveSlot(t)
	env.SendKey(key.Key{Code: 'm', Text: "m"})
	if _, ok := waitForText(env, 2*time.Second, func(s string) bool { return strings.Contains(s, "Models · prova") }); !ok {
		t.Fatal("models pane did not open")
	}
	env.SendKey(key.Key{Code: 'c', Text: "c"})
	text, ok := waitForText(env, 2*time.Second, func(s string) bool { return strings.Contains(s, "Catalog · prova") })
	if !ok {
		t.Fatalf("catalog pane did not open:\\n%s", text)
	}

	// Typing "sonnet" (with an 's') must filter rather than commit or escape
	// the pane.
	for _, ch := range "sonnet" {
		env.SendKey(key.Key{Code: ch, Text: string(ch)})
		env.Flush()
	}
	text, ok = waitForText(env, 2*time.Second, func(s string) bool {
		return strings.Contains(s, "Claude Sonnet 5") && !strings.Contains(s, "Claude Opus 5")
	})
	if !ok {
		t.Fatalf("'sonnet' query did not reach the filter:\\n%s", text)
	}
	if !strings.Contains(text, "Catalog · prova") {
		t.Fatalf("typing 's' must not leave the catalog:\\n%s", text)
	}

	// enter toggles the highlighted entry, ctrl+s adds it to the manifest.
	env.SendKey(key.Key{Code: key.KeyEnter})
	text, ok = waitForText(env, 2*time.Second, func(s string) bool { return strings.Contains(s, "1 selected (ctrl+s adds)") })
	if !ok {
		t.Fatalf("enter did not toggle the catalog entry:\\n%s", text)
	}
	env.SendKey(key.Key{Code: 's', Mod: key.ModCtrl})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mcpSettle(env)
		_, models := fixtureModels(f, "prova")
		for _, m := range models {
			if m.ID == "claude-sonnet-5" {
				if _, ok := waitForText(env, time.Second, func(s string) bool {
					return strings.Contains(s, "Models · prova") && strings.Contains(s, "Added 1 model(s) to prova")
				}); !ok {
					t.Fatal("catalog add never landed back on the models pane with its notice")
				}
				return
			}
		}
	}
	_, models := fixtureModels(f, "prova")
	t.Fatalf("ctrl+s never added the selected model: %+v", models)
}

func TestProvidersAliasPaneLoadsAndSaves(t *testing.T) {
	f := mountProvidersCommand(t)
	env := mcpMountActiveSlot(t)

	// prova -> models pane -> f2 on the highlighted model opens the editor.
	env.SendKey(key.Key{Code: 'm', Text: "m"})
	if _, ok := waitForText(env, 2*time.Second, func(s string) bool { return strings.Contains(s, "Models · prova") }); !ok {
		t.Fatal("models pane did not open")
	}
	env.SendKey(key.Key{Code: key.KeyF2})
	text, ok := waitForText(env, 2*time.Second, func(s string) bool { return strings.Contains(s, "Model aliases · prova/Fast model") })
	if !ok {
		t.Fatalf("alias editor did not open:\n%s", text)
	}
	text, ok = waitForText(env, 2*time.Second, func(s string) bool { return strings.Contains(s, "currently provb/b-fast") })
	if !ok {
		t.Fatalf("live alias state missing from editor:\n%s", text)
	}

	// Cursor starts on @default (already owned here); move down to @fast and
	// toggle it on: saving must request the exact staged set with @default
	// routed through DefaultHere rather than Aliases. Each key needs its own
	// flush so the next press sees the re-rendered cursor.
	env.SendKey(key.Key{Code: 'j', Text: "j"})
	env.Flush()
	env.SendKey(key.Key{Code: ' ', Text: " "})
	env.Flush()
	env.SendKey(key.Key{Code: key.KeyEnter})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if calls := f.assignCalls(); len(calls) > 0 {
			call := calls[len(calls)-1]
			if call.Provider != "prova" || call.Model != "a-fast" {
				t.Fatalf("assign request = %+v, want prova/a-fast", call)
			}
			if !call.DefaultHere {
				t.Fatalf("assign request = %+v, want DefaultHere", call)
			}
			if strings.Join(call.Aliases, ",") != "fast" {
				t.Fatalf("assign aliases = %v, want [fast]", call.Aliases)
			}
			// A successful save must dismiss the editor back to the models
			// pane so the confirmation notice is actually visible.
			text, ok = waitForText(env, 2*time.Second, func(s string) bool {
				return strings.Contains(s, "Models · prova") && strings.Contains(s, "Saved aliases on prova/a-fast")
			})
			if !ok {
				t.Fatalf("editor stayed open after save:\\n%s", text)
			}
			if strings.Contains(text, "Model aliases ·") {
				t.Fatalf("alias editor still rendered after save:\\n%s", text)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("alias editor save never reached the daemon")
}

// TestProvidersEmptyStateOpensAddPane proves the first-run experience: with no
// providers configured, the empty notice's advertised a key opens the add pane
// instead of being swallowed.
func TestProvidersEmptyStateOpensAddPane(t *testing.T) {
	newProviderFixture(t, nil)
	reg := NewRegistry()
	if err := reg.Execute("/providers"); err != nil {
		t.Fatalf("Execute(/providers): %v", err)
	}
	env := mcpMountActiveSlot(t)
	text, ok := waitForText(env, 2*time.Second, func(s string) bool { return strings.Contains(s, "Press a to add one") })
	if !ok {
		t.Fatalf("empty-state notice missing:\\n%s", text)
	}

	env.SendKey(key.Key{Code: 'a', Text: "a"})
	text, ok = waitForText(env, 2*time.Second, func(s string) bool { return strings.Contains(s, "Custom provider") })
	if !ok {
		t.Fatalf("a did not open the add pane from the empty state:\\n%s", text)
	}
}

// TestModelAliasStatusLineAlwaysShowsTiersAndClamps proves the /model subtitle
// lists every standard tier (unassigned included) even when no alias resolves,
// and clamps the line to the terminal width.
func TestModelAliasStatusLineAlwaysShowsTiersAndClamps(t *testing.T) {
	line := modelAliasStatusLine(nil, 200)
	for _, want := range []string{"@default → unassigned", "@fast → unassigned", "@flagship → unassigned"} {
		if !strings.Contains(line, want) {
			t.Errorf("empty-report line lacks %q: %q", want, line)
		}
	}

	line = modelAliasStatusLine([]api.AliasAssignment{
		{Alias: "fast", Provider: "provb", Model: "b-fast"},
	}, 200)
	if !strings.Contains(line, "@fast → provb/b-fast") || !strings.Contains(line, "@default → unassigned") {
		t.Errorf("mixed line = %q", line)
	}

	clamped := modelAliasStatusLine([]api.AliasAssignment{
		{Alias: "default", Provider: "verylongprovidername", Model: "very-long-model-identifier"},
		{Alias: "fast", Provider: "anotherlongprovider", Model: "another-long-model-name"},
		{Alias: "balanced", Provider: "thirdlongprovidername", Model: "third-long-model-identifier"},
		{Alias: "smart", Provider: "fourthlongprovider", Model: "fourth-long-model-name"},
		{Alias: "flagship", Provider: "fifthlongprovidername", Model: "fifth-long-model-ident"},
	}, 40)
	if d := len([]rune(clamped)); d > 38 {
		t.Errorf("clamped line width = %d, want <= 38: %q", d, clamped)
	}
}

// TestModelPickerAliasEditorOpensOnF2 proves /model shares the same editor:
// pressing f2 on a model row swaps the picker for the alias card. The card
// edits a WARP manifest, so it opens only for models from a named provider.
func TestModelPickerAliasEditorOpensOnF2(t *testing.T) {
	picker := mountModelPicker(t, modelListing(), &api.Model{ID: "local/plain", Name: "Plain"})
	picker.awaitText(func(s string) bool { return strings.Contains(s, "GPT-5.5") })

	// A person cannot press keys in the same tick a frame paints, so each key
	// here is followed by a settle beat like the mcp view tests do.
	// The provider-less local model must not open the editor.
	picker.inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyF2}})
	picker.inst.Frame()
	time.Sleep(20 * time.Millisecond)
	picker.inst.Frame()
	if strings.Contains(testutils.SurfaceText(picker.b), "Model aliases ·") {
		t.Fatal("editor opened for a model with no owning provider")
	}

	// One down lands on openai/gpt-5.5 (group headings are not cursor rows),
	// whose row owns @fast and @default.
	picker.inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyDown}})
	picker.inst.Frame()
	picker.inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyF2}})
	text := picker.awaitText(func(s string) bool { return strings.Contains(s, "Model aliases · openai/GPT-5.5") })
	if !strings.Contains(text, "Model aliases · openai/GPT-5.5") {
		t.Fatalf("f2 did not open the shared alias editor:\n%s", text)
	}

	picker.inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEscape}})
	text = picker.awaitText(func(s string) bool { return strings.Contains(s, "GPT-5.5") })
	if !strings.Contains(text, "GPT-5.5") {
		t.Fatalf("esc did not return to the model picker:\n%s", text)
	}
}

func TestProvidersAddBrowserOAuthPreset(t *testing.T) {
	for _, tc := range []struct{ name, label string }{
		{"codex", "Codex"},
		{"claudecode", "Claude Code"},
		{"github-copilot", "GitHub Copilot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newProviderFixture(t, nil)
			f.mu.Lock()
			f.presets = []api.ProviderSummary{{Name: tc.name, DisplayName: tc.name, AuthType: "oauth2", BrowserAuthLabel: "Continue in browser"}}
			f.mu.Unlock()
			verdicts := make(chan string, 1)
			app := testutils.NewApp(t, providersAddPane(providersAddPaneProps{
				client: store.Client(), onCreated: func(_ api.WorkspaceProvider, verdict string) { verdicts <- verdict },
			}), 100, 30)
			app.WaitFor(func(out string) bool { return strings.Contains(out, tc.label) })
			app.Press(key.Key{Code: key.KeyEnter})
			frame := app.WaitFor(func(out string) bool { return strings.Contains(out, "Fixed browser OAuth preset") })
			for _, want := range []string{"Add provider · " + tc.label, "/login " + tc.name, "Workspace (.agents/providers)", "Create provider"} {
				if !strings.Contains(frame, want) {
					t.Errorf("missing %q in form:\n%s", want, frame)
				}
			}
			frame = app.Press(key.Key{Code: 'e', Text: "e"})
			for _, forbidden := range []string{"Name", "Endpoint", "Auth env var", "API key", "Edit field", "New value:"} {
				if strings.Contains(frame, forbidden) {
					t.Errorf("unexpected %q in fixed form:\n%s", forbidden, frame)
				}
			}
			app.Press(key.Key{Code: key.KeyEscape})
			app.WaitFor(func(out string) bool { return strings.Contains(out, "Pick a built-in preset") })
			app.Press(key.Key{Code: key.KeyEnter})
			app.WaitFor(func(out string) bool { return strings.Contains(out, "Fixed browser OAuth preset") })
			app.Press(key.Key{Code: key.KeyEnter})
			app.WaitFor(func(out string) bool { return strings.Contains(out, "User (~/.config/tasksmith/agents/providers)") })
			app.Press(key.Key{Code: key.KeyDown})
			app.Press(key.Key{Code: key.KeyEnter})
			mcpBackground.Wait()
			select {
			case verdict := <-verdicts:
				if verdict != " · sign in with /login "+tc.name {
					t.Errorf("verdict = %q, want sign-in instructions", verdict)
				}
			case <-time.After(time.Second):
				t.Fatalf("preset was not created:\n%s", app.Text())
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			want := api.CreateProviderRequest{Name: tc.name, Type: tc.name, Scope: "user"}
			if len(f.created) != 1 {
				t.Fatalf("create requests = %+v, want one", f.created)
			}
			got := f.created[0]
			if got.Name != want.Name || got.Type != want.Type || got.Scope != want.Scope || got.Endpoint != "" ||
				got.AuthEnv != "" || got.APIKey != "" || got.AuthType != "" || got.DefaultModel != "" || len(got.Models) != 0 {
				t.Errorf("create request = %+v, want fixed request %+v", got, want)
			}
		})
	}
}

func TestProvidersAddRegularPresetKeepsEditing(t *testing.T) {
	newProviderFixture(t, nil)
	app := testutils.NewApp(t, providersAddPane(providersAddPaneProps{client: store.Client()}), 100, 30)
	app.WaitFor(func(out string) bool { return strings.Contains(out, "OpenAI") })
	app.Press(key.Key{Code: key.KeyEnter})
	frame := app.WaitFor(func(out string) bool { return strings.Contains(out, "Edit field") })
	for _, want := range []string{"Name", "Endpoint", "Auth env var", "API key"} {
		if !strings.Contains(frame, want) {
			t.Errorf("missing regular field %q:\n%s", want, frame)
		}
	}
	app.Press(key.Key{Code: key.KeyDown})
	app.Press(key.Key{Code: 'e', Text: "e"})
	app.WaitFor(func(out string) bool { return strings.Contains(out, "New value:") })
}

// TestProviderRowStatusShadowSuffix proves "(shadow)" marks a mute only: a
// disabled workspace override over an enabled inherited manifest earns the
// suffix, while an enabled full workspace override renders as plain "Ready".
func TestProviderRowStatusShadowSuffix(t *testing.T) {
	mute := providerRowStatus(api.WorkspaceProvider{Name: "prov", Type: "openai", Status: "disabled", Shadowed: true})
	if !strings.HasPrefix(mute, "Disabled (shadow)") {
		t.Errorf("muted shadow row = %q, want Disabled (shadow) prefix", mute)
	}
	override := providerRowStatus(api.WorkspaceProvider{Name: "prov", Type: "openai", Status: "active", Shadowed: true, Enabled: true})
	if override != "Ready" {
		t.Errorf("enabled full override row = %q, want Ready without (shadow)", override)
	}
}

func TestProvidersBrowserRendersInvalidConfigStatus(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()
	newProviderFixture(t, []api.WorkspaceProvider{
		{
			Name: "healthy", Type: "openai", Scope: "workspace",
			Path:    "~/proj/.agents/providers/healthy.provider.yaml",
			Enabled: true, Status: "active", Writable: true,
			Models: []api.ProviderModelInfo{{ID: "gpt-4o", Name: "gpt-4o"}},
		},
		{
			Name: "broken", Type: "anthropic", Scope: "workspace",
			Path:    "~/proj/.agents/providers/broken.provider.yaml",
			Enabled: true, Status: "invalid_config", Writable: true,
			AuthType: "env",
			Models:   []api.ProviderModelInfo{{ID: "claude-sonnet-5-5", Name: "claude-sonnet-5-5"}},
		},
	})
	reg := NewRegistry()
	if err := reg.Execute("/providers"); err != nil {
		t.Fatalf("Execute(/providers): %v", err)
	}
	env := mcpMountActiveSlot(t)
	frame := env.DumpText()
	wantRow := "❯ ⚠ broken Invalid config 1 model"
	found := false
	for line := range strings.Lines(frame) {
		if strings.Join(strings.Fields(line), " ") == wantRow {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected %q in rendered /providers view:\n%s", wantRow, frame)
	}
}

func TestProvidersBrowserAuthHealthStatusesAndLoginKey(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()
	t.Cleanup(func() { authBackground.Wait() })

	newProviderFixture(t, []api.WorkspaceProvider{
		{
			Name: "codex", Type: "codex", Scope: "workspace",
			Path:    "~/proj/.agents/providers/codex.provider.yaml",
			Enabled: true, Status: "expired_auth", AuthType: "oauth2",
			Account:  "dev@example.test",
			Writable: true,
			Models:   []api.ProviderModelInfo{{ID: "gpt-5.3-codex", Name: "gpt-5.3-codex"}},
		},
		{
			Name: "openai", Type: "openai", Scope: "workspace",
			Path:    "~/proj/.agents/providers/openai.provider.yaml",
			Enabled: true, Status: "invalid_auth", AuthType: "bearer",
			Endpoint: "https://api.openai.com/v1",
			Writable: true,
			Models:   []api.ProviderModelInfo{{ID: "gpt-4o", Name: "gpt-4o"}},
		},
	})

	reg := NewRegistry()
	if err := reg.Execute("/providers"); err != nil {
		t.Fatalf("Execute(/providers): %v", err)
	}
	app := testutils.NewApp(t, components.Slot(), 120, 24)
	out := app.WaitFor(func(s string) bool {
		return strings.Contains(s, "Session expired") && strings.Contains(s, "Invalid key")
	})
	if !strings.Contains(out, "dev@example.test") {
		t.Fatalf("expected account email in footer for codex:\n%s", out)
	}
	if !strings.Contains(out, "Sign in") {
		t.Fatalf("expected Sign in keybinding hint:\n%s", out)
	}

	// Move to openai (non-OAuth) and press l -> notice explains no OAuth sign-in.
	app.Press(key.Key{Code: key.KeyDown})
	out = app.Press(key.Key{Code: 'l', Text: "l"})
	if !strings.Contains(out, "Provider openai does not use OAuth sign-in.") {
		t.Fatalf("expected non-OAuth notice after pressing l on openai:\n%s", out)
	}

	// Move back to codex (oauth2) and press l -> opens inline sign-in pane.
	app.Press(key.Key{Code: key.KeyUp})
	app.Press(key.Key{Code: 'l', Text: "l"})
	out = app.WaitFor(func(s string) bool {
		return strings.Contains(s, "Signing in to codex")
	})
	if !strings.Contains(out, "Back") {
		t.Fatalf("expected Back hint on embedded /providers auth pane:\n%s", out)
	}

	// Pressing Esc returns to the providers browser without closing the slot.
	app.Press(key.Key{Code: key.KeyEscape})
	app.WaitFor(func(s string) bool {
		return strings.Contains(s, "Manage model providers") && strings.Contains(s, "Session expired")
	})
}
