package session

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

// renderWorkspaceRules renders the always-active workspace rules into a
// <workspace_rules> block, ordered by priority descending then name ascending
// (WARP's resolution order). It returns an empty string when no non-empty
// always-active rules exist.
func renderWorkspaceRules(resolved *warp.ResolvedAgent, cat *workspace.Catalog, globals map[string]any) string {
	type alwaysRule struct {
		rl       *warp.Rule
		rendered string
	}

	var active []alwaysRule
	for i := range resolved.Rules {
		rl := &resolved.Rules[i]
		if !rl.Spec.Triggers.Always {
			continue
		}
		rendered, err := rl.Render(&warp.RuleRenderOptions{
			Workspace: cat.WorkspaceSpec(),
			Project:   cat.Project(),
			Agent:     resolved.Agent,
			Globals:   globals,
		})
		if err != nil {
			rendered = rl.Spec.Instructions
		}
		if strings.TrimSpace(rendered) == "" {
			continue
		}
		active = append(active, alwaysRule{rl: rl, rendered: rendered})
	}
	if len(active) == 0 {
		return ""
	}

	slices.SortFunc(active, func(a, b alwaysRule) int {
		if a.rl.Spec.Priority != b.rl.Spec.Priority {
			return b.rl.Spec.Priority - a.rl.Spec.Priority
		}
		if a.rl.Metadata.Name < b.rl.Metadata.Name {
			return -1
		}
		if a.rl.Metadata.Name > b.rl.Metadata.Name {
			return 1
		}
		return 0
	})

	var b strings.Builder
	b.WriteString("<workspace_rules>\n")
	for _, ar := range active {
		fmt.Fprintf(&b, "<rule name=%q priority=\"%d\">\n%s\n</rule>\n", ar.rl.Metadata.Name, ar.rl.Spec.Priority, strings.TrimSpace(ar.rendered))
	}
	b.WriteString("</workspace_rules>")
	return b.String()
}

func (s *Session) renderSystemPrompt(agentName string) string {
	cat := s.workspace.Catalog()
	var wsPrompt string
	if wsSpec := cat.WorkspaceSpec(); wsSpec != nil {
		wsPrompt, _ = wsSpec.Render(&warp.WorkspaceRenderOptions{})
	}

	if agentName == "" {
		agentName = cat.DefaultAgentName()
	}

	var agentPrompt string
	var resolved *warp.ResolvedAgent
	var globals map[string]any
	if r, err := cat.ResolveAgent(agentName); err == nil && r != nil && r.Agent != nil {
		resolved = r
		globals = cat.AgentRenderGlobals(resolved)
		agentPrompt, _ = resolved.Agent.Render(&warp.AgentRenderOptions{
			Workspace: cat.WorkspaceSpec(),
			Project:   cat.Project(),
			Resolved:  resolved,
			Globals:   globals,
		})
	}

	var parts []string
	if p := strings.TrimSpace(wsPrompt); p != "" {
		parts = append(parts, p)
	}
	if p := strings.TrimSpace(agentPrompt); p != "" {
		parts = append(parts, p)
	}

	// Statically append the always-active workspace rules into a
	// <workspace_rules> block. Conditional rules are surfaced for on-demand
	// loading via <available_rules> instead, so they are not duplicated here.
	// The agent template never emits a rendered block itself, so guard on the
	// block signature (opening tag followed by a newline) to avoid clobbering a
	// template that already renders one, without matching the prose mention of
	// `<workspace_rules>` inside the rules_and_guidelines description.
	if resolved != nil && len(resolved.Rules) > 0 && !strings.Contains(agentPrompt, "<workspace_rules>\n") {
		if block := renderWorkspaceRules(resolved, cat, globals); block != "" {
			parts = append(parts, block)
		}
	}
	if s.ParentID != "" {
		subPrompt := fmt.Sprintf(`<subagent_context>
You are running as a delegated subagent (agent=%q, session_id=%q, work_dir=%q) invoked by a parent agent.
- Your caller is the parent agent, not the human user.
- Execute the delegated task autonomously using your available tools.
- The parent agent does NOT see your intermediate tool calls or thoughts — it only receives your final response when your turn completes.
- When you finish the task, write a clear, self-contained final response summarizing your findings, exact file:line references, or changes and verification results. Your final response is automatically delivered back to the parent agent.
</subagent_context>`, agentName, s.ID, s.WorkDir)
		parts = append(parts, subPrompt)
	}
	return strings.Join(parts, "\n\n")
}

