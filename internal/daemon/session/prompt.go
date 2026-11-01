package session

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
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
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

func (s *Session) renderSystemPrompt(agentName string) string {
	cat := s.workspace.Catalog()
	var wsPrompt string
	if wsSpec := cat.WorkspaceSpec(); wsSpec != nil {
		wsPrompt, _ = wsSpec.Render(&warp.WorkspaceRenderOptions{})
	}

	if agentName == "" {
		agentName = cat.DefaultAgentName()
	}

	globals := map[string]any{
		"HasTool": func(toolName string) bool {
			if resolved, err := cat.ResolveAgent(agentName); err == nil && resolved != nil {
				for _, t := range resolved.Tools {
					if strings.EqualFold(t.Metadata.Name, toolName) {
						return true
					}
				}
			}
			return false
		},
	}

	var agentPrompt string
	if resolved, err := cat.ResolveAgent(agentName); err == nil && resolved != nil && resolved.Agent != nil {
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

func (s *Session) resolveSlashCommand(msg message.Message) (message.Message, error) {
	if msg == nil {
		return nil, nil
	}
	cnt := msg.GetContent()
	text := strings.TrimSpace(cnt.Text())
	if !strings.HasPrefix(text, "/") || len(cnt) > 1 {
		return msg, nil
	}

	fields := strings.Fields(text)
	if len(fields) == 0 {
		return msg, nil
	}

	rawKey := strings.TrimPrefix(fields[0], "/")
	cmdKey := strings.ToLower(rawKey)
	rawArgs := fields[1:]

	cat := s.workspace.Catalog()

	// 1. Try resolving as Command
	if cmd, ok := cat.GetCommand(cmdKey); ok && cmd.Def != nil {
		positionalArgs, mergedGlobals := prepareSlashArgs(cmd.Def.Spec.Arguments, rawArgs)
		rendered, err := cmd.Def.Render(&warp.CommandRenderOptions{
			Workspace: cat.WorkspaceSpec(),
			Project:   cat.Project(),
			Args:      positionalArgs,
			Globals:   mergedGlobals,
		})
		if err != nil {
			return msg, fmt.Errorf("failed to render command %q: %w", cmd.Name, err)
		}
		newMsg := message.NewUser(&message.TextBlock{Text: rendered})
		if msg.GetID() != "" {
			newMsg.SetID(msg.GetID())
		}
		return newMsg, nil
	}

	// 2. Try resolving as Skill
	if sk, ok := cat.GetSkill(cmdKey); ok && sk.Def != nil {
		positionalArgs, mergedGlobals := prepareSlashArgs(sk.Def.Spec.Arguments, rawArgs)
		rendered, err := sk.Def.Render(&warp.SkillRenderOptions{
			Workspace: cat.WorkspaceSpec(),
			Project:   cat.Project(),
			Args:      positionalArgs,
			Globals:   mergedGlobals,
		})
		if err != nil {
			return msg, fmt.Errorf("failed to render skill %q: %w", sk.Name, err)
		}

		skillPath := s.workspace.PortablePath(sk.Def.Directory)
		pathAttr := ""
		pathNote := ""
		if skillPath != "" {
			pathAttr = fmt.Sprintf(" path=%q", skillPath)
			pathNote = fmt.Sprintf("\nSupporting assets and files for this skill are located in %q.", skillPath)
		}

		promptText := fmt.Sprintf("<embedded_skill name=%q%s>\n<instructions>\n%s\n</instructions>\n</embedded_skill>\n\nThe user invoked: %s%s\nFollow the skill instructions above.", sk.Name, pathAttr, rendered, text, pathNote)

		newMsg := message.NewUserTextMeta(promptText, map[string]any{
			"skill":   sk.Name,
			"path":    skillPath,
			"command": text,
		})
		if msg.GetID() != "" {
			newMsg.SetID(msg.GetID())
		}
		return newMsg, nil
	}

	return msg, nil
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
		}
	}
	return agent, model
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
	if !mode.IsValid() {
		return api.SessionConfig{}, fmt.Errorf("%w: unsupported permission mode %q", ErrInvalidConfig, config.Mode)
	}

	agent, modelID := resolveAgentModel(ws, catalog, config.Agent, models.ID(config.Model))
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
	modelID := models.ID(resolvedConfig.Model)

	diagnostics := modelDiagnostics(ws, catalog, resolvedConfig)

	var resolvedModel models.Model
	var found bool
	if modelID.Valid() && catalog != nil {
		resolvedModel, found = catalog.Get(modelID)
	}
	return resolvedConfig, resolvedModel, diagnostics, found, nil
}
