package workspace

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/memdb"
	"github.com/masterkeysrd/tasksmith/internal/core/tokenizer"
	"github.com/masterkeysrd/warp"
)

var (
	reProjectContext = regexp.MustCompile(`(?s)<project_context\b[^>]*>.*?</project_context>`)
	reSkillsSection  = regexp.MustCompile(`(?s)<skills_and_specialized_knowledge\b[^>]*>.*?</skills_and_specialized_knowledge>`)
	reSubagentsSec   = regexp.MustCompile(`(?s)<subagents_and_delegation\b[^>]*>.*?</subagents_and_delegation>`)
)

// AgentSnapshot pairs an API Agent presentation with its defining warp.Agent
// and resolved inheritance tree.
type AgentSnapshot struct {
	api.Agent
	Def      *warp.Agent
	Resolved *warp.ResolvedAgent
}

// SkillSnapshot pairs an API Skill presentation with its defining warp.Skill.
type SkillSnapshot struct {
	api.Skill
	Def *warp.Skill
}

// CommandSnapshot pairs an API Command presentation with its defining warp.Command.
type CommandSnapshot struct {
	api.Command
	Def *warp.Command
}

// Catalog acts as an in-memory query and resolution proxy over a warp.Registry
// (and scoped registry when cwd is inside a subproject), exposing native warp
// resource definitions pre-materialized in lock-free point-in-time memdb stores.
type Catalog struct {
	registry *warp.Registry
	cwd      string
	rootDir  string

	workspaceTokens int
	toolTokens      map[string]int

	agents    *memdb.DB[AgentSnapshot]
	skills    *memdb.DB[SkillSnapshot]
	commands  *memdb.DB[CommandSnapshot]
	tools     *memdb.DB[*warp.Tool]
	providers *memdb.DB[*warp.ModelProvider]
}

// NewCatalog constructs a Catalog proxying reg for the given cwd and rootDir,
// pre-materializing all effective resources in lock-free memdb stores.
func NewCatalog(reg *warp.Registry, cwd, rootDir string) *Catalog {
	c := &Catalog{
		registry:   reg,
		cwd:        cwd,
		rootDir:    rootDir,
		toolTokens: make(map[string]int),
		agents:     memdb.New(func(a AgentSnapshot) string { return a.Name }),
		skills:     memdb.New(func(s SkillSnapshot) string { return s.Name }),
		commands:   memdb.New(func(cmd CommandSnapshot) string { return cmd.Name }),
		tools:      memdb.New(func(t *warp.Tool) string { return t.Metadata.Name }),
		providers:  memdb.New(func(p *warp.ModelProvider) string { return p.Metadata.Name }),
	}
	c.populate()
	return c
}

// Registry returns the underlying warp.Registry instance.
func (c *Catalog) Registry() *warp.Registry {
	return c.registry
}

// WorkspaceSpec returns the workspace specification resource from WORKSPACE.md.
func (c *Catalog) WorkspaceSpec() *warp.Workspace {
	if c.registry == nil {
		return nil
	}
	return c.registry.WorkspaceSpec()
}

// Project returns the project corresponding to cwd, if cwd is within a project scope.
func (c *Catalog) Project() *warp.Project {
	if c.registry == nil {
		return nil
	}
	if proj, ok := c.registry.ProjectFromPath(c.cwd); ok {
		return proj
	}
	return nil
}

// Warnings returns non-fatal diagnostic warnings collected during registry loading.
func (c *Catalog) Warnings() []string {
	if c.registry == nil {
		return nil
	}
	return cloneSlice(c.registry.Warnings())
}

// DefaultAgentName returns the workspace's configured default agent name, or "main".
func (c *Catalog) DefaultAgentName() string {
	if c.registry != nil {
		if ws := c.registry.WorkspaceSpec(); ws != nil && ws.Def != nil && ws.Def.Spec.DefaultAgent != "" {
			return ws.Def.Spec.DefaultAgent
		}
	}
	return AgentNameMain
}