func parseSlashCommandArgs(argsDef []warp.Argument, rawArgs []string) map[string]string {
	argsMap := make(map[string]string)
	var positional []string
	for _, arg := range rawArgs {
		if strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			k := strings.TrimPrefix(parts[0], "--")
			argsMap[k] = parts[1]
		} else {
			positional = append(positional, arg)
		}
	}
	for i, p := range positional {
		argsMap[fmt.Sprintf("%d", i+1)] = p
		if i < len(argsDef) {
			if _, exists := argsMap[argsDef[i].Name]; !exists {
				argsMap[argsDef[i].Name] = p
			}
		}
	}
	return argsMap
}

func prepareSlashArgs(argsDef []warp.Argument, rawArgs []string) ([]string, map[string]any) {
	argsMap := parseSlashCommandArgs(argsDef, rawArgs)
	mergedGlobals := workspace.MergeStringArgs(argsMap, nil)
	positionalArgs := workspace.BuildPositionalArgs(argsDef, argsMap)
	return positionalArgs, mergedGlobals
}

// commandExecution is the resolved execution plan for a slash command. It
// carries the rendered prompt message along with the lifecycle metadata needed
// to decide whether the command runs inline on the current session or in a forked
// subagent session.
type commandExecution struct {
	// Message is the rendered user message to feed into the target turn.
	Message message.Message
	// Name is the command's resource name, used to title forked subagents.
	Name string
	// Resolved is the fully resolved command (skills, rules, tools, agent).
	Resolved *warp.ResolvedCommand
	// ContextMode is the normalized execution context mode.
	ContextMode warp.CommandContextMode
	// TargetAgent is the agent that should execute the command.
	TargetAgent string
	// TargetModel is the resolved model ID for the command, or "" to keep the
	// session's current model.
	TargetModel string
	// Subagent reports whether the command must run in a forked subagent session.
	Subagent bool
}

