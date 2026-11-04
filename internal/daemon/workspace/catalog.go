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

	// validationWarnings holds non-fatal diagnostic messages produced by
	// registry.Validate() (structural correctness and cross-reference checks
	// across WORKSPACE.md and all loaded resources).
	validationWarnings []string

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

// Projects returns all projects mapped into the workspace, sorted by project
// name. It returns nil when the catalog has no registry.
func (c *Catalog) Projects() []*warp.Project {
	if c.registry == nil {
		return nil
	}
	return c.registry.ListProjects()
}

// Plugins returns a deep-copied slice of the plugins declared in WORKSPACE.md
// (spec.plugins). It returns nil when the workspace spec or its definition is
// unavailable so callers cannot mutate the catalog's internal state.
func (c *Catalog) Plugins() []warp.WorkspacePlugin {
	ws := c.WorkspaceSpec()
	if ws == nil || ws.Def == nil {
		return nil
	}
	plugins := ws.Def.Spec.Plugins
	if len(plugins) == 0 {
		return nil
	}
	out := make([]warp.WorkspacePlugin, len(plugins))
	for i := range plugins {
		out[i] = *plugins[i].DeepCopy()
	}
	return out
}

// Warnings returns non-fatal diagnostic warnings collected during registry
// loading and structural validation. The returned slice is a defensive copy so
// callers cannot mutate the catalog's internal state.
func (c *Catalog) Warnings() []string {
	combined := make([]string, 0, len(c.validationWarnings))
	if c.registry != nil {
		combined = append(combined, c.registry.Warnings()...)
	}
	combined = append(combined, c.validationWarnings...)
	return combined
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

	// 1. Tools
	var toolsList []*warp.Tool
	rawTools := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindTool},
		Effective: true,
	})
	for _, r := range rawTools {
		if t, ok := r.(*warp.Tool); ok {
			toolsList = append(toolsList, t)
		}
	}
	c.toolTokens = make(map[string]int, len(toolsList))
	for _, t := range toolsList {
		toolTokens := tokenizer.Estimate(t.Metadata.Name) +
			tokenizer.Estimate(t.Metadata.Description) +
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
					Agent:        cmd.Spec.Agent,
					Context:      string(cmd.Spec.Context),
					ArgumentHint: cmd.Spec.ArgumentHint,
					Arguments:    ToAPIArguments(cmd.Spec.Arguments),
					Models:       slices.Clone(cmd.Spec.Models),
					Tools:        slices.Clone(cmd.Spec.Tools),
					Skills:       slices.Clone(cmd.Spec.Skills),
					Rules:        slices.Clone(cmd.Spec.Rules),
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
					Tools:        slices.Clone(sk.Spec.Tools),
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
			// Emit a diagnostic when the agent loads more than the recommended
			// number of eager tools on the first request.
			if partition := c.PartitionAgentTools(resolved); len(partition.Eager) > MaxRecommendedEagerTools {
				c.validationWarnings = append(c.validationWarnings, fmt.Sprintf("Agent %q loads %d tools on the first request (recommended maximum is %d); consider reducing spec.tools to use deferred loading", baseAgent.Metadata.Name, len(partition.Eager), MaxRecommendedEagerTools))
			}
			agentList = append(agentList, AgentSnapshot{
				Agent: api.Agent{
					Name:        baseAgent.Metadata.Name,
					DisplayName: baseAgent.Metadata.DisplayName,
					Description: baseAgent.Metadata.Description,
					Triggers:    slices.Clone(baseAgent.Spec.Triggers),
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

	// 6. Registry validation: surface cross-reference and structural issues
	// (missing defaultAgent/defaultProvider, invalid modelAliases, bad
	// policies.tools, dangling resource references) as non-fatal diagnostic
	// warnings so workspace.Open stays resilient to typos in WORKSPACE.md or
	// .agents/ instead of crashing.
	if err := c.registry.Validate(); err != nil {
		c.validationWarnings = append(c.validationWarnings, err.Error())
	}
}

// MaxRecommendedEagerTools is the recommended maximum number of eager tools an
// agent should load on the first request. Agents resolving to more than this
// many eager tools emit a diagnostic advising the user to reduce spec.tools and
// rely on deferred loading via tool_search.
const MaxRecommendedEagerTools = 20

// ToolPartition splits an agent's effective tools into eager tools (loaded on
// the first request with DeferLoading: false) and deferred tools (discoverable
// via tool_search with DeferLoading: true).
type ToolPartition struct {
	Eager    []string
	Deferred []string
}

// PartitionAgentTools computes the eager/deferred tool partition for a resolved
// agent. When resolved is nil (agentless workspace), every effective tool is
// eager and nothing is deferred.
//
// Eager tools come from resolved.Tools (WARP-resolved from agent.spec.tools and
// filtered by workspace and agent policies), excluding MCP tools (which are
// deferred by default) and conditional built-in tools whose preconditions aren't
// met (invoke_agent/manage_agents when the agent has no subagents, load_skill
// when it has no skills). tool_search is eager only when explicitly listed.
//
// Deferred tools are every effective tool not already eager, not tool_search or
// a conditional built-in whose precondition isn't met, and passing the agent's
// policy ceiling (resolved.ToolAction != ToolPolicyDeny).
func (c *Catalog) PartitionAgentTools(resolved *warp.ResolvedAgent) ToolPartition {
	if resolved == nil {
		return ToolPartition{Eager: c.ToolNames()}
	}

	hasSubagents := len(resolved.Subagents) > 0
	hasSkills := len(resolved.Skills) > 0

	eagerSet := make(map[string]bool)
	var eager []string
	for _, t := range resolved.Tools {
		name := t.Metadata.Name
		if _, _, isMCP := warp.ParseMCPToolName(name); isMCP {
			continue
		}
		if (name == "invoke_agent" || name == "manage_agents") && !hasSubagents {
			continue
		}
		if name == "load_skill" && !hasSkills {
			continue
		}
		if eagerSet[name] {
			continue
		}
		eagerSet[name] = true
		eager = append(eager, name)
	}

	var deferred []string
	for _, t := range c.Tools() {
		name := t.Metadata.Name
		if eagerSet[name] {
			continue
		}
		if name == "tool_search" {
			continue
		}
		if (name == "invoke_agent" || name == "manage_agents") && !hasSubagents {
			continue
		}
		if name == "load_skill" && !hasSkills {
			continue
		}
		if resolved.ToolAction(t) == warp.ToolPolicyDeny {
			continue
		}
		deferred = append(deferred, name)
	}

	return ToolPartition{Eager: eager, Deferred: deferred}
}

// MaxListedMCPToolsPerServer caps how many deferred tool names are listed for a
// single MCP server in the system prompt's <deferred_tools> directory.
const MaxListedMCPToolsPerServer = 15

// DeferredToolSummary describes a non-MCP deferred tool for system prompt discovery.
type DeferredToolSummary struct {
	Name        string
	Description string
}

// DeferredMCPSummary describes an MCP server and its deferred tools for system
// prompt discovery.
type DeferredMCPSummary struct {
	Name        string
	Prefix      string // "mcp__<name>__*"
	Description string
	Tools       []string
	ExtraCount  int
	// ToolsText is the pre-rendered "tools: `a`, `b`, +N more" segment. It is
	// computed in Go because the template shorthand preprocessor mangles named
	// range variables ($i, $t) that would otherwise join the list.
	ToolsText string
}

// DeferredToolsCatalog groups deferred custom/built-in tools and MCP servers.
type DeferredToolsCatalog struct {
	Tools      []DeferredToolSummary
	MCPServers []DeferredMCPSummary
}

// oneLine returns the first non-empty line of s, trimmed.
func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// oneLineDescription returns a single-line trimmed description for a tool.
func oneLineDescription(wt *warp.Tool) string {
	if wt == nil {
		return ""
	}
	return oneLine(wt.Metadata.Description)
}

// mcpDescription looks up a deferred MCP server's one-line description from the
// resolved agent's MCP resources.
func (c *Catalog) mcpDescription(resolved *warp.ResolvedAgent, serverName string) string {
	if resolved == nil {
		return ""
	}
	for _, m := range resolved.MCPs {
		if m != nil && m.Metadata.Name == serverName {
			return oneLine(m.Metadata.Description)
		}
	}
	return ""
}

// BuildDeferredToolsCatalog groups a resolved agent's deferred tools into a
// compact directory of custom/built-in tools and MCP servers for the system
// prompt. It returns nil when the agent has no deferred tools.
func (c *Catalog) BuildDeferredToolsCatalog(resolved *warp.ResolvedAgent) *DeferredToolsCatalog {
	partition := c.PartitionAgentTools(resolved)
	if len(partition.Deferred) == 0 {
		return nil
	}

	cat := &DeferredToolsCatalog{}

	// Preserve deterministic MCP server order and accumulate their tools.
	var serverOrder []string
	serverMap := make(map[string]*DeferredMCPSummary)

	for _, name := range partition.Deferred {
		// Strip the "mcp__" prefix so ParseMCPToolName yields the server name
		// and the short tool name.
		bare := name
		if strings.HasPrefix(bare, "mcp__") {
			bare = strings.TrimPrefix(bare, "mcp__")
		}
		serverName, shortToolName, isMCP := warp.ParseMCPToolName(bare)
		if isMCP {
			srv, ok := serverMap[serverName]
			if !ok {
				srv = &DeferredMCPSummary{
					Name:        serverName,
					Prefix:      "mcp__" + serverName + "__*",
					Description: c.mcpDescription(resolved, serverName),
				}
				serverMap[serverName] = srv
				serverOrder = append(serverOrder, serverName)
			}
			if len(srv.Tools) < MaxListedMCPToolsPerServer {
				srv.Tools = append(srv.Tools, shortToolName)
			} else {
				srv.ExtraCount++
			}
			continue
		}

		wt, _ := c.GetTool(name)
		cat.Tools = append(cat.Tools, DeferredToolSummary{
			Name:        name,
			Description: oneLineDescription(wt),
		})
	}

	for _, name := range serverOrder {
		srv := serverMap[name]
		srv.ToolsText = renderMCPTools(srv.Tools, srv.ExtraCount)
		cat.MCPServers = append(cat.MCPServers, *srv)
	}

	if len(cat.Tools) == 0 && len(cat.MCPServers) == 0 {
		return nil
	}
	return cat
}

// renderMCPTools formats a deferred MCP server's tool list for the system prompt
// as "tools: `a`, `b`, +N more". It is computed in Go because the template
// shorthand preprocessor mangles the named range variables ($i, $t) that would
// otherwise join the list.
func renderMCPTools(tools []string, extra int) string {
	var b strings.Builder
	b.WriteString("tools: ")
	for i, tn := range tools {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("`")
		b.WriteString(tn)
		b.WriteString("`")
	}
	if extra > 0 {
		b.WriteString(", +")
		b.WriteString(strconv.Itoa(extra))
		b.WriteString(" more")
	}
	return b.String()
}

// AgentRenderGlobals returns the template globals for rendering an agent's system
// prompt: a HasTool predicate over the agent's eager tools (including tool_search
// when the agent has deferred tools) and a compact DeferredTools directory for the
// tool_search section.
func (c *Catalog) AgentRenderGlobals(resolved *warp.ResolvedAgent) map[string]any {
	partition := c.PartitionAgentTools(resolved)
	eagerSet := make(map[string]bool, len(partition.Eager)+1)
	for _, name := range partition.Eager {
		eagerSet[strings.ToLower(name)] = true
	}
	if len(partition.Deferred) > 0 {
		eagerSet["tool_search"] = true
	}
	hasTool := func(toolName string) bool {
		return eagerSet[strings.ToLower(strings.TrimSpace(toolName))]
	}
	return map[string]any{
		"HasTool":       hasTool,
		"DeferredTools": c.BuildDeferredToolsCatalog(resolved),
	}
}

func (c *Catalog) calculateAgentContext(ag *warp.Agent, resolved *warp.ResolvedAgent) api.ContextBreakdown {
	contextFiles := c.workspaceTokens

	if ag == nil {
		return api.ContextBreakdown{ContextFiles: contextFiles}
	}

	globals := c.AgentRenderGlobals(resolved)
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
	// Only eager tools are sent in the initial prompt; deferred tools are
	// discovered via tool_search and excluded from the static breakdown.
	partition := c.PartitionAgentTools(resolved)
	eagerSet := make(map[string]bool, len(partition.Eager)+1)
	for _, name := range partition.Eager {
		eagerSet[name] = true
	}
	// tool_search is auto-activated when the agent has deferred tools, so its
	// token cost belongs in the eager system-tools breakdown.
	if len(partition.Deferred) > 0 {
		eagerSet["tool_search"] = true
	}
	toolsList := c.Tools()
	for _, t := range toolsList {
		if resolved != nil && !eagerSet[t.Metadata.Name] {
			continue
		}

		toolTokens, ok := c.toolTokens[t.Metadata.Name]
		if !ok {
			toolTokens = tokenizer.Estimate(t.Metadata.Name) +
				tokenizer.Estimate(t.Metadata.Description) +
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
						Triggers:    slices.Clone(baseAgent.Spec.Triggers),
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
						Tools:        slices.Clone(sk.Spec.Tools),
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

// ToolsForSkill resolves the tools declared by the skill referenced by skillRef
// for the named agent (or the workspace default agent when agentName is empty),
// enforcing both the workspace tool policy ceiling and the agent's full policy
// ceiling (include, exclude, dangerous, openWorld, readOnly). When no agent is
// active (agentless workspace), it resolves the skill's tools against the
// workspace policy ceiling only.
func (c *Catalog) ToolsForSkill(agentName, skillRef string) ([]*warp.Tool, error) {
	if c == nil {
		return nil, fmt.Errorf("catalog: nil catalog")
	}
	resolver := c.resolver()
	if resolver == nil {
		return nil, fmt.Errorf("catalog: no resolver available")
	}

	if agentName == "" {
		agentName = c.DefaultAgentName()
	}
	if agentName != "" {
		if resolved, err := c.ResolveAgent(agentName); err == nil && resolved != nil {
			return resolved.ToolsForSkill(resolver, skillRef)
		}
	}

	switch r := resolver.(type) {
	case *warp.ScopedRegistry:
		return r.ToolsForSkill(skillRef)
	case *warp.Registry:
		return r.ToolsForSkill(skillRef)
	}
	return nil, fmt.Errorf("catalog: resolver does not support ToolsForSkill")
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
						Agent:        cmd.Spec.Agent,
						Context:      string(cmd.Spec.Context),
						ArgumentHint: cmd.Spec.ArgumentHint,
						Arguments:    ToAPIArguments(cmd.Spec.Arguments),
						Models:       slices.Clone(cmd.Spec.Models),
						Tools:        slices.Clone(cmd.Spec.Tools),
						Skills:       slices.Clone(cmd.Spec.Skills),
						Rules:        slices.Clone(cmd.Spec.Rules),
						Namespace:    cmd.Namespace,
					},
					Def: cmd,
				}, true
			}
		}
	}
	return CommandSnapshot{}, false
}

// ResolveCommand resolves the fully merged command for cmdRef for the named
// agent (or the workspace default agent when agentName is empty). It verifies
// command enablement against the agent's spec.commands (unless the command
// declares an explicit spec.agent override) and eagerly resolves its skills,
// rules, and effective tools against the workspace and agent tool policy
// ceilings. When no agent is active (agentless workspace), it delegates to the
// underlying registry's resolver.
func (c *Catalog) ResolveCommand(agentName, cmdRef string) (*warp.ResolvedCommand, error) {
	if c == nil {
		return nil, fmt.Errorf("catalog: nil catalog")
	}
	resolver := c.resolver()
	if resolver == nil {
		return nil, fmt.Errorf("catalog: no resolver available")
	}

	if agentName == "" {
		agentName = c.DefaultAgentName()
	}
	if agentName != "" {
		if resolved, err := c.ResolveAgent(agentName); err == nil && resolved != nil {
			return resolved.ResolveCommand(resolver, cmdRef)
		}
	}

	switch r := resolver.(type) {
	case *warp.ScopedRegistry:
		return r.ResolveCommand(cmdRef)
	case *warp.Registry:
		return r.ResolveCommand(cmdRef)
	}
	return nil, fmt.Errorf("catalog: resolver does not support ResolveCommand")
}

// Tools returns all effective tools in scope, sorted by metadata name.
func (c *Catalog) Tools() []*warp.Tool {
	return c.tools.Snapshot().All()
}

// ToolNames returns the metadata names of all effective tools in scope, in the
// same order as [Tools]. It is a convenience for callers that need an explicit
// allowlist of every workspace tool.
func (c *Catalog) ToolNames() []string {
	tools := c.Tools()
	if len(tools) == 0 {
		return nil
	}
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Metadata.Name
	}
	return names
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

// ParseThinking extracts an api.ThinkingSetting from an agent's thinking configuration.
func ParseThinking(thinking *warp.AgentThinking) *api.ThinkingSetting {
	if thinking == nil {
		return nil
	}
	switch thinking.Type {
	case warp.ThinkingStrategyEffort:
		if thinking.Effort != "" {
			return &api.ThinkingSetting{Type: "effort", Effort: thinking.Effort}
		}
	case warp.ThinkingStrategyBudget:
		if thinking.BudgetTokens > 0 {
			return &api.ThinkingSetting{Type: "budget", Budget: thinking.BudgetTokens}
		}
	case warp.ThinkingStrategyToggle:
		if thinking.Enabled != nil {
			return &api.ThinkingSetting{Type: "toggle", Enabled: *thinking.Enabled}
		}
	case warp.ThinkingStrategyAdaptive:
		return &api.ThinkingSetting{Type: "adaptive", Enabled: true}
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