// Policies returns the workspace-level policies declared in WORKSPACE.md.
func (c *Catalog) Policies() *warp.Policies {
	if c.registry == nil {
		return nil
	}
	ws := c.registry.WorkspaceSpec()
	if ws == nil || ws.Def == nil {
		return nil
	}
	return ws.Def.Spec.Policies
}

func (c *Catalog) resolver() warp.Resolver {
	if c.registry == nil {
		return nil
	}
	if proj, ok := c.registry.ProjectFromPath(c.cwd); ok {
		return c.registry.Project(proj.Name)
	}
	return c.registry
}

func (c *Catalog) resolveByKind(kind warp.Kind, name string) (warp.Resource, bool) {
	resolver := c.resolver()
	if resolver == nil || strings.TrimSpace(name) == "" {
		return nil, false
	}

	if strings.Contains(name, "/") {
		if res, ok := resolver.ResolveResource(name); ok && res.GetKind() == kind {
			return res, true
		}
	}

	resources := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{kind},
		Effective: true,
	})
	for _, r := range resources {
		if r.GetName() == name || r.QualifiedName() == name {
			return r, true
		}
	}
	return nil, false
}

func (c *Catalog) resolveWarpAgent(name string) (*warp.Agent, *warp.ResolvedAgent, bool) {
	res, ok := c.resolveByKind(warp.KindAgent, name)
	if !ok {
		return nil, nil, false
	}
	baseAgent, ok := res.(*warp.Agent)
	if !ok {
		return nil, nil, false
	}

	resolver := c.resolver()
	var resolved *warp.ResolvedAgent
	switch r := resolver.(type) {
	case *warp.ScopedRegistry:
		resolved, _ = r.ResolveAgent(baseAgent.QualifiedName())
	case *warp.Registry:
		resolved, _ = r.ResolveAgent(baseAgent.QualifiedName())
	}
	if resolved != nil && resolved.Agent != nil {
		return resolved.Agent, resolved, true
	}
	return baseAgent, nil, true
}