// resolveCommandExecution resolves a slash command message into its execution
// plan. It returns nil, nil for messages that are not slash commands.
func (s *Session) resolveCommandExecution(msg message.Message) (*commandExecution, error) {
	if msg == nil {
		return nil, nil
	}
	cnt := msg.GetContent()
	text := strings.TrimSpace(cnt.Text())
	if !strings.HasPrefix(text, "/") || len(cnt) > 1 {
		return nil, nil
	}

	fields := strings.Fields(text)
	if len(fields) == 0 {
		return nil, nil
	}

	rawKey := strings.TrimPrefix(fields[0], "/")
	cmdKey := strings.ToLower(rawKey)
	rawArgs := fields[1:]

	cat := s.workspace.Catalog()

	// 1. Try resolving as Command
	if cmd, ok := cat.GetCommand(cmdKey); ok && cmd.Def != nil {
		// Validate command enablement against the active agent and eagerly
		// resolve its skills, rules, and policy-filtered effective tools.
		cmdRef := cmd.Def.QualifiedName()
		if cmdRef == "" {
			cmdRef = cmd.Def.Metadata.Name
		}
		resolvedCmd, err := cat.ResolveCommand(s.Config.Agent, cmdRef)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve command %q: %w", cmd.Name, err)
		}

		// Validate the declared context mode.
		ctxMode, err := validateCommandContext(cmd.Def.Spec.Context)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve command %q: %w", cmd.Name, err)
		}

		// Determine the executing agent: prefer the command's resolved agent,
		// falling back to the session's active agent.
		var activeAgent *warp.Agent
		targetAgent := s.Config.Agent
		if resolvedCmd != nil && resolvedCmd.Agent != nil && resolvedCmd.Agent.Agent != nil {
			activeAgent = resolvedCmd.Agent.Agent
			targetAgent = resolvedCmd.Agent.Agent.Metadata.Name
		} else if resolved, rerr := cat.ResolveAgent(s.Config.Agent); rerr == nil && resolved != nil {
			activeAgent = resolved.Agent
		}

		// Resolve the target model from the command's model preferences.
		targetModel := s.resolveCommandModel(cmd.Def.Spec.Models)

		// A command runs in a forked subagent when it requests clean/fork
		// context, or when it targets a different agent than the session.
		subagent := ctxMode == warp.CommandContextClean || ctxMode == warp.CommandContextFork ||
			(cmd.Def.Spec.Agent != "" && !strings.EqualFold(cmd.Def.Spec.Agent, s.Config.Agent))

		positionalArgs, mergedGlobals := prepareSlashArgs(cmd.Def.Spec.Arguments, rawArgs)
		rendered, err := cmd.Def.Render(&warp.CommandRenderOptions{
			Workspace: cat.WorkspaceSpec(),
			Project:   cat.Project(),
			Agent:     activeAgent,
			Resolved:  resolvedCmd,
			Args:      positionalArgs,
			Globals:   mergedGlobals,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to render command %q: %w", cmd.Name, err)
		}

		// Eagerly embed the command's declared skills and rules into the prompt
		// text, prepending the rendered blocks ahead of the command body.
		promptText := rendered
		if resolvedCmd != nil && (len(resolvedCmd.Skills) > 0 || len(resolvedCmd.Rules) > 0) {
			var b strings.Builder
			for _, sk := range resolvedCmd.Skills {
				skRendered, rerr := sk.Render(&warp.SkillRenderOptions{
					Workspace: cat.WorkspaceSpec(),
					Project:   cat.Project(),
					Agent:     activeAgent,
				})
				if rerr != nil {
					return nil, fmt.Errorf("failed to render skill %q for command %q: %w", sk.Metadata.Name, cmd.Name, rerr)
				}
				skillPath := s.workspace.PortablePath(sk.Directory)
				fmt.Fprintf(&b, "<embedded_skill name=%q path=%q>\n<instructions>\n%s\n</instructions>\n</embedded_skill>\n", sk.Metadata.Name, skillPath, skRendered)
			}
			for _, rl := range resolvedCmd.Rules {
				rlRendered, rerr := rl.Render(&warp.RuleRenderOptions{
					Workspace: cat.WorkspaceSpec(),
					Project:   cat.Project(),
					Agent:     activeAgent,
				})
				if rerr != nil {
					return nil, fmt.Errorf("failed to render rule %q for command %q: %w", rl.Metadata.Name, cmd.Name, rerr)
				}
				rulePath := s.workspace.PortablePath(rl.GetPath())
				if rulePath == "" {
					rulePath = s.workspace.PortablePath(rl.Directory)
				}
				fmt.Fprintf(&b, "<embedded_rule name=%q path=%q>\n<instructions>\n%s\n</instructions>\n</embedded_rule>\n", rl.Metadata.Name, rulePath, rlRendered)
			}
			promptText = b.String() + rendered
		}

		// Activate the command's policy-allowed deferred tools into the model's
		// context via tool-reference blocks (deferred, registered tools) and
		// inline tool-definition blocks (policy-allowed but unregistered tools).
		var resolvedTools []*warp.Tool
		if resolvedCmd != nil {
			resolvedTools = resolvedCmd.Tools
		}
		content := make(message.Content, 0, len(resolvedTools)+1)
		content = append(content, &message.TextBlock{Text: promptText})
		content = append(content, tools.ActivationBlocks(s.Tools, cat, resolvedTools)...)

		newMsg := message.NewUser(content...)
		if msg.GetID() != "" {
			newMsg.SetID(msg.GetID())
		}

		return &commandExecution{
			Message:     newMsg,
			Name:        cmd.Name,
			Resolved:    resolvedCmd,
			ContextMode: ctxMode,
			TargetAgent: targetAgent,
			TargetModel: targetModel,
			Subagent:    subagent,
		}, nil
	}

	// 2. Try resolving as Skill
	if sk, ok := cat.GetSkill(cmdKey); ok && sk.Def != nil {
		// Resolve the active session agent so the skill template can reference
		// {{.Agent.*}} fields.
		var activeAgent *warp.Agent
		if resolved, err := cat.ResolveAgent(s.Config.Agent); err == nil && resolved != nil {
			activeAgent = resolved.Agent
		}

		positionalArgs, mergedGlobals := prepareSlashArgs(sk.Def.Spec.Arguments, rawArgs)
		rendered, err := sk.Def.Render(&warp.SkillRenderOptions{
			Workspace: cat.WorkspaceSpec(),
			Project:   cat.Project(),
			Agent:     activeAgent,
			Args:      positionalArgs,
			Globals:   mergedGlobals,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to render skill %q: %w", sk.Name, err)
		}

		skillPath := s.workspace.PortablePath(sk.Def.Directory)
		pathAttr := ""
		pathNote := ""
		if skillPath != "" {
			pathAttr = fmt.Sprintf(" path=%q", skillPath)
			pathNote = fmt.Sprintf("\nSupporting assets and files for this skill are located in %q.", skillPath)
		}

		promptText := fmt.Sprintf("<embedded_skill name=%q%s>\n<instructions>\n%s\n</instructions>\n</embedded_skill>\n\nThe user invoked: %s%s\nFollow the skill instructions above.", sk.Name, pathAttr, rendered, text, pathNote)

		// Activate the skill's policy-allowed deferred tools into the model's
		// context by appending a tool-reference block for each deferred tool and
		// an inline tool-definition block for any policy-allowed tool that is not
		// registered in the session container.
		skillTools, _ := cat.ToolsForSkill(s.Config.Agent, sk.Def.QualifiedName())
		content := make(message.Content, 0, len(skillTools)+1)
		content = append(content, &message.TextBlock{Text: promptText})
		content = append(content, tools.ActivationBlocks(s.Tools, cat, skillTools)...)

		newMsg := &message.User{
			Base: message.Base{Metadata: map[string]any{
				"skill":   sk.Name,
				"path":    skillPath,
				"command": text,
			}},
			Content: content,
		}
		if msg.GetID() != "" {
			newMsg.SetID(msg.GetID())
		}
		return &commandExecution{Message: newMsg}, nil
	}

	return nil, nil
}

