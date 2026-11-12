package harness

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

// AttachmentResolverConfig configures a unified [AttachmentResolver] for user messages.
type AttachmentResolverConfig struct {
	AgentName   string
	Workspace   *workspace.Workspace
	Tools       tools.Container
	Rules       *RuleMatcher
	Std         *std.Std
	Sandbox     sandbox.Sandbox
	Permissions permissions.SessionManager
	WorkDir     string
	MCPCaller   func() tools.MCPCaller
	Logger      log.Interface
}

// Validate ensures all required dependencies are present.
func (c AttachmentResolverConfig) Validate() error {
	if c.Workspace == nil {
		return fmt.Errorf("%w: workspace is required", ErrInvalidConfig)
	}
	if c.Std == nil {
		return fmt.Errorf("%w: std is required", ErrInvalidConfig)
	}
	if c.Sandbox == nil {
		return fmt.Errorf("%w: sandbox is required", ErrInvalidConfig)
	}
	if c.Permissions == nil {
		return fmt.Errorf("%w: permissions manager is required", ErrInvalidConfig)
	}
	return nil
}

// AttachmentResolver validates and resolves all structured user-message attachments
// (files, directories, rules, skills, MCP resources) alongside conditional rule
// suggestions into a single <system_reminders> block.
type AttachmentResolver struct {
	agentName string
	workspace *workspace.Workspace
	tools     tools.Container
	rules     *RuleMatcher
	std       *std.Std
	sandbox   sandbox.Sandbox
	perms     permissions.SessionManager
	workDir   string
	mcpCaller func() tools.MCPCaller
	logger    log.Interface
}

// NewAttachmentResolver validates cfg and constructs a unified [AttachmentResolver].
func NewAttachmentResolver(cfg AttachmentResolverConfig) (*AttachmentResolver, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &AttachmentResolver{
		agentName: cfg.AgentName,
		workspace: cfg.Workspace,
		tools:     cfg.Tools,
		rules:     cfg.Rules,
		std:       cfg.Std,
		sandbox:   cfg.Sandbox,
		perms:     cfg.Permissions,
		workDir:   cmp.Or(strings.TrimSpace(cfg.WorkDir), cfg.Workspace.RootDir()),
		mcpCaller: cfg.MCPCaller,
		logger:    cmp.Or[log.Interface](cfg.Logger, log.Default()),
	}, nil
}

func normalizeLineRange(start, end int) (int, int) {
	if start > 0 && end > 0 && end < start {
		return end, start
	}
	return start, end
}

func parseMCPTarget(target string) (serverName, uri string, ok bool) {
	serverName, uri, found := strings.Cut(strings.TrimSpace(target), ":")
	serverName = strings.TrimSpace(serverName)
	uri = strings.TrimSpace(uri)
	if !found || serverName == "" || uri == "" {
		return "", "", false
	}
	return serverName, uri, true
}

// Validate normalizes incoming request attachments and enforces sandbox file
// policies and MCP resource policies upfront at turn submission time.
func (r *AttachmentResolver) Validate(ctx context.Context, atts []api.Attachment) ([]api.Attachment, error) {
	if len(atts) == 0 {
		return nil, nil
	}
	cat := r.workspace.Catalog()
	out := make([]api.Attachment, len(atts))
	for i, att := range atts {
		target := strings.TrimSpace(att.Target)
		if target == "" {
			return nil, fmt.Errorf("%w: invalid attachment %d: target cannot be empty", ErrInvalidConfig, i)
		}

		norm := api.Attachment{
			Kind:   att.Kind,
			Ref:    att.Ref,
			Target: target,
		}

		switch att.Kind {
		case api.AttachmentFile:
			norm.StartLine, norm.EndLine = normalizeLineRange(att.StartLine, att.EndLine)
			m, _ := fileMentionFromAttachment(norm)
			if err := r.CheckFileAccess(ctx, m); err != nil {
				return nil, &api.APIError{
					StatusCode: http.StatusForbidden,
					Code:       "forbidden",
					Message:    err.Error(),
				}
			}

		case api.AttachmentMCP:
			serverName, uri, ok := parseMCPTarget(target)
			if !ok {
				return nil, fmt.Errorf("%w: invalid mcp attachment %d: expected <server>:<uri>", ErrInvalidConfig, i)
			}
			norm.Target = serverName + ":" + uri
			norm.AllowProhibited = att.AllowProhibited
			def, found := cat.GetMCP(serverName)
			if !found {
				return nil, &api.APIError{
					StatusCode: http.StatusNotFound,
					Code:       "not_found",
					Message:    fmt.Sprintf("MCP server %q is not configured in this workspace", serverName),
				}
			}
			if !norm.AllowProhibited && !def.AllowsResource(uri) {
				return nil, &api.APIError{
					StatusCode: http.StatusForbidden,
					Code:       "mcp_policy_prohibited",
					Message:    fmt.Sprintf("MCP resource %q is prohibited by spec.policies.resources", uri),
				}
			}

		case api.AttachmentSymbol:
			norm.StartLine, norm.EndLine = normalizeLineRange(att.StartLine, att.EndLine)

		case api.AttachmentRule, api.AttachmentSkill:

		default:
			return nil, fmt.Errorf("%w: invalid attachment %d: unknown kind %q", ErrInvalidConfig, i, att.Kind)
		}

		out[i] = norm
	}
	return out, nil
}

