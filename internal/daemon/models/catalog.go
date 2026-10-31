package models

import (
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/internal/core/memdb"
	"github.com/masterkeysrd/warp"
)

// Role identifies the cognitive capability role for model resolution.
type Role string

const (
	// RoleCompletion is the role for conversational and tool-calling completion models.
	RoleCompletion Role = "completion"
	// RoleDecision is the role for probabilistic decision evaluation models.
	RoleDecision Role = "decision"
	// RoleEmbedding is the role for dense vector embedding generation models.
	RoleEmbedding Role = "embedding"
)

func (r Role) warp() warp.ModelRole { return warp.ModelRole(r) }

// WorkspaceCatalog defines the interface for querying workspace model providers
// and role resolution policies.
type WorkspaceCatalog interface {
	Providers() []*warp.ModelProvider
	ResolveModels(role warp.ModelRole, explicit ...string) []warp.ResolvedModel
}

// Catalog holds the built models of one workspace. It is safe for concurrent use.
type Catalog struct {
	cat       WorkspaceCatalog
	profiles  profile.Provider
	providers map[string]string // provider name -> provider type

	listed *memdb.DB[Model]
	built  *memdb.DB[Model]
}

// Build constructs a Catalog using cat for manifest models and profiles for fallback definitions.
func Build(cat WorkspaceCatalog, profiles profile.Provider) (*Catalog, error) {
	c := &Catalog{
		cat:       cat,
		profiles:  profiles,
		providers: make(map[string]string),
		listed:    memdb.New(func(m Model) string { return string(m.ID) }),
		built:     memdb.New(func(m Model) string { return string(m.ID) }),
	}
	if cat != nil {
		var listedModels []Model
		for _, p := range cat.Providers() {
			c.providers[p.Metadata.Name] = p.Spec.Type
			for _, m := range p.Spec.Models {
				id := Normalize(p.Metadata.Name, m.ID)
				var base *profile.Profile
				if profiles != nil {
					if prof, ok := profiles.GetProfile(string(id)); ok {
						base = &prof
					}
				}
				merged := Merge(id, p.Spec.Type, &m, base)
				listedModels = append(listedModels, merged)
			}
		}
		c.listed.Replace(listedModels...)
		c.built.Replace(listedModels...)
	}
	return c, nil
}

// Get returns the model for id, building it on first use when id names a
// model that is not listed in the manifest but is known to the provider.
func (c *Catalog) Get(id ID) (Model, bool) {
	if !id.Valid() {
		return Model{}, false
	}
	if m, ok := c.built.Snapshot().Get(string(id)); ok {
		return m.Clone(), true
	}
	var base *profile.Profile
	if c.profiles != nil {
		if p, ok := c.profiles.GetProfile(string(id)); ok {
			base = &p
		}
	}
	m := Merge(id, c.providers[id.Provider()], nil, base)
	c.built.Save(m)
	return m.Clone(), true
}

// Resolve applies warp's role/priority rules and returns the selected model.
func (c *Catalog) Resolve(role Role, explicit ...string) (Model, bool) {
	if c.cat == nil {
		return Model{}, false
	}
	for _, candidate := range c.cat.ResolveModels(role.warp(), explicit...) {
		id := ID(candidate.QualifiedID)
		if !id.Valid() {
			continue
		}
		if len(c.providers) > 0 {
			if _, ok := c.providers[id.Provider()]; !ok {
				continue
			}
		}
		if m, ok := c.Get(id); ok {
			return m, true
		}
	}
	return Model{}, false
}

// List returns every model listed in the workspace manifests.
func (c *Catalog) List() []Model {
	all := c.listed.Snapshot().All()
	out := make([]Model, len(all))
	for i, m := range all {
		out[i] = m.Clone()
	}
	return out
}