func (c *Catalog) populate() {
	if c.registry == nil {
		return
	}
	resolver := c.resolver()
	if resolver == nil {
		return
	}

	// 0. Workspace prompt tokens pre-computed once
	if wsSpec := c.registry.WorkspaceSpec(); wsSpec != nil {
		wsPrompt, _ := wsSpec.Render(&warp.WorkspaceRenderOptions{})
		c.workspaceTokens = tokenizer.Estimate(wsPrompt)
	}

	// 1. Tools: Standalone tools + inline toolkit tools
	var toolsList []*warp.Tool
	rawTools := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindTool},
		Effective: true,
	})
	seenTools := make(map[string]bool, len(rawTools))
	for _, r := range rawTools {
		if t, ok := r.(*warp.Tool); ok {
			toolsList = append(toolsList, t)
			seenTools[t.Metadata.Name] = true
		}
	}
	toolkits := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindToolkit},
		Effective: true,
	})
	for _, r := range toolkits {
		tk, ok := r.(*warp.Toolkit)
		if !ok {
			continue
		}
		for _, item := range tk.Spec.Tools {
			if item.Ref == "" && item.Name != "" && !seenTools[item.Name] {
				seenTools[item.Name] = true
				tSpec := item.ToolSpec
				if tSpec.Description == "" {
					tSpec.Description = item.Description
				}
				toolsList = append(toolsList, &warp.Tool{
					BaseResource: warp.BaseResource{
						APIVersion: tk.APIVersion,
						Kind:       warp.KindTool,
						Namespace:  tk.Namespace,
						Metadata: warp.Metadata{
							Name:        item.Name,
							Description: item.Description,
						},
					},
					Spec: tSpec,
				})
			}
		}
	}
	c.toolTokens = make(map[string]int, len(toolsList))
	for _, t := range toolsList {
		toolTokens := tokenizer.Estimate(t.Metadata.Name) +
			tokenizer.Estimate(t.Spec.Description) +
			tokenizer.Estimate(t.Spec.Instructions)
		if len(t.Spec.InputSchema) > 0 {
			if data, err := json.Marshal(t.Spec.InputSchema); err == nil {
				toolTokens += tokenizer.Estimate(string(data))
			}
		}
		c.toolTokens[t.Metadata.Name] = toolTokens
	}
	c.tools.Replace(toolsList...)

	// 2. Providers
	var provList []*warp.ModelProvider
	rawProviders := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindModelProvider},
		Effective: true,
	})
	for _, r := range rawProviders {
		if mp, ok := r.(*warp.ModelProvider); ok {
			provList = append(provList, mp)
		}
	}
	c.providers.Replace(provList...)

	// 3. Commands
	var cmdList []CommandSnapshot
	rawCommands := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindCommand},
		Effective: true,
	})
	for _, r := range rawCommands {
		if cmd, ok := r.(*warp.Command); ok {
			cmdList = append(cmdList, CommandSnapshot{
				Command: api.Command{
					Name:         cmd.Metadata.Name,
					DisplayName:  cmd.Metadata.DisplayName,
					Description:  cmd.Metadata.Description,
					ArgumentHint: cmd.Spec.ArgumentHint,
					Arguments:    ToAPIArguments(cmd.Spec.Arguments),
					Models:       slices.Clone(cmd.Spec.Models),
					Tools:        slices.Clone(cmd.Spec.Tools),
					Namespace:    cmd.Namespace,
				},
				Def: cmd,
			})
		}
	}
	c.commands.Replace(cmdList...)

	// 4. Skills
	var skillList []SkillSnapshot
	rawSkills := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindSkill},
		Effective: true,
	})
	for _, r := range rawSkills {
		if sk, ok := r.(*warp.Skill); ok {
			scopeNamespace := c.ScopeNamespace(sk.BaseResource)
			skillList = append(skillList, SkillSnapshot{
				Skill: api.Skill{
					Name:         sk.Metadata.Name,
					DisplayName:  sk.Metadata.DisplayName,
					Description:  sk.Metadata.Description,
					UseWhen:      sk.Spec.UseWhen,
					Keywords:     slices.Clone(sk.Spec.Keywords),
					ArgumentHint: sk.Spec.ArgumentHint,
					Arguments:    ToAPIArguments(sk.Spec.Arguments),
					Namespace:    scopeNamespace,
					Scope:        FormatSkillScope(scopeNamespace),
					Instructions: sk.Spec.Instructions,
					Tokens:       tokenizer.Estimate(strings.TrimSpace(sk.Spec.Instructions)),
				},
				Def: sk,
			})
		}
	}
	c.skills.Replace(skillList...)

	// 5. Agents
	var agentList []AgentSnapshot
	rawAgents := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindAgent},
		Effective: true,
	})
	for _, r := range rawAgents {
		if ag, ok := r.(*warp.Agent); ok {
			baseAgent, resolved, _ := c.resolveWarpAgent(ag.Metadata.Name)
			if baseAgent == nil {
				baseAgent = ag
			}
			ctxBreakdown := c.calculateAgentContext(baseAgent, resolved)
			agentList = append(agentList, AgentSnapshot{
				Agent: api.Agent{
					Name:        baseAgent.Metadata.Name,
					DisplayName: baseAgent.Metadata.DisplayName,
					Description: baseAgent.Metadata.Description,
					Models:      slices.Clone(baseAgent.Spec.Models),
					Thinking:    ParseThinking(baseAgent.Spec.Thinking),
					Context:     ctxBreakdown,
				},
				Def:      baseAgent,
				Resolved: resolved,
			})
		}
	}
	c.agents.Replace(agentList...)
}

