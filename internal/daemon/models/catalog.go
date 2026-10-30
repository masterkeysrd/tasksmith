package models

import (
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
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

func (r Role) warp() workspace.ModelRole { return workspace.ModelRole(r) }

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
	ws        workspace.Workspace
	profiles  profile.Provider
	providers map[string]string
	listed    map[ID]workspace.ProviderModel

	mu    sync.Mutex
	built map[ID]Model
}

var _ Catalog = (*catalogImpl)(nil)

func Build(ws workspace.Workspace, profiles profile.Provider) (Catalog, error) {
	c := &catalogImpl{
		ws:        ws,
		profiles:  profiles,
		providers: make(map[string]string),
		listed:    make(map[ID]workspace.ProviderModel),
		built:     make(map[ID]Model),
	}
	for _, p := range ws.Providers() {
		c.providers[p.Name] = p.Type
		for _, m := range p.Models {
			c.listed[Normalize(p.Name, m.ID)] = m
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
	var manifest *workspace.ProviderModel
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
	c := &m.Capabilities
	c.Completion = copyBool(c.Completion)
	c.Decision = copyBool(c.Decision)
	c.Embedding = copyBool(c.Embedding)
	c.Tools = copyBool(c.Tools)
	c.Attachment = copyBool(c.Attachment)
	c.StructuredOutput = copyBool(c.StructuredOutput)
	c.Temperature = copyBool(c.Temperature)
	c.Dimensions = slices.Clone(c.Dimensions)
	c.Thinking = slices.Clone(c.Thinking)
	for i := range c.Thinking {
		c.Thinking[i].Values = slices.Clone(c.Thinking[i].Values)
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

func ResolveAgentModel(ws workspace.Workspace, catalog Catalog, agent string, model ID) (string, ID) {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		agent = ws.DefaultAgentName()
	}
	model = ID(strings.TrimSpace(string(model)))
	if model == "" {
		if ag, ok := ws.GetAgent(agent); ok && ag != nil && len(ag.Models) > 0 {
			for _, candidate := range ws.ResolveModels(workspace.RoleCompletion, ag.Models...) {
				id := ID(candidate)
				if id.Valid() {
					if _, available := ws.GetProvider(id.Provider()); available {
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
	rm, ok := c.ws.ResolveModel(role.warp(), explicit...)
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
