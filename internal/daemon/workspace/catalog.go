package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/memdb"
	"github.com/masterkeysrd/tasksmith/internal/core/tokenizer"
	"github.com/masterkeysrd/warp"
)

var (
	reProjectContext = regexp.MustCompile(`(?s)<project_context\b[^>]*>.*?</project_context>`)
	reSkillsSection  = regexp.MustCompile(`(?s)<skills_and_specialized_knowledge\b[^>]*>.*?</skills_and_specialized_knowledge>`)
	reRulesSection   = regexp.MustCompile(`(?s)<rules_and_guidelines\b[^>]*>.*?</rules_and_guidelines>`)
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

// RuleSnapshot pairs an API Rule presentation with its defining warp.Rule.
type RuleSnapshot struct {
	api.Rule
	Def *warp.Rule
}

// ToolSnapshot pairs an API Tool presentation with its defining warp.Tool.
type ToolSnapshot struct {
	api.Tool
	Def *warp.Tool
}

// MCPSnapshot pairs an API MCPServer presentation with its defining warp.MCP.
// The presentation carries only what the manifest declares; live connection
// state and discovered tools, prompts, and resources are overlaid by the
// workspace's MCPService.
type MCPSnapshot struct {
	api.MCPServer
	Def *warp.MCP
}

// catalogState holds the precomputed values a Catalog publishes alongside its
// snapshot stores. It is replaced as one unit after populate() rebuilds every
// store, so a refresh never exposes a torn mix of old and old-and-new values.
type catalogState struct {
	workspaceTokens int
	toolTokens      map[string]int

	// validationWarnings holds non-fatal diagnostic messages produced by
	// registry.Validate() (structural correctness and cross-reference checks
	// across WORKSPACE.md and all loaded resources).
	validationWarnings []string
}

// Catalog acts as an in-memory query and resolution proxy over a warp.Registry
// (and scoped registry when cwd is inside a subproject), exposing native warp
// resource definitions pre-materialized in lock-free point-in-time memdb stores.
type Catalog struct {
	registry *warp.Registry
	cwd      string
	rootDir  string

	// populateMu serializes rebuilds. Each rebuild assembles its state locally and
	// publishes it through state, so concurrent readers observe either the full old
	// snapshot or the full new one.
	populateMu sync.Mutex
	state      atomic.Pointer[catalogState]

	agents    *memdb.DB[AgentSnapshot]
	skills    *memdb.DB[SkillSnapshot]
	commands  *memdb.DB[CommandSnapshot]
	rules     *memdb.DB[RuleSnapshot]
	tools     *memdb.DB[ToolSnapshot]
	mcps      *memdb.DB[MCPSnapshot]
	providers *memdb.DB[*warp.ModelProvider]

	// deniedMCPTools holds tools discovered on MCP servers that spec.policies
	// prohibits. They are deliberately absent from tools so they can never be
	// registered as callable, yet /tools must still show them as blocked.
	deniedMu   sync.Mutex
	deniedKeys map[string][]string
	denied     *memdb.DB[ToolSnapshot]

	// mcpResources probes whether the workspace's MCP runtime currently exposes a
	// resource that spec.policies permits, which decides whether the
	// resource-reading built-in tool is worth loading. It is a function rather
	// than a manager reference so the catalog keeps no dependency on the MCP
	// implementation, and it is safe to evaluate during a rebuild.
	mcpResources atomic.Pointer[func() bool]
}

// NewCatalog constructs a Catalog proxying reg for the given cwd and rootDir,
// pre-materializing all effective resources in lock-free memdb stores.
func NewCatalog(reg *warp.Registry, cwd, rootDir string) *Catalog {
	c := &Catalog{
		registry:   reg,
		cwd:        cwd,
		rootDir:    rootDir,
		agents:     memdb.New(func(a AgentSnapshot) string { return a.Name }),
		skills:     memdb.New(func(s SkillSnapshot) string { return s.Name }),
		commands:   memdb.New(func(cmd CommandSnapshot) string { return cmd.Name }),
		rules:      memdb.New(func(r RuleSnapshot) string { return r.Name }),
		tools:      memdb.New(func(t ToolSnapshot) string { return t.Name }),
		mcps:       memdb.New(func(m MCPSnapshot) string { return m.Name }),
		providers:  memdb.New(func(p *warp.ModelProvider) string { return p.Metadata.Name }),
		deniedKeys: make(map[string][]string),
		denied:     memdb.New(func(t ToolSnapshot) string { return t.Name }),
	}
	c.state.Store(&catalogState{toolTokens: make(map[string]int)})
	c.populate()
	return c
}

// Registry returns the underlying warp.Registry instance.
func (c *Catalog) Registry() *warp.Registry {
	return c.registry
}

// Refresh re-materializes every snapshot store from the current registry state.
// It exists for registry mutations that happen after the catalog was built, such
// as an MCP server's discovered tools being registered at runtime, so tool lists,
// agent partitions, and token counts reflect what the registry now holds. Readers
// keep seeing a consistent point-in-time view because each store and the
// precomputed state are published atomically.
func (c *Catalog) Refresh() error {
	if c == nil {
		return errors.New("workspace: catalog is nil")
	}
	c.populate()
	return nil
}

// snapshot returns the currently published precomputed state, which is never nil
// once the catalog is constructed.
func (c *Catalog) snapshot() *catalogState {
	if s := c.state.Load(); s != nil {
		return s
	}
	return &catalogState{toolTokens: make(map[string]int)}
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
	state := c.snapshot()
	combined := make([]string, 0, len(state.validationWarnings))
	if c.registry != nil {
		combined = append(combined, c.registry.Warnings()...)
	}
	combined = append(combined, state.validationWarnings...)
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

// populate rebuilds every snapshot store from the registry. It computes into a
// scratch state and publishes immutable copies, so a refresh racing a reader
// exposes either the complete previous state or the complete new one, never a mix.
func (c *Catalog) populate() {
	if c.registry == nil {
		return
	}
	resolver := c.resolver()
	if resolver == nil {
		return
	}

	c.populateMu.Lock()
	defer c.populateMu.Unlock()
	st := &catalogState{toolTokens: make(map[string]int)}

	// 0. Workspace prompt tokens pre-computed once
	if wsSpec := c.registry.WorkspaceSpec(); wsSpec != nil {
		wsPrompt, _ := wsSpec.Render(&warp.WorkspaceRenderOptions{})
		st.workspaceTokens = tokenizer.Estimate(wsPrompt)
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
	toolSnapshots := make([]ToolSnapshot, 0, len(toolsList))
	for _, t := range toolsList {
		snapshot := c.newToolSnapshot(t)
		st.toolTokens[snapshot.Name] = snapshot.Tokens
		toolSnapshots = append(toolSnapshots, snapshot)
	}
	c.tools.Replace(toolSnapshots...)

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

	// 5. Rules
	var ruleList []RuleSnapshot
	rawRules := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindRule},
		Effective: true,
	})
	for _, r := range rawRules {
		if rl, ok := r.(*warp.Rule); ok {
			scopeNamespace := c.ScopeNamespace(rl.BaseResource)
			// triggers.decision is not yet supported; surface a non-fatal
			// diagnostic so the user knows the condition is ignored.
			if strings.TrimSpace(rl.Spec.Triggers.Decision) != "" {
				st.validationWarnings = append(st.validationWarnings, fmt.Sprintf("Rule %q specifies unsupported triggers.decision %q; decision triggers are ignored", rl.Metadata.Name, rl.Spec.Triggers.Decision))
			}
			ruleList = append(ruleList, RuleSnapshot{
				Rule: api.Rule{
					Name:        rl.Metadata.Name,
					DisplayName: rl.Metadata.DisplayName,
					Description: rl.Metadata.Description,
					Priority:    rl.Spec.Priority,
					Triggers: api.RuleTriggers{
						Always:   rl.Spec.Triggers.Always,
						Globs:    slices.Clone(rl.Spec.Triggers.Globs),
						UseWhen:  rl.Spec.Triggers.UseWhen,
						Keywords: slices.Clone(rl.Spec.Triggers.Keywords),
						Decision: rl.Spec.Triggers.Decision,
						Tools:    slices.Clone(rl.Spec.Triggers.Tools),
					},
					Namespace:    scopeNamespace,
					Scope:        FormatSkillScope(scopeNamespace),
					Path:         rl.GetPath(),
					Instructions: rl.Spec.Instructions,
					Tokens:       tokenizer.Estimate(strings.TrimSpace(rl.Spec.Instructions)),
				},
				Def: rl,
			})
		}
	}
	c.rules.Replace(ruleList...)

	// Publish token counts before computing per-agent context breakdowns, which
	// read them back through the published state. The copy keeps the scratch state
	// safe to keep mutating while readers hold this snapshot.
	c.state.Store(st.clone())

	// 6. Agents
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
				st.validationWarnings = append(st.validationWarnings, fmt.Sprintf("Agent %q loads %d tools on the first request (recommended maximum is %d); consider reducing spec.tools to use deferred loading", baseAgent.Metadata.Name, len(partition.Eager), MaxRecommendedEagerTools))
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

	// 7. MCP servers
	var mcpList []MCPSnapshot
	rawMCPs := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindMCP},
		Effective: true,
	})
	for _, r := range rawMCPs {
		if m, ok := r.(*warp.MCP); ok {
			mcpList = append(mcpList, c.newMCPSnapshot(m))
		}
	}
	c.mcps.Replace(mcpList...)

	// 8. Registry validation: surface cross-reference and structural issues
	// (missing defaultAgent/defaultProvider, invalid modelAliases, bad
	// policies.tools, dangling resource references) as non-fatal diagnostic
	// warnings so workspace.Open stays resilient to typos in WORKSPACE.md or
	// .agents/ instead of crashing.
	if err := c.registry.Validate(); err != nil {
		st.validationWarnings = append(st.validationWarnings, err.Error())
	}

	c.state.Store(st.clone())
}