func (c *Catalog) calculateAgentContext(ag *warp.Agent, resolved *warp.ResolvedAgent) api.ContextBreakdown {
	contextFiles := c.workspaceTokens

	if ag == nil {
		return api.ContextBreakdown{ContextFiles: contextFiles}
	}

	globals := map[string]any{
		"HasTool": func(toolName string) bool {
			return true
		},
	}
	agentPrompt, _ := ag.Render(&warp.AgentRenderOptions{
		Workspace: c.WorkspaceSpec(),
		Project:   c.Project(),
		Resolved:  resolved,
		Globals:   globals,
	})

	var skills, subagents int

	projMatches := reProjectContext.FindAllString(agentPrompt, -1)
	for _, m := range projMatches {
		contextFiles += tokenizer.Estimate(m)
	}
	agentPrompt = reProjectContext.ReplaceAllString(agentPrompt, "")

	skillMatches := reSkillsSection.FindAllString(agentPrompt, -1)
	for _, m := range skillMatches {
		skills += tokenizer.Estimate(m)
	}
	agentPrompt = reSkillsSection.ReplaceAllString(agentPrompt, "")

	subagentMatches := reSubagentsSec.FindAllString(agentPrompt, -1)
	for _, m := range subagentMatches {
		subagents += tokenizer.Estimate(m)
	}
	agentPrompt = reSubagentsSec.ReplaceAllString(agentPrompt, "")

	systemPrompt := tokenizer.Estimate(strings.TrimSpace(agentPrompt))

	var systemTools, customTools, mcpTools int
	toolsList := c.Tools()
	for _, t := range toolsList {
		if resolved != nil && len(resolved.Tools) > 0 {
			var found bool
			for _, rt := range resolved.Tools {
				if rt.Metadata.Name == t.Metadata.Name {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		toolTokens, ok := c.toolTokens[t.Metadata.Name]
		if !ok {
			toolTokens = tokenizer.Estimate(t.Metadata.Name) +
				tokenizer.Estimate(t.Spec.Description) +
				tokenizer.Estimate(t.Spec.Instructions)
			if len(t.Spec.InputSchema) > 0 {
				if data, err := json.Marshal(t.Spec.InputSchema); err == nil {
					toolTokens += tokenizer.Estimate(string(data))
				}
			}
		}

		ns := strings.ToLower(t.Namespace)
		if ns == warp.NamespaceSystem || ns == "system" || ns == "" {
			systemTools += toolTokens
		} else if ns == "mcp" || strings.HasPrefix(t.Metadata.Name, "mcp__") {
			mcpTools += toolTokens
		} else {
			customTools += toolTokens
		}
	}

	return api.ContextBreakdown{
		SystemPrompt: systemPrompt,
		ContextFiles: contextFiles,
		SystemTools:  systemTools,
		CustomTools:  customTools,
		MCPTools:     mcpTools,
		Skills:       skills,
		Subagents:    subagents,
	}
}

// Agents returns all effective agent snapshots in scope, sorted by metadata name.
func (c *Catalog) Agents() []AgentSnapshot {
	return c.agents.Snapshot().All()
}

// GetAgent resolves an agent snapshot by name or qualified name.
func (c *Catalog) GetAgent(name string) (AgentSnapshot, bool) {
	if snap, ok := c.agents.Snapshot().Get(name); ok {
		return snap, true
	}
	if strings.Contains(name, "/") {
		for _, s := range c.agents.Snapshot().All() {
			if s.Def != nil && (s.Def.QualifiedName() == name || s.Def.Metadata.Name == name) {
				return s, true
			}
		}
		if res, ok := c.resolveByKind(warp.KindAgent, name); ok {
			if ag, ok := res.(*warp.Agent); ok {
				baseAgent, resolved, _ := c.resolveWarpAgent(ag.QualifiedName())
				if baseAgent == nil {
					baseAgent = ag
				}
				ctxBreakdown := c.calculateAgentContext(baseAgent, resolved)
				return AgentSnapshot{
					Agent: api.Agent{
						Name:        baseAgent.Metadata.Name,
						DisplayName: baseAgent.Metadata.DisplayName,
						Description: baseAgent.Metadata.Description,
						Models:      slices.Clone(baseAgent.Spec.Models),
						Thinking:    ParseThinking(baseAgent.Spec.Thinking),
						Context:     ctxBreakdown,
					},
					Def:      baseAgent,
					Resolved: resolved,
				}, true
			}
		}
	}
	return AgentSnapshot{}, false
}

// ResolveAgent resolves the fully merged agent definition and capabilities for name.
func (c *Catalog) ResolveAgent(name string) (*warp.ResolvedAgent, error) {
	if name == "" {
		name = c.DefaultAgentName()
	}
	snap, ok := c.GetAgent(name)
	if !ok {
		return nil, fmt.Errorf("workspace: agent %q not found", name)
	}
	if snap.Resolved != nil {
		return snap.Resolved, nil
	}
	return &warp.ResolvedAgent{
		Agent: snap.Def,
	}, nil
}

// Skills returns all effective skill snapshots in scope, sorted by metadata name.
func (c *Catalog) Skills() []SkillSnapshot {
	return c.skills.Snapshot().All()
}

// GetSkill resolves a skill snapshot by name or qualified name.
func (c *Catalog) GetSkill(name string) (SkillSnapshot, bool) {
	if snap, ok := c.skills.Snapshot().Get(name); ok {
		return snap, true
	}
	if strings.Contains(name, "/") {
		for _, s := range c.skills.Snapshot().All() {
			if s.Def != nil && s.Def.QualifiedName() == name {
				return s, true
			}
		}
		if res, ok := c.resolveByKind(warp.KindSkill, name); ok {
			if sk, ok := res.(*warp.Skill); ok {
				scopeNamespace := c.ScopeNamespace(sk.BaseResource)
				return SkillSnapshot{
					Skill: api.Skill{
						Name:         sk.Metadata.Name,
						DisplayName:  sk.Metadata.DisplayName,
						Description:  sk.Metadata.Description,
						UseWhen:      sk.Spec.UseWhen,
						Keywords:     slices.Clone(sk.Spec.Keywords),
						ArgumentHint: sk.Spec.ArgumentHint,
						Arguments:    ToAPIArguments(sk.Spec.Arguments),
						Namespace:    scopeNamespace,
						Scope:        FormatSkillScope(scopeNamespace),
						Instructions: sk.Spec.Instructions,
						Tokens:       tokenizer.Estimate(strings.TrimSpace(sk.Spec.Instructions)),
					},
					Def: sk,
				}, true
			}
		}
	}
	return SkillSnapshot{}, false
}

// Commands returns all effective command snapshots in scope, sorted by metadata name.
func (c *Catalog) Commands() []CommandSnapshot {
	return c.commands.Snapshot().All()
}

// GetCommand resolves a command snapshot by name or qualified name.
func (c *Catalog) GetCommand(name string) (CommandSnapshot, bool) {
	if snap, ok := c.commands.Snapshot().Get(name); ok {
		return snap, true
	}
	if strings.Contains(name, "/") {
		for _, s := range c.commands.Snapshot().All() {
			if s.Def != nil && s.Def.QualifiedName() == name {
				return s, true
			}
		}
		if res, ok := c.resolveByKind(warp.KindCommand, name); ok {
			if cmd, ok := res.(*warp.Command); ok {
				return CommandSnapshot{
					Command: api.Command{
						Name:         cmd.Metadata.Name,
						DisplayName:  cmd.Metadata.DisplayName,
						Description:  cmd.Metadata.Description,
						ArgumentHint: cmd.Spec.ArgumentHint,
						Arguments:    ToAPIArguments(cmd.Spec.Arguments),
						Models:       slices.Clone(cmd.Spec.Models),
						Tools:        slices.Clone(cmd.Spec.Tools),
						Namespace:    cmd.Namespace,
					},
					Def: cmd,
				}, true
			}
		}
	}
	return CommandSnapshot{}, false
}

// Tools returns all effective tools in scope, sorted by metadata name.
func (c *Catalog) Tools() []*warp.Tool {
	return c.tools.Snapshot().All()
}

// GetTool resolves an effective tool by name or qualified name.
func (c *Catalog) GetTool(name string) (*warp.Tool, bool) {
	if t, ok := c.tools.Snapshot().Get(name); ok {
		return t, true
	}
	if strings.Contains(name, "/") {
		for _, t := range c.tools.Snapshot().All() {
			if t.QualifiedName() == name {
				return t, true
			}
		}
		if res, ok := c.resolveByKind(warp.KindTool, name); ok {
			if t, ok := res.(*warp.Tool); ok {
				return t, true
			}
		}
	}
	return nil, false
}

// Providers returns all effective model providers in scope, sorted by metadata name.
func (c *Catalog) Providers() []*warp.ModelProvider {
	return c.providers.Snapshot().All()
}

// GetProvider resolves an effective model provider by name or qualified name.
func (c *Catalog) GetProvider(name string) (*warp.ModelProvider, bool) {
	if p, ok := c.providers.Snapshot().Get(name); ok {
		return p, true
	}
	if strings.Contains(name, "/") {
		for _, p := range c.providers.Snapshot().All() {
			if p.QualifiedName() == name {
				return p, true
			}
		}
		if res, ok := c.resolveByKind(warp.KindModelProvider, name); ok {
			if p, ok := res.(*warp.ModelProvider); ok {
				return p, true
			}
		}
	}
	return nil, false
}

// ResolveModels returns the prioritized list of resolved models for role.
func (c *Catalog) ResolveModels(role warp.ModelRole, explicit ...string) []warp.ResolvedModel {
	resolver := c.resolver()
	if resolver == nil {
		return nil
	}
	return resolver.ResolveModels(role, explicit...)
}

// ResolveModel resolves the highest-priority model reference for role and explicit overrides.
func (c *Catalog) ResolveModel(role warp.ModelRole, explicit ...string) (*warp.ResolvedModel, bool) {
	resolved := c.ResolveModels(role, explicit...)
	if len(resolved) == 0 {
		return nil, false
	}
	return &resolved[0], true
}

// AgentContext calculates token breakdown metrics for the named agent.
func (c *Catalog) AgentContext(name string) api.ContextBreakdown {
	if name == "" {
		name = c.DefaultAgentName()
	}
	if snap, ok := c.GetAgent(name); ok {
		return snap.Context
	}
	return api.ContextBreakdown{ContextFiles: c.workspaceTokens}
}

// ScopeNamespace normalizes a project-slug namespace to warp.NamespaceLocal
// when reg.GetProject(ns) matches, for display and API conversion.
func (c *Catalog) ScopeNamespace(b warp.BaseResource) string {
	ns := b.Namespace
	if c.registry != nil && ns != "" && ns != warp.NamespaceSystem && ns != warp.NamespaceUser && ns != warp.NamespaceWorkspace {
		if _, isProj := c.registry.GetProject(ns); isProj {
			return warp.NamespaceLocal
		}
	}
	return ns
}

// ParseThinking extracts an api.ThinkingSetting from an agent's thinking configuration map.
func ParseThinking(thinking map[string]any) *api.ThinkingSetting {
	if thinking == nil {
		return nil
	}
	if effort, ok := thinking["effort"].(string); ok && effort != "" {
		return &api.ThinkingSetting{Type: "effort", Effort: effort}
	}
	if value, err := strconv.Atoi(fmt.Sprint(thinking["budget_tokens"])); err == nil && value > 0 {
		return &api.ThinkingSetting{Type: "budget", Budget: value}
	}
	if value, ok := thinking["enabled"].(bool); ok {
		return &api.ThinkingSetting{Type: "toggle", Enabled: value}
	}
	if typ, _ := thinking["type"].(string); typ == "effort" {
		if effort, ok := thinking["default"].(string); ok && effort != "" {
			return &api.ThinkingSetting{Type: "effort", Effort: effort}
		}
	}
	return nil
}

// ToAPIArguments converts warp arguments to public API arguments.
func ToAPIArguments(args []warp.Argument) []api.Argument {
	if args == nil {
		return nil
	}
	out := make([]api.Argument, len(args))
	for i, a := range args {
		out[i] = api.Argument{
			Name:        a.Name,
			Description: a.Description,
			Required:    a.Required,
			Default:     a.Default,
		}
	}
	return out
}

// FormatSkillScope formats a namespace into its user-facing scope title.
func FormatSkillScope(ns string) string {
	switch strings.ToLower(strings.TrimSpace(ns)) {
	case "workspace":
		return "Workspace"
	case "local", "project":
		return "Project"
	case "user":
		return "User"
	case "system", "builtin", "":
		return "Builtin"
	default:
		s := strings.TrimSpace(ns)
		if len(s) > 0 {
			return strings.ToUpper(s[:1]) + s[1:]
		}
		return "Builtin"
	}
}