// ResolveUserMessage resolves all structured attachments and rule triggers on a
// genuine user message into a single <system_reminders> message, returning the
// updated state, the synthesized reminder (or nil when nothing matched), and any
// newly loaded rules.
func (r *AttachmentResolver) ResolveUserMessage(ctx context.Context, msg message.Message, state State) (State, *message.User, []RuleLoaded) {
	user, ok := msg.(*message.User)
	if !ok || user == nil || SystemKindOf(user) != "" {
		return state, nil, nil
	}

	if invokedSkill, _ := user.GetMetadata()["skill"].(string); strings.TrimSpace(invokedSkill) != "" {
		state = state.RecordLoadedSkills(strings.TrimSpace(invokedSkill))
	}

	atts := AttachmentsOf(user)

	var sections []string
	var ruleNames []string

	ruleBlocks, loaded := r.resolveRuleBlocks(atts, state)
	if len(loaded) > 0 {
		state = state.RecordLoadedRules(ruleLoadedNames(loaded)...)
		ruleNames = append(ruleNames, ruleLoadedNames(loaded)...)
		sections = append(sections, ruleBlocks...)
	}

	suggestionSection, suggested := r.rules.SuggestUserRules(user, state)
	if len(suggested) > 0 {
		state = state.RecordSuggestedRules(suggested...)
		ruleNames = append(ruleNames, suggested...)
	}
	if suggestionSection != "" {
		sections = append(sections, suggestionSection)
	}

	skillBlocks, loadedSkills, skillToolBlocks := r.resolveSkillBlocks(atts, state)
	if len(loadedSkills) > 0 {
		state = state.RecordLoadedSkills(loadedSkills...)
		sections = append(sections, skillBlocks...)
	}

	if len(atts) > 0 {
		sections = append(sections, r.resolveFileBlocks(ctx, atts)...)
		sections = append(sections, r.resolveMCPBlocks(ctx, atts)...)
	}

	if len(sections) == 0 {
		return state, nil, loaded
	}

	var sb strings.Builder
	sb.WriteString("<system_reminders>\n")
	sb.WriteString(strings.Join(sections, "\n\n"))
	sb.WriteString("\n</system_reminders>")

	meta := map[string]any{
		MetaSystemKind: SystemKindReminder,
	}
	if len(ruleNames) > 0 {
		meta[MetaRuleNames] = ruleNames
	}

	content := make(message.Content, 0, 1+len(skillToolBlocks))
	content = append(content, &message.TextBlock{Text: sb.String()})
	content = append(content, skillToolBlocks...)

	var reminderMsg message.Message = &message.User{
		Base:    message.Base{Metadata: meta},
		Content: content,
	}
	if len(skillToolBlocks) > 0 {
		var newTools []string
		reminderMsg, newTools = state.DeduplicateToolBlocks(reminderMsg)
		state = state.RecordLoadedTools(newTools...)
	}

	reminder, _ := reminderMsg.(*message.User)
	return state, reminder, loaded
}

func (r *AttachmentResolver) renderContext() (*warp.Workspace, *warp.Project, *warp.Agent, map[string]any) {
	cat := r.workspace.Catalog()
	var activeAgent *warp.Agent
	var globals map[string]any
	if resolved, err := cat.ResolveAgent(r.agentName); err == nil {
		activeAgent = resolved.Agent
		globals = cat.AgentRenderGlobals(resolved)
	}
	return cat.WorkspaceSpec(), cat.Project(), activeAgent, globals
}

func (r *AttachmentResolver) lookupRule(name string) (*warp.Rule, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return nil, false
	}
	cat := r.workspace.Catalog()
	if snap, ok := cat.GetRule(strings.TrimSpace(name)); ok {
		return snap.Def, true
	}
	for _, snap := range cat.Rules() {
		if matchWarpResourceKey(snap.Def, snap.Namespace, key) {
			return snap.Def, true
		}
	}
	return nil, false
}

func (r *AttachmentResolver) lookupSkill(name string) (*warp.Skill, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return nil, false
	}
	cat := r.workspace.Catalog()
	if snap, ok := cat.GetSkill(strings.TrimSpace(name)); ok {
		return snap.Def, true
	}
	for _, snap := range cat.Skills() {
		if matchWarpResourceKey(snap.Def, snap.Namespace, key) {
			return snap.Def, true
		}
	}
	return nil, false
}

func (r *AttachmentResolver) portablePath(raw string) string {
	raw = strings.TrimSpace(raw)
	return cmp.Or(r.workspace.PortablePath(raw), raw)
}