// clone returns a defensive copy so a published state is never mutated afterwards.
func (s *catalogState) clone() *catalogState {
	if s == nil {
		return &catalogState{toolTokens: make(map[string]int)}
	}
	out := &catalogState{
		workspaceTokens: s.workspaceTokens,
		toolTokens:      make(map[string]int, len(s.toolTokens)),
	}
	maps.Copy(out.toolTokens, s.toolTokens)
	if len(s.validationWarnings) > 0 {
		out.validationWarnings = slices.Clone(s.validationWarnings)
	}
	return out
}

// explicitlyNamedAgentTools returns the tool names an agent lists outright in
// spec.tools, normalized so an MCP tool written as "mcp__<server>__<tool>" also
// matches the name it materializes under, "<server>__<tool>".
func (c *Catalog) explicitlyNamedAgentTools(resolved *warp.ResolvedAgent) map[string]bool {
	names := make(map[string]bool)
	if resolved == nil || resolved.Agent == nil {
		return names
	}
	for _, entry := range resolved.Agent.Spec.Tools {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		names[entry] = true
		if bare, ok := strings.CutPrefix(entry, mcpToolNamePrefix); ok {
			names[bare] = true
		}
	}
	return names
}

// mcpToolNamePrefix is the optional prefix users may write in spec.tools and the
// deferred-tools directory before an MCP tool's "<server>__<tool>" name.
const mcpToolNamePrefix = "mcp__"