// validateCommandContext normalizes and validates a command's declared context
// mode. An empty value defaults to inline.
func validateCommandContext(mode warp.CommandContextMode) (warp.CommandContextMode, error) {
	switch mode {
	case "", warp.CommandContextInline:
		return warp.CommandContextInline, nil
	case warp.CommandContextFork, warp.CommandContextClean:
		return mode, nil
	default:
		return "", fmt.Errorf("%w: unsupported command context mode %q", ErrInvalidConfig, string(mode))
	}
}

// resolveCommandModel resolves the first available completion model from a
// command's model preferences. It returns "" when no preference is declared or
// none resolves, signalling that the session's current model should be kept.
func (s *Session) resolveCommandModel(modelRefs []string) string {
	if len(modelRefs) == 0 || s.catalog == nil {
		return ""
	}
	if resolved, ok := s.catalog().Resolve(models.RoleCompletion, modelRefs...); ok {
		return string(resolved.ID)
	}
	return ""
}

func (s *Session) resolveSlashCommand(msg message.Message) (message.Message, error) {
	exec, err := s.resolveCommandExecution(msg)
	if err != nil {
		return msg, err
	}
	if exec == nil {
		return msg, nil
	}
	return exec.Message, nil
}

func (s *Session) generateTitle(userText string) {
	ctx, cancel := context.WithTimeout(s.bgCtx, 30*time.Second)
	defer cancel()

	// Release the in-flight latch when this attempt produces no title so that a
	// later user message can retry (e.g. after a transient timeout or provider
	// error) instead of leaving the session permanently untitled.
	defer func() {
		s.Lock()
		if strings.TrimSpace(s.Title) == "" {
			s.titleGenerating = false
		}
		s.Unlock()
	}()

	var sysPrompt string
	var llmModel *llm.Model
	ready := func() bool {
		s.reloadLock.RLock()
		defer s.reloadLock.RUnlock()

		cat := s.workspace.Catalog()
		resolvedTitle, err := cat.ResolveAgent(workspace.AgentNameTitleGenerator)
		if err != nil || resolvedTitle == nil || resolvedTitle.Agent == nil {
			if err != nil {
				s.logger.Warn("failed to resolve title generator agent", log.String("error", err.Error()))
			}
			return false
		}
		sysPrompt, err = resolvedTitle.Agent.Render(&warp.AgentRenderOptions{
			Workspace: cat.WorkspaceSpec(),
			Project:   cat.Project(),
			Resolved:  resolvedTitle,
		})
		if err != nil || strings.TrimSpace(sysPrompt) == "" {
			if err != nil {
				s.logger.Warn("failed to render title generator prompt", log.String("error", err.Error()))
			}
			return false
		}

		catalog := s.catalog()
		var effective models.Model
		var found bool
		if len(resolvedTitle.Agent.Spec.Models) > 0 {
			effective, found = catalog.Resolve(models.RoleCompletion, resolvedTitle.Agent.Spec.Models...)
		}
		if !found {
			s.Lock()
			modelName := models.ID(s.Config.Model)
			s.Unlock()
			effective, found = catalog.Get(modelName)
		}
		if !found {
			return false
		}
		settings := s.modelSettings(api.SessionConfig{Agent: workspace.AgentNameTitleGenerator})
		cfg, _, err := effective.Configure(settings)
		if err == nil {
			llmModel, err = s.gateway().LLM(ctx, effective, cfg)
		}
		if err != nil {
			s.logger.Warn("failed to resolve LLM for title generation", log.String("error", err.Error()))
			return false
		}
		return true
	}()
	if !ready {
		return
	}

	msgs := []message.Message{
		message.NewSystemText(strings.TrimSpace(sysPrompt)),
		message.NewUserText(userText),
	}
	s.Lock()
	sessionAgent := s.Config.Agent
	s.Unlock()
	titleCtx := scope.With(ctx, scope.Scope{
		SessionID:     s.ID,
		WorkspaceRoot: s.workspace.RootDir(),
		Agent:         sessionAgent,
		Purpose:       string(metrics.PurposeTitle),
	})
	resp, err := llmModel.Invoke(titleCtx, msgs)
	if err != nil || resp == nil {
		if err != nil {
			s.logger.Warn("failed to invoke title generator model", log.String("error", err.Error()))
		}
		return
	}

	textContent := resp.GetContent().Text()

	var title string
	for line := range strings.SplitSeq(textContent, "\n") {
		line = strings.TrimSpace(line)
		line = strings.Trim(line, "\"'`")
		line = strings.TrimSpace(line)
		if line != "" {
			title = line
			break
		}
	}

	runes := []rune(title)
	if len(runes) > 50 {
		title = strings.TrimSpace(string(runes[:50]))
	}

	if title == "" {
		return
	}

	now := time.Now().UTC()

	s.Lock()
	if strings.TrimSpace(s.Title) != "" {
		s.Unlock()
		return
	}
	prevTitle := s.Title
	prevUpdated := s.UpdatedAt
	s.Title = title
	s.UpdatedAt = now
	s.Unlock()

	if err := s.checkpoint(ctx); err != nil {
		s.Lock()
		s.Title = prevTitle
		s.UpdatedAt = prevUpdated
		s.Unlock()
		s.logger.Warn("failed to persist generated title", log.String("error", err.Error()))
		return
	}

	s.publisher.Publish(api.TopicSessions, api.SessionUpdatedEvent{
		Workspace: s.workspace.RootDir(),
		Session:   ToAPISession(s),
	})
}

