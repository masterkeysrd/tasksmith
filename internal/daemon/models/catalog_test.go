package models

import (
	"testing"

	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// fakeWorkspace embeds a nil workspace.Workspace and overrides only the methods
// the catalog uses.
type fakeWorkspace struct {
	workspace.Workspace
	providers []workspace.Provider
	resolve   func(role workspace.ModelRole, explicit ...string) (*workspace.ResolvedModel, bool)
}

func (w *fakeWorkspace) Providers() []workspace.Provider { return w.providers }

func (w *fakeWorkspace) ResolveModel(role workspace.ModelRole, explicit ...string) (*workspace.ResolvedModel, bool) {
	if w.resolve != nil {
		return w.resolve(role, explicit...)
	}
	return nil, false
}

type fakeBase struct {
	profiles map[ID]profile.Profile
	calls    int
}

func (b *fakeBase) GetProfile(id string) (profile.Profile, bool) {
	b.calls++
	p, ok := b.profiles[ID(id)]
	return p, ok
}

func TestCatalogLazyAndListedOnly(t *testing.T) {
	ws := &fakeWorkspace{providers: []workspace.Provider{{Metadata: workspace.Metadata{Name: "local"}, Type: "ollama", Models: []workspace.ProviderModel{{ID: "listed"}}}}}
	base := &fakeBase{}
	c, err := Build(ws, base)
	if err != nil {
		t.Fatal(err)
	}
	if base.calls != 0 {
		t.Fatalf("build queried profiles %d times", base.calls)
	}
	if _, ok := c.Get("invalid"); ok {
		t.Fatal("invalid id accepted")
	}
	for range 2 {
		m, ok := c.Get("local/listed")
		if !ok || m.ProviderType != "ollama" {
			t.Fatalf("listed model: %+v, %v", m, ok)
		}
	}
	if base.calls != 1 {
		t.Fatalf("listed model queried %d times", base.calls)
	}
	c.Get("local/unlisted")
	list := c.List()
	if len(list) != 1 || list[0].ID != "local/listed" {
		t.Fatalf("unexpected list: %+v", list)
	}
	if base.calls != 2 {
		t.Fatalf("unexpected profile calls: %d", base.calls)
	}
}

func TestCatalogReturnsIndependentModels(t *testing.T) {
	ws := &fakeWorkspace{providers: []workspace.Provider{{Metadata: workspace.Metadata{Name: "local"}, Type: "ollama", Models: []workspace.ProviderModel{{ID: "listed"}}}}}
	base := &fakeBase{profiles: map[ID]profile.Profile{"local/listed": {Capabilities: profile.Capabilities{Temperature: true, ReasoningOptions: []profile.ReasoningOption{{Type: "effort", Values: []string{"high"}}}}, Pricing: profile.Pricing{Input: 1}}}}
	c, err := Build(ws, base)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := c.Get("local/listed")
	*m.Capabilities.Temperature = false
	m.Capabilities.Thinking[0].Values[0] = "low"
	m.Pricing.Input = 5
	next, _ := c.Get("local/listed")
	if !*next.Capabilities.Temperature || next.Capabilities.Thinking[0].Values[0] != "high" || next.Pricing.Input != 1 {
		t.Fatalf("cached model was mutated: %+v", next)
	}
}

func TestCatalogBuildListed(t *testing.T) {
	ws := &fakeWorkspace{providers: []workspace.Provider{
		{
			Metadata: workspace.Metadata{Name: "anthropic"},
			Type:     "anthropic",
			Models: []workspace.ProviderModel{
				{ID: "claude-3", Name: "Claude 3", Limits: workspace.ModelLimits{Context: 100000, Output: 8192}},
			},
		},
	}}
	base := &fakeBase{
		profiles: map[ID]profile.Profile{
			ID("anthropic/claude-3"): {Name: "Claude 3", Limits: profile.Limits{Context: 200000, Output: 4096}, Capabilities: profile.Capabilities{Temperature: true}},
		},
	}

	c, err := Build(ws, base)
	if err != nil {
		t.Fatal(err)
	}

	m, ok := c.Get(ID("anthropic/claude-3"))
	if !ok {
		t.Fatal("listed model not found")
	}
	// Manifest limits win over base.
	if m.Limits.Context != 100000 || m.Limits.Output != 8192 {
		t.Fatalf("manifest limits should win: %+v", m.Limits)
	}
	// Base fills the temperature capability.
	if m.Capabilities.Temperature == nil || !*m.Capabilities.Temperature {
		t.Fatalf("base temperature should fill: %+v", m.Capabilities.Temperature)
	}
	if m.ProviderType != "anthropic" {
		t.Fatalf("provider type: %q", m.ProviderType)
	}
	if !m.Source.Manifest || !m.Source.BuiltIn {
		t.Fatalf("source: %+v", m.Source)
	}

	list := c.List()
	if len(list) != 1 || list[0].ID != ID("anthropic/claude-3") {
		t.Fatalf("list: %+v", list)
	}
}

func TestCatalogUnlistedFromBase(t *testing.T) {
	ws := &fakeWorkspace{providers: []workspace.Provider{{Metadata: workspace.Metadata{Name: "ollama"}, Type: "ollama"}}}
	base := &fakeBase{
		profiles: map[ID]profile.Profile{
			ID("ollama/llama3.2"): {Name: "Llama 3.2", Limits: profile.Limits{Context: 8000, Output: 1000}},
		},
	}
	c, err := Build(ws, base)
	if err != nil {
		t.Fatal(err)
	}
	// Not in the manifest, but known to the provider: built on demand.
	m, ok := c.Get(ID("ollama/llama3.2"))
	if !ok {
		t.Fatal("unlisted model not found")
	}
	if m.Limits.Context != 8000 || m.Limits.Output != 1000 {
		t.Fatalf("unlisted from base: %+v", m.Limits)
	}
	if !m.Source.BuiltIn || m.Source.Manifest {
		t.Fatalf("source: %+v", m.Source)
	}
	// Memoized: a second Get returns the same value.
	if _, ok := c.Get(ID("ollama/llama3.2")); !ok {
		t.Fatal("memoized get failed")
	}
}

func TestCatalogUnknown(t *testing.T) {
	ws := &fakeWorkspace{providers: []workspace.Provider{{Metadata: workspace.Metadata{Name: "anthropic"}, Type: "anthropic"}}}
	base := &fakeBase{}
	c, err := Build(ws, base)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := c.Get(ID("anthropic/does-not-exist"))
	if !ok {
		t.Fatal("unknown model should still return")
	}
	if m.Limits.Context != 128000 || m.Limits.Output != 16384 {
		t.Fatalf("unknown fallback limits: %+v", m.Limits)
	}
	if m.Pricing != nil {
		t.Fatalf("unknown pricing should be nil")
	}
	if m.Capabilities.Completion != nil {
		t.Fatalf("unknown capabilities should be nil")
	}
	if m.ProviderType != "anthropic" {
		t.Fatalf("provider type: %q", m.ProviderType)
	}
}

func TestCatalogResolve(t *testing.T) {
	ws := &fakeWorkspace{
		providers: []workspace.Provider{
			{Metadata: workspace.Metadata{Name: "anthropic"}, Type: "anthropic", Models: []workspace.ProviderModel{{ID: "claude-3"}}},
		},
		resolve: func(role workspace.ModelRole, explicit ...string) (*workspace.ResolvedModel, bool) {
			return &workspace.ResolvedModel{ProviderName: "anthropic", ModelID: "claude-3", QualifiedID: "anthropic/claude-3"}, true
		},
	}
	base := &fakeBase{}
	c, err := Build(ws, base)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := c.Resolve(RoleCompletion)
	if !ok {
		t.Fatal("resolve failed")
	}
	if m.ID != ID("anthropic/claude-3") {
		t.Fatalf("resolved id: %s", m.ID)
	}

	// No resolver: Resolve returns false.
	ws.resolve = nil
	if _, ok := c.Resolve(RoleCompletion); ok {
		t.Fatal("resolve should fail when resolver returns nothing")
	}
}

func TestCatalogNilBase(t *testing.T) {
	ws := &fakeWorkspace{providers: []workspace.Provider{
		{Metadata: workspace.Metadata{Name: "ollama"}, Type: "ollama", Models: []workspace.ProviderModel{{ID: "llama3.2", Limits: workspace.ModelLimits{Context: 8000}}}},
	}}
	c, err := Build(ws, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := c.Get(ID("ollama/llama3.2"))
	if !ok || m.Limits.Context != 8000 {
		t.Fatalf("manifest-only build: %+v ok=%v", m, ok)
	}
	// Unknown model with nil base still returns a permissive model.
	if m, ok := c.Get(ID("ollama/unknown")); !ok || m.Limits.Context != 128000 {
		t.Fatalf("unknown with nil base: %+v ok=%v", m, ok)
	}
}