// SetDeniedMCPTools records the tools discovered on one MCP server that
// spec.policies prohibits, replacing any previously recorded set for that server.
// They stay out of the effective tool list so they can never be called, while
// still appearing in tool listings as blocked by policy.
func (c *Catalog) SetDeniedMCPTools(def *warp.MCP, raw []*warp.Tool) {
	if def == nil || strings.TrimSpace(def.Metadata.Name) == "" {
		return
	}
	serverName := def.Metadata.Name
	snaps := make([]ToolSnapshot, 0, len(raw))
	for _, t := range raw {
		if t == nil {
			continue
		}
		snap := c.newToolSnapshot(t)
		// A discovered raw tool carries no namespace of its own, so report the
		// namespace of the server that exposed it.
		if ns := def.GetNamespace(); ns != "" {
			snap.Namespace = ns
		}
		snap.Status = api.ToolStatusDenied
		snaps = append(snaps, snap)
	}

	c.deniedMu.Lock()
	defer c.deniedMu.Unlock()
	if c.deniedKeys == nil {
		c.deniedKeys = make(map[string][]string)
	}
	for _, name := range c.deniedKeys[serverName] {
		c.denied.Delete(name)
	}
	if len(snaps) == 0 {
		delete(c.deniedKeys, serverName)
		return
	}
	keys := make([]string, 0, len(snaps))
	for _, snap := range snaps {
		keys = append(keys, snap.Name)
	}
	slices.Sort(keys)
	c.denied.Save(snaps...)
	c.deniedKeys[serverName] = keys
}