func modelSettings(ws *workspace.Workspace, config api.SessionConfig) models.Settings {
	settings := models.Settings{
		Thinking:       config.Thinking,
		StrictThinking: config.Thinking != nil,
	}
	if ws != nil && ws.Catalog() != nil {
		if agent, ok := ws.Catalog().GetAgent(config.Agent); ok {
			if agent.Def != nil && agent.Def.Spec.Temperature != nil {
				value := float32(*agent.Def.Spec.Temperature)
				settings.Temperature = &value
			}
			if settings.Thinking == nil {
				settings.Thinking = agent.Thinking
				settings.StrictThinking = false
			}
		}
	}
	return settings
}

func (s *Session) modelSettings(config api.SessionConfig) models.Settings {
	return modelSettings(s.workspace, config)
}

func resolveModelConfig(ws *workspace.Workspace, catalog *models.Catalog, config api.SessionConfig) (llm.ModelConfig, []api.SessionDiagnostic, error) {
	if catalog == nil {
		return llm.ModelConfig{}, nil, fmt.Errorf("session: catalog unavailable")
	}
	model, ok := catalog.Get(models.ID(config.Model))
	if !ok {
		return llm.ModelConfig{}, nil, fmt.Errorf("session: model %q unavailable", config.Model)
	}
	return model.Configure(modelSettings(ws, config))
}