func (r *AttachmentResolver) resolveRuleBlocks(atts []api.Attachment, state State) ([]string, []RuleLoaded) {
	if len(atts) == 0 {
		return nil, nil
	}
	var blocks []string
	var loaded []RuleLoaded
	seen := make(map[string]struct{})
	wsSpec, proj, activeAgent, globals := r.renderContext()

	for _, att := range atts {
		if att.Kind != api.AttachmentRule {
			continue
		}
		rl, ok := r.lookupRule(att.Target)
		if !ok || state.HasLoadedRule(rl.Metadata.Name) {
			continue
		}
		key := strings.ToLower(rl.Metadata.Name)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		path := r.portablePath(rl.GetPath())
		rendered := renderWarpRule(rl, wsSpec, proj, activeAgent, globals)
		blocks = append(blocks, fmt.Sprintf("<rule name=%q path=%q>\n%s\n</rule>", rl.Metadata.Name, path, rendered))
		loaded = append(loaded, RuleLoaded{
			ID:      uuid.NewString(),
			Rule:    rl.Metadata.Name,
			Trigger: "manual",
			Matched: cmp.Or(att.Ref, "@rule:"+rl.Metadata.Name),
			Path:    path,
		})
	}

	return blocks, loaded
}

func (r *AttachmentResolver) resolveSkillBlocks(atts []api.Attachment, state State) ([]string, []string, []message.Block) {
	if len(atts) == 0 {
		return nil, nil, nil
	}
	var blocks []string
	var loaded []string
	var toolBlocks []message.Block
	seen := make(map[string]struct{})
	seenTools := make(map[string]struct{})
	wsSpec, proj, activeAgent, globals := r.renderContext()

	for _, att := range atts {
		if att.Kind != api.AttachmentSkill {
			continue
		}
		sk, ok := r.lookupSkill(att.Target)
		if !ok || state.HasLoadedSkill(sk.Metadata.Name) {
			continue
		}
		key := strings.ToLower(sk.Metadata.Name)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		rendered, err := sk.Render(&warp.SkillRenderOptions{
			Workspace: wsSpec,
			Project:   proj,
			Agent:     activeAgent,
			Globals:   globals,
		})
		if err != nil {
			rendered = sk.Spec.Instructions
		}

		path := r.portablePath(sk.Directory)
		body, _ := tools.TruncateSkillInstructions(strings.TrimSpace(rendered), path)
		blocks = append(blocks, fmt.Sprintf("<skill name=%q path=%q>\n%s\n</skill>", sk.Metadata.Name, path, body))
		loaded = append(loaded, sk.Metadata.Name)

		for _, blk := range r.skillToolBlocks(sk) {
			if tName := strings.ToLower(toolBlockName(blk)); tName != "" {
				if _, dup := seenTools[tName]; dup {
					continue
				}
				seenTools[tName] = struct{}{}
			}
			toolBlocks = append(toolBlocks, blk)
		}
	}

	return blocks, loaded, toolBlocks
}

func (r *AttachmentResolver) skillToolBlocks(sk *warp.Skill) []message.Block {
	cat := r.workspace.Catalog()
	skillTools, err := cat.ToolsForSkill(r.agentName, cmp.Or(sk.QualifiedName(), sk.Metadata.Name))
	if err != nil {
		return nil
	}
	return tools.ActivationBlocks(r.tools, cat, skillTools)
}

func (r *AttachmentResolver) resolveMCPBlocks(ctx context.Context, atts []api.Attachment) []string {
	var caller tools.MCPCaller
	if r.mcpCaller != nil {
		caller = r.mcpCaller()
	}

	var blocks []string
	seen := make(map[string]struct{})
	for _, att := range atts {
		if att.Kind != api.AttachmentMCP {
			continue
		}
		serverName, uri, ok := parseMCPTarget(att.Target)
		if !ok {
			continue
		}
		dedupKey := strings.ToLower(serverName) + ":" + uri
		if _, exists := seen[dedupKey]; exists {
			continue
		}
		seen[dedupKey] = struct{}{}

		resp, err := tools.ReadMCPResource(ctx, r.workspace, caller, serverName, uri, att.AllowProhibited)
		if err != nil {
			if _, prohibited := errors.AsType[*tools.MCPProhibitedError](err); !prohibited {
				r.logger.Warn("mcp resource attachment failed", log.String("server", serverName), log.String("uri", uri), log.Any("err", err))
			}
			continue
		}

		var attrs strings.Builder
		if att.Ref != "" {
			fmt.Fprintf(&attrs, " ref=%q", att.Ref)
		}
		fmt.Fprintf(&attrs, " server=%q uri=%q", serverName, uri)
		if resp.MIMEType != "" {
			fmt.Fprintf(&attrs, " mime_type=%q", resp.MIMEType)
		}
		blocks = append(blocks, fmt.Sprintf("<context_resource%s>\n%s\n</context_resource>", attrs.String(), resp.Text))
	}
	return blocks
}