// DeniedMCPToolSnapshots returns the discovered MCP tools that policy prohibits,
// sorted by name.
func (c *Catalog) DeniedMCPToolSnapshots() []ToolSnapshot {
	snaps := c.denied.Snapshot().All()
	slices.SortFunc(snaps, func(a, b ToolSnapshot) int { return strings.Compare(a.Name, b.Name) })
	return snaps
}

// SetMCPResourceProbe installs the predicate that decides whether the
// resource-reading built-in tool is worth loading. Passing nil clears it, which
// makes the tool inactive. The probe is consulted on every rebuild, because MCP
// resources appear only once a server has connected in the background.
func (c *Catalog) SetMCPResourceProbe(probe func() bool) {
	if probe == nil {
		c.mcpResources.Store(nil)
		return
	}
	c.mcpResources.Store(&probe)
}

// HasAllowedMCPResources reports whether the workspace's MCP runtime exposes a
// policy-permitted MCP resource. With no runtime probe installed it reports false,
// so a workspace without an MCP runtime never loads the resource-reading tool.
func (c *Catalog) HasAllowedMCPResources() bool {
	probe := c.mcpResources.Load()
	if probe == nil || *probe == nil {
		return false
	}
	return (*probe)()
}

// MaxRecommendedEagerTools is the recommended maximum number of eager tools an
// agent should load on the first request. Agents resolving to more than this
// many eager tools emit a diagnostic advising the user to reduce spec.tools and
// rely on deferred loading via tool_search.
const MaxRecommendedEagerTools = 20

// builtinToolReadMCPResource names the built-in tool that reads a resource from an
// MCP server. It is conditional: it costs context only when the workspace actually
// has a readable MCP resource, so the catalog owns its activation.
const builtinToolReadMCPResource = "read_mcp_resource"

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
// filtered by workspace and agent policies), excluding MCP tools — which are
// deferred by default because they are discovered at runtime and can be numerous —
// unless the agent names one outright in spec.tools, and excluding conditional
// built-in tools whose preconditions aren't met (invoke_agent/manage_agents when
// the agent has no subagents, load_skill when it has no skills, read_mcp_resource
// when no server exposes a permitted resource). tool_search is
// eager only when explicitly listed.
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
	hasConditionalRules := c.HasConditionalRules(resolved)
	hasMCPResources := c.HasAllowedMCPResources()

	eagerSet := make(map[string]bool)
	var eager []string
	explicitlyNamed := c.explicitlyNamedAgentTools(resolved)
	for _, t := range resolved.Tools {
		name := t.Metadata.Name
		// MCP tools are discovered at runtime and can be numerous, so they stay
		// deferred unless the agent names one outright in spec.tools.
		if _, _, isMCP := warp.ParseMCPToolName(name); isMCP && !explicitlyNamed[name] {
			continue
		}
		if (name == "invoke_agent" || name == "manage_agents") && !hasSubagents {
			continue
		}
		if name == "load_skill" && !hasSkills {
			continue
		}
		if name == "load_rule" && !hasConditionalRules {
			continue
		}
		if name == builtinToolReadMCPResource && !hasMCPResources {
			continue
		}
		if eagerSet[name] {
			continue
		}
		eagerSet[name] = true
		eager = append(eager, name)
	}

	// load_rule is always active (eager) when the agent has conditional rules,
	// even if a custom agent omitted it from spec.tools, unless it is explicitly
	// denied by the workspace or agent tool policy ceiling.
	if hasConditionalRules && !eagerSet["load_rule"] {
		if wt, ok := c.GetTool("load_rule"); ok && resolved.ToolAction(wt) != warp.ToolPolicyDeny {
			eagerSet["load_rule"] = true
			eager = append(eager, "load_rule")
		}
	}

	// read_mcp_resource is likewise always active once the workspace actually has a
	// readable MCP resource, since an agent author cannot know at authoring time
	// whether the servers it talks to expose resources.
	if hasMCPResources && !eagerSet[builtinToolReadMCPResource] {
		if wt, ok := c.GetTool(builtinToolReadMCPResource); ok && resolved.ToolAction(wt) != warp.ToolPolicyDeny {
			eagerSet[builtinToolReadMCPResource] = true
			eager = append(eager, builtinToolReadMCPResource)
		}
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
		// load_rule and read_mcp_resource are never deferred: each is either eager
		// (when its precondition is met) or omitted entirely (when it isn't). A
		// tool_search lookup for a tool that depends on a live MCP connection would
		// otherwise surface something that may not be callable.
		if name == "load_rule" || name == builtinToolReadMCPResource {
			continue
		}
		if resolved.ToolAction(t) == warp.ToolPolicyDeny {
			continue
		}
		deferred = append(deferred, name)
	}

	return ToolPartition{Eager: eager, Deferred: deferred}
}

