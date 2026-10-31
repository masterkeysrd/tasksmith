package models

import (
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
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

// Catalog holds the built models of one workspace. It is safe for concurrent use.
type Catalog interface {
	// Get returns the model for id, building it on first use when id names a
	// model that is not listed in the manifest but is known to the provider.
	Get(id ID) (Model, bool)

	// Resolve applies warp's role/priority rules (agent models, workspace
	// defaults, availability) and returns the selected model.
	Resolve(role Role, explicit ...string) (Model, bool)

	// List returns every model listed in the workspace manifests.
	List() []Model
}

type catalogImpl struct {
	cat       *workspace.Catalog
	profiles  profile.Provider
	providers map[string]string
	listed    map[ID]warp.ProviderModel

	mu    sync.Mutex
	built map[ID]Model
}

var _ Catalog = (*catalogImpl)(nil)

// Build constructs a Catalog using cat for manifest models and profiles for fallback definitions.
func Build(cat *workspace.Catalog, profiles profile.Provider) (Catalog, error) {
	c := &catalogImpl{
		cat:       cat,
		profiles:  profiles,
		providers: make(map[string]string),
		listed:    make(map[ID]warp.ProviderModel),
		built:     make(map[ID]Model),
	}
	if cat != nil {
		for _, p := range cat.Providers() {
			c.providers[p.Metadata.Name] = p.Spec.Type
			for _, m := range p.Spec.Models {
				c.listed[Normalize(p.Metadata.Name, m.ID)] = m
			}
		}
	}
	return c, nil
}

func (c *catalogImpl) Get(id ID) (Model, bool) {
	if !id.Valid() {
		return Model{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if m, ok := c.built[id]; ok {
		return cloneModel(m), true
	}
	var manifest *warp.ProviderModel
	if entry, ok := c.listed[id]; ok {
		manifest = &entry
	}
	var base *profile.Profile
	if c.profiles != nil {
		if p, ok := c.profiles.GetProfile(string(id)); ok {
			base = &p
		}
	}
	m := Merge(id, c.providers[id.Provider()], manifest, base)
	c.built[id] = m
	return cloneModel(m), true
}

func cloneModel(m Model) Model {
	copyBool := func(p *bool) *bool {
		if p == nil {
			return nil
		}
		v := *p
		return &v
	}
	m.Capabilities.Completion = copyBool(m.Capabilities.Completion)
	m.Capabilities.Decision = copyBool(m.Capabilities.Decision)
	m.Capabilities.Embedding = copyBool(m.Capabilities.Embedding)
	m.Capabilities.Tools = copyBool(m.Capabilities.Tools)
	m.Capabilities.Attachment = copyBool(m.Capabilities.Attachment)
	m.Capabilities.StructuredOutput = copyBool(m.Capabilities.StructuredOutput)
	m.Capabilities.Temperature = copyBool(m.Capabilities.Temperature)
	m.Capabilities.Dimensions = slices.Clone(m.Capabilities.Dimensions)
	if m.Capabilities.Thinking != nil {
		th := make([]ThinkingOption, len(m.Capabilities.Thinking))
		for i, opt := range m.Capabilities.Thinking {
			opt.Values = slices.Clone(opt.Values)
			th[i] = opt
		}
		m.Capabilities.Thinking = th
	}
	m.Modalities.Input = slices.Clone(m.Modalities.Input)
	m.Modalities.Output = slices.Clone(m.Modalities.Output)
	if m.Pricing != nil {
		pricing := *m.Pricing
		pricing.Tiers = slices.Clone(pricing.Tiers)
		m.Pricing = &pricing
	}
	return m
}

// ResolveAgentModel resolves the effective model ID for agent.
func ResolveAgentModel(cat *workspace.Catalog, catalog Catalog, agent string, model ID) (string, ID) {
	if cat == nil {
		return agent, model
	}
	agent = strings.TrimSpace(agent)
	if agent == "" {
		agent = cat.DefaultAgentName()
	}
	model = ID(strings.TrimSpace(string(model)))
	if model == "" {
		if ag, ok := cat.GetAgent(agent); ok && len(ag.Models) > 0 {
			for _, candidate := range cat.ResolveModels(warp.ModelRoleCompletion, ag.Models...) {
				id := ID(candidate.QualifiedID)
				if id.Valid() {
					if _, available := cat.GetProvider(id.Provider()); available {
						if resolved, ok := catalog.Get(id); ok {
							return agent, resolved.ID
						}
					}
				}
			}
		}
	}
	var explicit []string
	if model != "" {
		explicit = []string{string(model)}
	}
	if resolved, ok := catalog.Resolve(RoleCompletion, explicit...); ok {
		model = resolved.ID
	}
	return agent, model
}

func (c *catalogImpl) Resolve(role Role, explicit ...string) (Model, bool) {
	if c.cat == nil {
		return Model{}, false
	}
	rm, ok := c.cat.ResolveModel(role.warp(), explicit...)
	if !ok {
		return Model{}, false
	}
	return c.Get(ID(rm.QualifiedID))
}

func (c *catalogImpl) List() []Model {
	out := make([]Model, 0, len(c.listed))
	for id := range c.listed {
		if m, ok := c.Get(id); ok {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