func (s *Session) resolveModelConfig(config api.SessionConfig) (llm.ModelConfig, []api.SessionDiagnostic, error) {
	var cat *models.Catalog
	if s.catalog != nil {
		cat = s.catalog()
	}
	return resolveModelConfig(s.workspace, cat, config)
}

func modelDiagnostics(ws *workspace.Workspace, catalog *models.Catalog, config api.SessionConfig) []api.SessionDiagnostic {
	_, diagnostics, err := resolveModelConfig(ws, catalog, config)
	for i := range diagnostics {
		diagnostics[i].Message = fmt.Sprintf("Agent %q, model %q: %s", config.Agent, config.Model, diagnostics[i].Message)
	}
	if err != nil && config.Thinking != nil && errors.Is(err, models.ErrInvalidConfig) {
		return append(diagnostics, api.SessionDiagnostic{Code: "thinking_override_unsupported", Setting: "thinking", Message: fmt.Sprintf("The thinking override is unavailable for model %q: %v", config.Model, err)})
	}
	if err != nil {
		return []api.SessionDiagnostic{{Code: "model_config_unavailable", Message: fmt.Sprintf("Model configuration is unavailable: %v", err)}}
	}
	// Emit a diagnostic when the active agent loads more than the recommended
	// number of eager tools on the first request.
	if cat := ws.Catalog(); cat != nil {
		agentName := config.Agent
		if agentName == "" {
			agentName = cat.DefaultAgentName()
		}
		if resolved, rerr := cat.ResolveAgent(agentName); rerr == nil && resolved != nil {
			if partition := cat.PartitionAgentTools(resolved); len(partition.Eager) > workspace.MaxRecommendedEagerTools {
				diagnostics = append(diagnostics, api.SessionDiagnostic{Code: "too_many_eager_tools", Setting: "tools", Message: fmt.Sprintf("Agent %q loads %d tools on the first request (recommended maximum is %d); consider reducing spec.tools to use deferred loading", agentName, len(partition.Eager), workspace.MaxRecommendedEagerTools)})
			}
		}
	}
	return diagnostics
}

func (s *Session) modelDiagnostics(config api.SessionConfig) []api.SessionDiagnostic {
	var cat *models.Catalog
	if s.catalog != nil {
		cat = s.catalog()
	}
	return modelDiagnostics(s.workspace, cat, config)
}

// SessionDir returns the session data directory for the given workspace root and session ID.
func SessionDir(workspaceRoot, sessionID string) (string, error) {
	wsDir, err := xdg.WorkspaceDir(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("session: resolve workspace data dir: %w", err)
	}
	return filepath.Join(wsDir, "sessions", filepath.Base(sessionID)), nil
}

// resolveAgentModel resolves the effective agent name and model ID given the workspace and catalog.
func resolveAgentModel(ws *workspace.Workspace, catalog *models.Catalog, agent string, model models.ID) (string, models.ID) {
	var cat *workspace.Catalog
	if ws != nil {
		cat = ws.Catalog()
	}
	if cat == nil {
		return agent, model
	}
	agent = strings.TrimSpace(agent)
	if agent == "" {
		agent = cat.DefaultAgentName()
	}
	model = models.ID(strings.TrimSpace(string(model)))
	if model == "" {
		if resolvedAgent, err := cat.ResolveAgent(agent); err == nil && resolvedAgent != nil && resolvedAgent.Agent != nil && len(resolvedAgent.Agent.Spec.Models) > 0 {
			if catalog != nil {
				if resolved, ok := catalog.Resolve(models.RoleCompletion, resolvedAgent.Agent.Spec.Models...); ok {
					return agent, resolved.ID
				}
			}
		}
	}
	var explicit []string
	if model != "" {
		explicit = []string{string(model)}
	}
	if catalog != nil {
		if resolved, ok := catalog.Resolve(models.RoleCompletion, explicit...); ok {
			model = resolved.ID
		} else if model != "" {
			if resolved, ok := catalog.Get(model); ok {
				model = resolved.ID
			}
		}
	}
	return agent, model
}