// ToolStatuses classifies every effective tool for a resolved agent as eager,
// deferred, denied, or inactive, sharing the partition's decision rules so an
// interactive tool browser can report what the agent can actually reach.
//
// A nil resolved (agentless workspace) makes every effective tool eager,
// mirroring [Catalog.PartitionAgentTools]. tool_search is eager whenever any
// tool is deferred (the session auto-activates it) unless a policy denies it.
// Tools that are neither eager nor deferred are denied when the workspace or
// agent policy ceiling blocks them, and inactive otherwise (conditional
// built-ins whose preconditions are unmet, for example).
func (c *Catalog) ToolStatuses(resolved *warp.ResolvedAgent) map[string]api.ToolStatus {
	tools := c.Tools()
	statuses := make(map[string]api.ToolStatus, len(tools))
	// Policy-prohibited MCP tools are not in the effective list, so classify them
	// directly; they are blocked regardless of the agent's own policy.
	denied := c.DeniedMCPToolSnapshots()
	if resolved == nil {
		for _, t := range tools {
			statuses[t.Metadata.Name] = api.ToolStatusEager
		}
		for _, snap := range denied {
			statuses[snap.Name] = api.ToolStatusDenied
		}
		return statuses
	}

	partition := c.PartitionAgentTools(resolved)
	for _, name := range partition.Eager {
		statuses[name] = api.ToolStatusEager
	}
	for _, name := range partition.Deferred {
		statuses[name] = api.ToolStatusDeferred
	}

	hasDeferred := len(partition.Deferred) > 0
	for _, t := range tools {
		name := t.Metadata.Name
		if _, ok := statuses[name]; ok {
			continue
		}
		switch {
		case name == "tool_search" && hasDeferred && resolved.ToolAction(t) != warp.ToolPolicyDeny:
			statuses[name] = api.ToolStatusEager
		case resolved.ToolAction(t) == warp.ToolPolicyDeny:
			statuses[name] = api.ToolStatusDenied
		default:
			statuses[name] = api.ToolStatusInactive
		}
	}
	for _, snap := range denied {
		statuses[snap.Name] = api.ToolStatusDenied
	}
	return statuses
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
		if after, ok := strings.CutPrefix(bare, "mcp__"); ok {
			bare = after
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
		"HasTool":          hasTool,
		"DeferredTools":    c.BuildDeferredToolsCatalog(resolved),
		"ConditionalRules": c.ConditionalTemplateRules(resolved),
	}
}

