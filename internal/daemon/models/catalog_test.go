package models

import (
	"testing"

	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/warp"
)

type stubCatalog struct {
	providers []*warp.ModelProvider
}

func (s *stubCatalog) Providers() []*warp.ModelProvider {
	return s.providers
}

func (s *stubCatalog) ResolveModels(role warp.ModelRole, explicit ...string) []warp.ResolvedModel {
	if len(explicit) > 0 && explicit[0] != "" {
		return []warp.ResolvedModel{{QualifiedID: explicit[0]}}
	}
	for _, p := range s.providers {
		if p.Spec.DefaultModel != "" {
			return []warp.ResolvedModel{{QualifiedID: Normalize(p.Metadata.Name, p.Spec.DefaultModel).String()}}
		}
		if len(p.Spec.Models) > 0 {
			return []warp.ResolvedModel{{QualifiedID: Normalize(p.Metadata.Name, p.Spec.Models[0].ID).String()}}
		}
	}
	return nil
}

func testCatalogFromProviders(providers ...*warp.ModelProvider) *stubCatalog {
	return &stubCatalog{providers: providers}
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
	cat := testCatalogFromProviders(&warp.ModelProvider{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindModelProvider,
			Metadata:   warp.Metadata{Name: "local"},
		},
		Spec: warp.ModelProviderSpec{
			Type:   "ollama",
			Models: []warp.ProviderModel{{ID: "listed"}},
		},
	})
	base := &fakeBase{}
	c, err := Build(cat, base)
	if err != nil {
		t.Fatal(err)
	}
	// Listed models are merged eagerly at Build time.
	if base.calls != 1 {
		t.Fatalf("build queried profiles %d times, want 1", base.calls)
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
	// Calling Get on pre-built listed model does not query profile again.
	if base.calls != 1 {
		t.Fatalf("listed model queried %d times, want 1", base.calls)
	}
	c.Get("local/unlisted")
	list := c.List()
	if len(list) != 1 || list[0].ID != "local/listed" {
		t.Fatalf("unexpected list: %+v", list)
	}
	// Unlisted model queries profile on demand.
	if base.calls != 2 {
		t.Fatalf("unexpected profile calls: %d, want 2", base.calls)
	}
}

func TestCatalogReturnsIndependentModels(t *testing.T) {
	cat := testCatalogFromProviders(&warp.ModelProvider{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindModelProvider,
			Metadata:   warp.Metadata{Name: "local"},
		},
		Spec: warp.ModelProviderSpec{
			Type:   "ollama",
			Models: []warp.ProviderModel{{ID: "listed"}},
		},
	})
	base := &fakeBase{profiles: map[ID]profile.Profile{"local/listed": {Capabilities: profile.Capabilities{Temperature: true, ReasoningOptions: []profile.ReasoningOption{{Type: "effort", Values: []string{"high"}}}}, Pricing: profile.Pricing{Input: 1}}}}
	c, err := Build(cat, base)
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
	cat := testCatalogFromProviders(&warp.ModelProvider{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindModelProvider,
			Metadata:   warp.Metadata{Name: "anthropic"},
		},
		Spec: warp.ModelProviderSpec{
			Type: "anthropic",
			Models: []warp.ProviderModel{
				{ID: "claude-3", Name: "Claude 3", Limits: warp.ProviderModelLimits{Context: 100000, Output: 8192}},
			},
		},
	})
	base := &fakeBase{
		profiles: map[ID]profile.Profile{
			ID("anthropic/claude-3"): {Name: "Claude 3", Limits: profile.Limits{Context: 200000, Output: 4096}, Capabilities: profile.Capabilities{Temperature: true}},
		},
	}

	c, err := Build(cat, base)
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
	cat := testCatalogFromProviders(&warp.ModelProvider{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindModelProvider,
			Metadata:   warp.Metadata{Name: "ollama"},
		},
		Spec: warp.ModelProviderSpec{Type: "ollama"},
	})
	base := &fakeBase{
		profiles: map[ID]profile.Profile{
			ID("ollama/llama3.2"): {Name: "Llama 3.2", Limits: profile.Limits{Context: 8000, Output: 1000}},
		},
	}
	c, err := Build(cat, base)
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
	cat := testCatalogFromProviders(&warp.ModelProvider{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindModelProvider,
			Metadata:   warp.Metadata{Name: "anthropic"},
		},
		Spec: warp.ModelProviderSpec{Type: "anthropic"},
	})
	base := &fakeBase{}
	c, err := Build(cat, base)
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
	cat := testCatalogFromProviders(&warp.ModelProvider{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindModelProvider,
			Metadata:   warp.Metadata{Name: "anthropic"},
		},
		Spec: warp.ModelProviderSpec{
			Type:         "anthropic",
			DefaultModel: "claude-3",
			Models:       []warp.ProviderModel{{ID: "claude-3"}},
		},
	})
	base := &fakeBase{}
	c, err := Build(cat, base)
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

	// Empty catalog: Resolve returns false.
	emptyCat := &stubCatalog{}
	cEmpty, err := Build(emptyCat, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cEmpty.Resolve(RoleCompletion); ok {
		t.Fatal("resolve should fail when resolver returns nothing")
	}
}

func TestCatalogNilBase(t *testing.T) {
	cat := testCatalogFromProviders(&warp.ModelProvider{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindModelProvider,
			Metadata:   warp.Metadata{Name: "ollama"},
		},
		Spec: warp.ModelProviderSpec{
			Type:   "ollama",
			Models: []warp.ProviderModel{{ID: "llama3.2", Limits: warp.ProviderModelLimits{Context: 8000}}},
		},
	})
	c, err := Build(cat, nil)
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