// validateCompletionModel verifies that the resolved model supports chat completion.
func validateCompletionModel(catalog *models.Catalog, modelID models.ID) error {
	if modelID.Valid() && catalog != nil {
		if resolved, ok := catalog.Get(modelID); ok && !resolved.SupportsCompletion() {
			return fmt.Errorf("%w: model %q does not support chat completion", ErrInvalidConfig, modelID)
		}
	}
	return nil
}

// validateThinking validates that the model supports the given thinking settings.
func validateThinking(catalog *models.Catalog, model models.ID, setting *api.ThinkingSetting) error {
	if catalog == nil {
		return fmt.Errorf("%w: catalog unavailable", ErrInvalidConfig)
	}
	resolved, ok := catalog.Get(model)
	if !ok {
		return fmt.Errorf("%w: model %q unavailable", ErrInvalidConfig, model)
	}
	_, _, err := resolved.Configure(models.Settings{Thinking: setting, StrictThinking: true})
	return err
}

// resolveConfig validates config and resolves its agent and model, defaulting
// the permission mode. A "default" thinking setting is normalized to nil.
func resolveConfig(ws *workspace.Workspace, catalog *models.Catalog, config api.SessionConfig) (api.SessionConfig, error) {
	mode := cmp.Or(config.Mode, api.ModeDefault)
	if ws != nil && ws.IsRestricted() {
		mode = api.ModePlan
	}
	if !mode.IsValid() {
		return api.SessionConfig{}, fmt.Errorf("%w: unsupported permission mode %q", ErrInvalidConfig, config.Mode)
	}

	agent, modelID := resolveAgentModel(ws, catalog, config.Agent, models.ID(config.Model))
	if err := validateCompletionModel(catalog, modelID); err != nil {
		return api.SessionConfig{}, err
	}
	if config.Thinking != nil {
		if err := validateThinking(catalog, modelID, config.Thinking); err != nil {
			return api.SessionConfig{}, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
		}
		if config.Thinking.Type == "default" {
			config.Thinking = nil
		}
	}
	return api.SessionConfig{Agent: agent, Model: string(modelID), Thinking: config.Thinking, Mode: mode}, nil
}

// Preview computes the resolved session configuration, model details, and diagnostics
// for a proposed session configuration without creating a session.
func Preview(ctx context.Context, ws *workspace.Workspace, catalog *models.Catalog, config api.SessionConfig) (api.SessionConfig, models.Model, []api.SessionDiagnostic, bool, error) {
	if err := ctx.Err(); err != nil {
		return api.SessionConfig{}, models.Model{}, nil, false, err
	}
	resolvedConfig, err := resolveConfig(ws, catalog, config)
	if err != nil {
		return api.SessionConfig{}, models.Model{}, nil, false, err
	}
	if err := validateHumanAgentTrigger(ws, resolvedConfig.Agent); err != nil {
		return api.SessionConfig{}, models.Model{}, nil, false, err
	}
	modelID := models.ID(resolvedConfig.Model)

	diagnostics := modelDiagnostics(ws, catalog, resolvedConfig)

	var resolvedModel models.Model
	var found bool
	if modelID.Valid() && catalog != nil {
		resolvedModel, found = catalog.Get(modelID)
	}
	return resolvedConfig, resolvedModel, diagnostics, found, nil
}

// validateHumanAgentTrigger returns an error when a top-level human session
// attempts to use an agent whose triggers exclude "human". Subagent sessions
// (which set a Parent or ParentID) are not subject to this check.
func validateHumanAgentTrigger(ws *workspace.Workspace, agentName string) error {
	if ws == nil || ws.Catalog() == nil || strings.TrimSpace(agentName) == "" {
		return nil
	}
	snap, ok := ws.Catalog().GetAgent(strings.TrimSpace(agentName))
	if ok && !snap.CanHumanTrigger() {
		return fmt.Errorf("%w: agent %q cannot be triggered by human (triggers: %s)", ErrInvalidConfig, snap.Name, strings.Join(snap.Triggers, ", "))
	}
	return nil
}
