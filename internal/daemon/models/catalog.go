package models

import (
	"slices"
	"strings"

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
	WorkspaceSpec() *warp.Workspace
}

// Catalog holds the built models of one workspace. It is safe for concurrent use.
type Catalog struct {
	cat       WorkspaceCatalog
	profiles  profile.Provider
	providers map[string]string // provider name -> provider type

	listed *memdb.DB[Model]
	built  *memdb.DB[Model]
	// fold maps a lowercased qualified ID or WARP short name to the canonical
	// ID of a listed model, so references differing only in casing resolve to
	// the declared model instead of synthesizing an unlisted one.
	fold map[string]ID
}

// Build constructs a Catalog using cat for manifest models and profiles for fallback definitions.
func Build(cat WorkspaceCatalog, profiles profile.Provider) (*Catalog, error) {
	c := &Catalog{
		cat:       cat,
		profiles:  profiles,
		providers: make(map[string]string),
		listed:    memdb.New(func(m Model) string { return string(m.ID) }),
		built:     memdb.New(func(m Model) string { return string(m.ID) }),
		fold:      make(map[string]ID),
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
				c.fold[strings.ToLower(string(id))] = id
			}
		}
		// Short names are indexed after canonical IDs so an ID always wins a
		// collision with another model's short name.
		for _, p := range cat.Providers() {
			for _, m := range p.Spec.Models {
				if m.Name == "" || m.Name == m.ID {
					continue
				}
				key := strings.ToLower(Normalize(p.Metadata.Name, m.Name).String())
				if _, exists := c.fold[key]; !exists {
					c.fold[key] = Normalize(p.Metadata.Name, m.ID)
				}
			}
		}
		c.applyWorkspaceAliases(listedModels)
		c.listed.Replace(listedModels...)
		c.built.Replace(listedModels...)
	}
	return c, nil
}

// applyWorkspaceAliases attaches workspace.spec.modelAliases to the models they
// resolve to. Resolution is delegated to the workspace catalog so chained
// aliases ("@fast") and bare targets follow the same rules as everywhere else.
func (c *Catalog) applyWorkspaceAliases(models []Model) {
	ws := c.cat.WorkspaceSpec()
	if ws == nil || ws.Def == nil || len(ws.Def.Spec.ModelAliases) == 0 {
		return
	}
	byID := make(map[ID]int, len(models))
	for i, m := range models {
		byID[m.ID] = i
	}
	names := make([]string, 0, len(ws.Def.Spec.ModelAliases))
	for name := range ws.Def.Spec.ModelAliases {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		alias := strings.TrimPrefix(name, "@")
		if alias == "" {
			continue
		}
		id, ok := c.resolveAliasID(ID("@" + alias))
		if !ok {
			continue
		}
		i, ok := byID[id]
		if !ok {
			continue
		}
		models[i].Aliases = appendUnique(models[i].Aliases, alias)
	}
}

// Get returns the model for id, building it on first use when id names a
// model that is not listed in the manifest but is known to the provider.
//
// Aliases and shorthands ("@fast", "@default", "provider/@alias", and
// workspace.spec.modelAliases) are canonicalized by delegating to the workspace
// catalog's ResolveModels rather than re-implementing alias rules here.
//
// A reference naming a listed model — by canonical id, WARP short name, or any
// casing of either — always resolves to that declared model, so its limits,
// capabilities, and pricing are never discarded.
func (c *Catalog) Get(id ID) (Model, bool) {
	// Canonicalize aliases/shorthands through warp before the validity check and
	// the built lookup so bare aliases (which are not valid IDs) still resolve.
	if c.cat != nil && c.shouldResolveAlias(id) {
		if canonical, ok := c.resolveAliasID(id); ok {
			id = canonical
		}
	}

	if !id.Valid() {
		return Model{}, false
	}
	if m, ok := c.built.Snapshot().Get(string(id)); ok {
		return m.Clone(), true
	}
	// A reference that differs from a listed model only in casing would
	// otherwise synthesize an unlisted model and discard every declared limit,
	// capability, and price.
	if canonical, ok := c.fold[strings.ToLower(string(id))]; ok {
		if m, ok := c.built.Snapshot().Get(string(canonical)); ok {
			return m.Clone(), true
		}
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

// shouldResolveAlias reports whether id is an alias/shorthand or an unknown id
// that should be canonicalized through the workspace catalog's ResolveModels
// before a direct built lookup.
func (c *Catalog) shouldResolveAlias(id ID) bool {
	if !id.Valid() {
		return true
	}
	if strings.HasPrefix(string(id), "@") || strings.HasPrefix(id.Name(), "@") {
		return true
	}
	if _, ok := c.built.Snapshot().Get(string(id)); !ok {
		return true
	}
	return false
}

// resolveAliasID delegates to the workspace catalog to canonicalize an
// alias/shorthand id into a provider/model-id qualified ID, honoring
// workspace.spec.modelAliases.
//
// An explicit alias reference ("@fast", "@default", or "provider/@alias") is
// canonicalized to its target QualifiedID even when that target is not a listed
// model. A bare reference without "@" must match a declared model definition —
// a candidate whose Model is non-nil — because WARP qualifies any bare string
// against defaultProvider as a pass-through (Model == nil); accepting that would
// synthesize an unlisted model, so unknown bare IDs fall through to the
// validity check and are rejected.
func (c *Catalog) resolveAliasID(id ID) (ID, bool) {
	s := string(id)
	isAlias := strings.HasPrefix(s, "@") || strings.Contains(s, "/@")
	for _, role := range []warp.ModelRole{warp.ModelRoleCompletion, warp.ModelRoleDecision, warp.ModelRoleEmbedding} {
		for _, candidate := range c.cat.ResolveModels(role, s) {
			if candidate.QualifiedID == "" {
				continue
			}
			if !isAlias && candidate.Model == nil {
				continue
			}
			qid := ID(candidate.QualifiedID)
			if !qid.Valid() {
				continue
			}
			if len(c.providers) > 0 {
				if _, ok := c.providers[qid.Provider()]; !ok {
					continue
				}
			}
			return qid, true
		}
	}
	return ID(""), false
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