func (c *Catalog) calculateAgentContext(ag *warp.Agent, resolved *warp.ResolvedAgent) api.ContextBreakdown {
	state := c.snapshot()
	contextFiles := state.workspaceTokens

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

	var skills, subagents, rules int

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

	ruleMatches := reRulesSection.FindAllString(agentPrompt, -1)
	for _, m := range ruleMatches {
		rules += tokenizer.Estimate(m)
	}
	agentPrompt = reRulesSection.ReplaceAllString(agentPrompt, "")

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

		toolTokens, ok := state.toolTokens[t.Metadata.Name]
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

		// MCP tools carry the namespace of the manifest that declares the server,
		// so membership is decided by the server name embedded in the tool name.
		if c.mcpServerForTool(t) != nil {
			mcpTools += toolTokens
			continue
		}
		ns := strings.ToLower(t.Namespace)
		if ns == warp.NamespaceSystem || ns == "system" || ns == "" {
			systemTools += toolTokens
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
		Rules:        rules,
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

// Rules returns all effective rule snapshots in scope, sorted by metadata name.
func (c *Catalog) Rules() []RuleSnapshot {
	return c.rules.Snapshot().All()
}

// GetRule resolves a rule snapshot by name or qualified name.
func (c *Catalog) GetRule(name string) (RuleSnapshot, bool) {
	if snap, ok := c.rules.Snapshot().Get(name); ok {
		return snap, true
	}
	if strings.Contains(name, "/") {
		for _, s := range c.rules.Snapshot().All() {
			if s.Def != nil && s.Def.QualifiedName() == name {
				return s, true
			}
		}
		if res, ok := c.resolveByKind(warp.KindRule, name); ok {
			if rl, ok := res.(*warp.Rule); ok {
				scopeNamespace := c.ScopeNamespace(rl.BaseResource)
				return RuleSnapshot{
					Rule: api.Rule{
						Name:        rl.Metadata.Name,
						DisplayName: rl.Metadata.DisplayName,
						Description: rl.Metadata.Description,
						Priority:    rl.Spec.Priority,
						Triggers: api.RuleTriggers{
							Always:   rl.Spec.Triggers.Always,
							Globs:    slices.Clone(rl.Spec.Triggers.Globs),
							UseWhen:  rl.Spec.Triggers.UseWhen,
							Keywords: slices.Clone(rl.Spec.Triggers.Keywords),
							Decision: rl.Spec.Triggers.Decision,
							Tools:    slices.Clone(rl.Spec.Triggers.Tools),
						},
						Namespace:    scopeNamespace,
						Scope:        FormatSkillScope(scopeNamespace),
						Path:         rl.GetPath(),
						Instructions: rl.Spec.Instructions,
						Tokens:       tokenizer.Estimate(strings.TrimSpace(rl.Spec.Instructions)),
					},
					Def: rl,
				}, true
			}
		}
	}
	return RuleSnapshot{}, false
}

// HasConditionalRules reports whether the resolved agent has at least one rule
// that is not unconditionally active (i.e. !triggers.always). Such rules are
// surfaced to the agent via the load_rule tool and the <available_rules>
// discovery directory rather than being statically injected into the system
// prompt. It returns false when resolved is nil or all rules are always-active.
func (c *Catalog) HasConditionalRules(resolved *warp.ResolvedAgent) bool {
	if resolved == nil {
		return false
	}
	for _, rl := range resolved.Rules {
		if !rl.Spec.Triggers.Always {
			return true
		}
	}
	return false
}

// templateRule is the view model exposed to agent templates for a conditional
// (non-always) workspace rule. It carries the rule's trigger fields and its
// rendered instructions so the <available_rules> discovery directory can advise
// the agent on when to load the rule via load_rule.
type templateRule struct {
	Name         string
	Description  string
	UseWhen      string
	Keywords     []string
	Globs        []string
	Tools        []string
	Always       bool
	Priority     int
	Instructions string
}

// ConditionalTemplateRules returns the view models for the resolved agent's
// conditional (non-always) rules, sorted by priority descending then name
// ascending. Always-active rules are excluded because they are statically
// injected into <workspace_rules> instead of being surfaced for on-demand
// loading.
func (c *Catalog) ConditionalTemplateRules(resolved *warp.ResolvedAgent) []templateRule {
	if resolved == nil {
		return nil
	}

	var out []templateRule
	for i := range resolved.Rules {
		rl := &resolved.Rules[i]
		if rl.Spec.Triggers.Always {
			continue
		}
		rendered, err := rl.Render(&warp.RuleRenderOptions{
			Workspace: c.WorkspaceSpec(),
			Project:   c.Project(),
			Agent:     resolved.Agent,
		})
		if err != nil {
			rendered = rl.Spec.Instructions
		}
		out = append(out, templateRule{
			Name:         rl.Metadata.Name,
			Description:  rl.Metadata.Description,
			UseWhen:      rl.Spec.Triggers.UseWhen,
			Keywords:     slices.Clone(rl.Spec.Triggers.Keywords),
			Globs:        slices.Clone(rl.Spec.Triggers.Globs),
			Tools:        slices.Clone(rl.Spec.Triggers.Tools),
			Always:       rl.Spec.Triggers.Always,
			Priority:     rl.Spec.Priority,
			Instructions: rendered,
		})
	}

	slices.SortFunc(out, func(a, b templateRule) int {
		if a.Priority != b.Priority {
			return b.Priority - a.Priority
		}
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})

	return out
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

// newToolSnapshot materializes the API presentation of an effective tool,
// including its scope label, safety annotations, JSON schemas, and the token
// cost of the catalog entry it contributes to an agent's context.
func (c *Catalog) newToolSnapshot(t *warp.Tool) ToolSnapshot {
	scopeNamespace := c.ScopeNamespace(t.BaseResource)

	toolTokens := tokenizer.Estimate(t.Metadata.Name) +
		tokenizer.Estimate(t.Metadata.Description) +
		tokenizer.Estimate(t.Spec.Instructions)
	if len(t.Spec.InputSchema) > 0 {
		if data, err := json.Marshal(t.Spec.InputSchema); err == nil {
			toolTokens += tokenizer.Estimate(string(data))
		}
	}

	scope := FormatSkillScope(scopeNamespace)
	if server := c.mcpServerForTool(t); server != nil {
		scope = "MCP (" + server.Metadata.Name + ")"
	}

	tool := api.Tool{
		Name:         t.Metadata.Name,
		DisplayName:  t.Metadata.DisplayName,
		Description:  t.Metadata.Description,
		Namespace:    scopeNamespace,
		Scope:        scope,
		Command:      slices.Clone(t.Spec.Command),
		InputSchema:  maps.Clone(t.Spec.InputSchema),
		OutputSchema: maps.Clone(t.Spec.OutputSchema),
		Tokens:       toolTokens,
	}
	if t.Spec.Annotations != nil {
		tool.Annotations = api.ToolAnnotation{
			IsOpenWorld:  t.Spec.Annotations.IsOpenWorld,
			IsDangerous:  t.Spec.Annotations.IsDangerous,
			IsReadOnly:   t.Spec.Annotations.IsReadOnly,
			IsIdempotent: t.Spec.Annotations.IsIdempotent,
			UserHint:     t.Spec.Annotations.UserHint,
		}
	}
	return ToolSnapshot{Tool: tool, Def: t}
}

// mcpServerForTool reports the MCP server a tool was discovered from, or nil when
// the tool is a regular manifest tool. MCP tools are materialized as
// "<server>__<tool>", so a tool is attributed to a server only when a server with
// that name exists and produced exactly this tool name.
func (c *Catalog) mcpServerForTool(t *warp.Tool) *warp.MCP {
	if t == nil {
		return nil
	}
	serverName, rawName, ok := warp.ParseMCPToolName(t.Metadata.Name)
	if !ok {
		return nil
	}
	res, ok := c.resolveByKind(warp.KindMCP, serverName)
	if !ok {
		return nil
	}
	server, ok := res.(*warp.MCP)
	if !ok || server == nil {
		return nil
	}
	if server.ToolName(rawName) != t.Metadata.Name {
		return nil
	}
	return server
}

// MCPServerForTool reports the MCP server a tool was discovered from, or nil when
// the tool is a regular manifest tool. It is the exported form of the attribution
// used to give MCP tools their scope label, so a tool container can register MCP
// tools without duplicating the naming rules.
func (c *Catalog) MCPServerForTool(t *warp.Tool) *warp.MCP {
	return c.mcpServerForTool(t)
}

// ToolSnapshots returns all effective tool snapshots in scope, sorted by
// metadata name. Each snapshot carries the pre-materialized api.Tool
// presentation (scope, schemas, annotations, and token cost) alongside the
// defining warp.Tool.
func (c *Catalog) ToolSnapshots() []ToolSnapshot {
	return c.tools.Snapshot().All()
}

// Tools returns all effective tools in scope, sorted by metadata name.
func (c *Catalog) Tools() []*warp.Tool {
	snaps := c.tools.Snapshot().All()
	if len(snaps) == 0 {
		return nil
	}
	out := make([]*warp.Tool, len(snaps))
	for i, s := range snaps {
		out[i] = s.Def
	}
	return out
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
	if snap, ok := c.GetToolSnapshot(name); ok {
		return snap.Def, true
	}
	return nil, false
}

// GetToolSnapshot resolves an effective tool snapshot by name or qualified name.
func (c *Catalog) GetToolSnapshot(name string) (ToolSnapshot, bool) {
	if snap, ok := c.tools.Snapshot().Get(name); ok {
		return snap, true
	}
	if strings.Contains(name, "/") {
		for _, s := range c.tools.Snapshot().All() {
			if s.Def != nil && s.Def.QualifiedName() == name {
				return s, true
			}
		}
		if res, ok := c.resolveByKind(warp.KindTool, name); ok {
			if t, ok := res.(*warp.Tool); ok {
				return c.newToolSnapshot(t), true
			}
		}
	}
	return ToolSnapshot{}, false
}

// newMCPSnapshot materializes the API presentation of an effective MCP server
// from what its manifest declares: scope, transport, rendered command or
// endpoint, and authentication scheme. Live connection state and discovered
// tools, prompts, and resources are overlaid by the workspace's MCPService.
func (c *Catalog) newMCPSnapshot(m *warp.MCP) MCPSnapshot {
	if m == nil {
		return MCPSnapshot{}
	}
	scopeNamespace := c.ScopeNamespace(m.BaseResource)
	transport := strings.TrimSpace(string(m.Spec.Type))
	if transport == "" {
		transport = string(warp.MCPTransportStdio)
	}
	server := api.MCPServer{
		Name:        m.Metadata.Name,
		DisplayName: m.Metadata.DisplayName,
		Description: m.Metadata.Description,
		Namespace:   scopeNamespace,
		Scope:       FormatSkillScope(scopeNamespace),
		Path:        m.GetPath(),
		Transport:   transport,
		Command:     slices.Clone(m.Spec.Command),
		Endpoint:    m.Spec.Endpoint,
		Status:      api.MCPStatusDisconnected,
	}
	if m.Spec.Auth != nil {
		server.AuthType = strings.TrimSpace(m.Spec.Auth.Type)
	}
	return MCPSnapshot{MCPServer: server, Def: m}
}

// MCPSnapshots returns all effective MCP server snapshots in scope, sorted by
// metadata name. Each snapshot carries the manifest-derived api.MCPServer
// presentation alongside the defining warp.MCP.
func (c *Catalog) MCPSnapshots() []MCPSnapshot {
	return c.mcps.Snapshot().All()
}

// MCPs returns all effective MCP server definitions in scope, sorted by metadata
// name.
func (c *Catalog) MCPs() []*warp.MCP {
	snaps := c.mcps.Snapshot().All()
	if len(snaps) == 0 {
		return nil
	}
	out := make([]*warp.MCP, len(snaps))
	for i, s := range snaps {
		out[i] = s.Def
	}
	return out
}

// GetMCPSnapshot resolves an effective MCP server snapshot by name or qualified
// name.
func (c *Catalog) GetMCPSnapshot(name string) (MCPSnapshot, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return MCPSnapshot{}, false
	}
	if snap, ok := c.mcps.Snapshot().Get(name); ok {
		return snap, true
	}
	if strings.Contains(name, "/") {
		for _, s := range c.mcps.Snapshot().All() {
			if s.Def != nil && s.Def.QualifiedName() == name {
				return s, true
			}
		}
		if res, ok := c.resolveByKind(warp.KindMCP, name); ok {
			if m, ok := res.(*warp.MCP); ok {
				return c.newMCPSnapshot(m), true
			}
		}
	}
	return MCPSnapshot{}, false
}

// GetMCP resolves an effective MCP server definition by name or qualified name.
func (c *Catalog) GetMCP(name string) (*warp.MCP, bool) {
	if snap, ok := c.GetMCPSnapshot(name); ok {
		return snap.Def, true
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
	return api.ContextBreakdown{ContextFiles: c.snapshot().workspaceTokens}
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
