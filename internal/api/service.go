package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/internal/agent/model"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/agent/resolver"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
	"github.com/masterkeysrd/tasksmith/internal/filetrack"
	"github.com/masterkeysrd/tasksmith/internal/mcp"
	"github.com/masterkeysrd/tasksmith/internal/metrics"
	"github.com/masterkeysrd/tasksmith/internal/session"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
	"github.com/masterkeysrd/warp"
	"github.com/modelcontextprotocol/go-sdk/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type Workspace interface {
	Projects() []*warp.Project
	Agents() []*warp.Agent
	Providers() []*warp.ModelProvider
	ProvidersPresets() []*warp.ModelProvider
	ToolsPresets() []*warp.Tool
	MCPs() []*warp.MCP
	Initialize(ctx context.Context, opts workspace.InitializationOptions) error
	GetWorkspaceConfig(ctx context.Context) (workspace.WorkspaceConfig, error)
	ResolveDefaults(ctx context.Context) (agentName, providerName, modelName string, err error)
	ResolveAgent(ctx context.Context, ref string) (*warp.ResolvedAgent, error)
	AuthorizeTools(ctx context.Context, tools []string) error
	ListResources(opts warp.QueryOptions) []warp.Resource
}

// Service provides methods to interact with the workspace through API types.
type Service struct {
	ws           Workspace
	sm           *session.Manager
	metricsStore *metrics.Store
	lspManager   *lsp.Manager
	fileTracker  filetrack.WorkspaceTracker
}

// NewService creates a new API service.
func NewService(
	ws Workspace,
	sm *session.Manager,
	metricsStore *metrics.Store,
	lspManager *lsp.Manager,
	fileTracker filetrack.WorkspaceTracker,
) *Service {
	return &Service{
		ws:           ws,
		sm:           sm,
		metricsStore: metricsStore,
		lspManager:   lspManager,
		fileTracker:  fileTracker,
	}
}

func (s *Service) newPermissionManager(ctx context.Context, sessionID string) (*permissions.FSManager, error) {
	cwd := ""
	var isConfigured bool
	if s.ws != nil {
		if cfg, err := s.ws.GetWorkspaceConfig(ctx); err == nil {
			cwd = cfg.CWD
			isConfigured = cfg.IsConfigured
		}
	}
	pm, err := permissions.NewFSManager(cwd, sessionID)
	if err != nil {
		return nil, err
	}
	pm.SetWorkspaceInitializedFn(func() bool {
		return isConfigured
	})
	return pm, nil
}

// GetWorkspaceConfig returns the workspace configuration.
func (s *Service) GetWorkspaceConfig(ctx context.Context, req GetWorkspaceConfigRequest) (*GetWorkspaceConfigResponse, error) {
	cfg, err := s.ws.GetWorkspaceConfig(ctx)
	if err != nil {
		return nil, err
	}
	return &GetWorkspaceConfigResponse{
		Name:            cfg.Name,
		DefaultProvider: cfg.DefaultProvider,
		AuthorizedTools: cfg.AuthorizedTools,
		IsConfigured:    cfg.IsConfigured,
		IsTrusted:       cfg.IsTrusted,
		HasManifest:     cfg.HasManifest,
		CWD:             cfg.CWD,
	}, nil
}

// InitializeWorkspace initializes the workspace with configuration files, theme, and providers.
func (s *Service) InitializeWorkspace(ctx context.Context, req InitializeWorkspaceRequest) (*InitializeWorkspaceResponse, error) {
	opts := workspace.InitializationOptions{
		ProjectName:      req.ProjectName,
		SelectedProvider: req.SelectedProvider,
		APIKey:           req.APIKey,
		Endpoint:         req.Endpoint,
		DefaultModel:     req.DefaultModel,
		Theme:            req.Theme,
		AuthorizedTools:  req.AuthorizedTools,
		TrustOnly:        req.TrustOnly,
		AuthType:         req.AuthType,
		AuthEnv:          req.AuthEnv,
		AuthFile:         req.AuthFile,
		AuthHeader:       req.AuthHeader,
	}
	if err := s.ws.Initialize(ctx, opts); err != nil {
		return nil, err
	}
	return &InitializeWorkspaceResponse{Success: true}, nil
}

// AuthorizeWorkspaceTools adds tools to the workspace allowed tools configuration.
func (s *Service) AuthorizeWorkspaceTools(ctx context.Context, req AuthorizeWorkspaceToolsRequest) (*AuthorizeWorkspaceToolsResponse, error) {
	if err := s.ws.AuthorizeTools(ctx, req.Tools); err != nil {
		return nil, err
	}
	return &AuthorizeWorkspaceToolsResponse{Success: true}, nil
}

// ListProjects returns a list of projects in the workspace.
func (s *Service) ListProjects(ctx context.Context, req ListProjectsRequest) (*ListProjectsResponse, error) {
	projects := s.ws.Projects()
	resp := &ListProjectsResponse{
		Projects: make([]Project, 0, len(projects)),
	}

	for _, p := range projects {
		resp.Projects = append(resp.Projects, Project{
			Name:        p.Name,
			DisplayName: p.Name,
			Path:        p.Path,
		})
	}

	sort.Slice(resp.Projects, func(i, j int) bool {
		return resp.Projects[i].Name < resp.Projects[j].Name
	})

	return resp, nil
}

// ListAgents returns a list of agents in the workspace.
func (s *Service) ListAgents(ctx context.Context, req ListAgentsRequest) (*ListAgentsResponse, error) {
	defaultAgent, _, _, _ := s.ws.ResolveDefaults(ctx)

	resources := s.ws.ListResources(warp.QueryOptions{
		Kinds: []warp.Kind{warp.KindAgent},
		Filter: func(r warp.Resource) bool {
			a, ok := r.(*warp.Agent)
			if !ok {
				return false
			}
			// Filter out system-provided internal agents unless explicitly requested, but keep the resolved default agent
			if !req.IncludeSystem && a.GetNamespace() == "system" && a.Metadata.Name != defaultAgent {
				return false
			}

			// Filter agents by trigger: if triggers list is defined, it must contain "human"
			if len(a.Spec.Triggers) > 0 {
				hasHuman := slices.Contains(a.Spec.Triggers, "human")
				if !hasHuman && a.Metadata.Name != defaultAgent {
					return false
				}
			}
			return true
		},
	})

	resp := &ListAgentsResponse{
		Agents: make([]Agent, 0, len(resources)),
	}

	for _, r := range resources {
		a := r.(*warp.Agent)

		var tools []string
		var skills []string
		var subagents []string
		var models []string
		temperature := a.Spec.Temperature

		resolved, err := s.ws.ResolveAgent(ctx, a.Metadata.Name)
		if err == nil && resolved != nil {
			if len(resolved.Tools) > 0 {
				tools = make([]string, len(resolved.Tools))
				for idx, t := range resolved.Tools {
					tools[idx] = t.GetName()
				}
			}
			if len(resolved.Skills) > 0 {
				skills = make([]string, len(resolved.Skills))
				for idx, sk := range resolved.Skills {
					skills[idx] = sk.GetName()
				}
			}
			if len(resolved.Subagents) > 0 {
				subagents = make([]string, len(resolved.Subagents))
				for idx, sub := range resolved.Subagents {
					subagents[idx] = sub.GetName()
				}
			}
			if resolved.Agent != nil {
				models = resolved.Agent.Spec.Models
				temperature = resolved.Agent.Spec.Temperature
			}
		} else {
			if a.Spec.Policies != nil && a.Spec.Policies.Tools != nil {
				tools = a.Spec.Policies.Tools.Include
			}
			models = a.Spec.Models
			skills = a.Spec.Skills
			subagents = a.Spec.Subagents
		}

		sort.Strings(tools)
		sort.Strings(skills)
		sort.Strings(subagents)

		resp.Agents = append(resp.Agents, Agent{
			Name:         a.Metadata.Name,
			Description:  a.Metadata.Description,
			Instructions: a.Spec.Instructions,
			Models:       models,
			Temperature:  temperature,
			Tools:        tools,
			Skills:       skills,
			Subagents:    subagents,
			Triggers:     a.Spec.Triggers,
		})
	}

	sort.Slice(resp.Agents, func(i, j int) bool {
		// Keep the resolved default agent as the first agent
		if resp.Agents[i].Name == defaultAgent {
			return true
		}
		if resp.Agents[j].Name == defaultAgent {
			return false
		}
		return resp.Agents[i].Name < resp.Agents[j].Name
	})

	return resp, nil
}

// ListSkills returns a list of skills the active agent has access to.
func (s *Service) ListSkills(ctx context.Context, req ListSkillsRequest) (*ListSkillsResponse, error) {
	agentName := "main"
	if s.sm != nil && req.SessionID != "" {
		if sd, err := s.sm.GetSession(ctx, req.SessionID); err == nil && sd != nil {
			if sd.Settings.AgentName != "" {
				agentName = sd.Settings.AgentName
			}
		}
	}

	var skills []SkillItem
	if ws, ok := s.ws.(*workspace.Workspace); ok {
		if resolvedAgent, err := ws.ResolveAgent(ctx, agentName); err == nil && resolvedAgent != nil {
			for _, skill := range resolvedAgent.Skills {
				skills = append(skills, SkillItem{
					Name:        skill.Metadata.Name,
					Description: skill.Metadata.Description,
				})
			}
		}
	}

	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})

	return &ListSkillsResponse{
		Skills: skills,
	}, nil
}

// ListProviders returns a list of model providers in the workspace.
func (s *Service) ListProviders(ctx context.Context, req ListProvidersRequest) (*ListProvidersResponse, error) {
	providers := s.ws.Providers()
	cfg, _ := s.ws.GetWorkspaceConfig(ctx)

	resp := &ListProvidersResponse{
		Providers: make([]Provider, 0, len(providers)),
	}
	if cfg.DefaultProvider != "" {
		resp.DefaultProvider = cfg.DefaultProvider
	}

	for _, p := range providers {
		displayName := p.Metadata.DisplayName
		if displayName == "" {
			displayName = p.Metadata.Name
		}

		// Try to instantiate the Loom provider to fetch model profiles/capabilities
		var loomProvider llm.Provider
		if lp, err := model.CreateProvider(ctx, p); err == nil {
			loomProvider = lp
		}

		models := make([]Model, 0, len(p.Spec.Models))
		for _, m := range p.Spec.Models {
			var family string
			var openWeights bool
			var caps ModelCapabilities
			var pricing ModelPricing
			var modalities ModelModalities

			knowledgeCutoff := "n/a"
			lastUpdated := "n/a"

			if loomProvider != nil {
				if prof, ok := loomProvider.GetProfile(m.ID); ok {
					family = prof.Family
					openWeights = prof.OpenWeights
					if prof.Knowledge != "" {
						knowledgeCutoff = prof.Knowledge
					}
					if prof.LastUpdated != "" {
						lastUpdated = prof.LastUpdated
					}
					caps = ModelCapabilities{
						Attachment:  prof.Capabilities.Attachment,
						Reasoning:   prof.Capabilities.Reasoning,
						ToolCall:    prof.Capabilities.ToolCall,
						Temperature: prof.Capabilities.Temperature,
					}
					if len(prof.Capabilities.ReasoningOptions) > 0 {
						caps.ReasoningOptions = make([]ModelReasoningOption, 0, len(prof.Capabilities.ReasoningOptions))
						for _, ro := range prof.Capabilities.ReasoningOptions {
							caps.ReasoningOptions = append(caps.ReasoningOptions, ModelReasoningOption{
								Type:   ro.Type,
								Values: ro.Values,
							})
						}
					}
					pricing = ModelPricing{
						Input:      prof.Pricing.Input,
						Output:     prof.Pricing.Output,
						CacheRead:  prof.Pricing.CacheRead,
						CacheWrite: prof.Pricing.CacheWrite,
						Reasoning:  prof.Pricing.Reasoning,
					}
					if len(prof.Pricing.TieredLimits) > 0 {
						pricing.TieredLimits = make([]TierPricing, 0, len(prof.Pricing.TieredLimits))
						for _, tp := range prof.Pricing.TieredLimits {
							pricing.TieredLimits = append(pricing.TieredLimits, TierPricing{
								Input:      tp.Input,
								Output:     tp.Output,
								CacheRead:  tp.CacheRead,
								CacheWrite: tp.CacheWrite,
								Reasoning:  tp.Reasoning,
								TierLimit:  tp.TierLimit,
							})
						}
					}
					for _, mod := range prof.Modalities.Inputs {
						modalities.Inputs = append(modalities.Inputs, string(mod))
					}
					for _, mod := range prof.Modalities.Outputs {
						modalities.Outputs = append(modalities.Outputs, string(mod))
					}
				}
			}

			contextWindow := m.Limits.Context
			maxOutputTokens := m.Limits.Output
			// Fallback to Loom profile limits if not configured in Warp
			if contextWindow == 0 && loomProvider != nil {
				if prof, ok := loomProvider.GetProfile(m.ID); ok {
					contextWindow = prof.Limits.Context
				}
			}
			if maxOutputTokens == 0 && loomProvider != nil {
				if prof, ok := loomProvider.GetProfile(m.ID); ok {
					maxOutputTokens = prof.Limits.Output
				}
			}

			label := m.Label
			if label == "" && loomProvider != nil {
				if prof, ok := loomProvider.GetProfile(m.ID); ok && prof.Name != "" {
					label = prof.Name
				}
			}
			if label == "" {
				label = m.Name
			}
			if label == "" {
				label = m.ID
			}

			models = append(models, Model{
				ID:              m.ID,
				Name:            m.Name,
				Label:           label,
				ContextWindow:   contextWindow,
				MaxOutputTokens: maxOutputTokens,
				Family:          family,
				OpenWeights:     openWeights,
				Capabilities:    caps,
				Pricing:         pricing,
				Modalities:      modalities,
				IsDefault:       m.ID == p.Spec.DefaultModel,
				KnowledgeCutoff: knowledgeCutoff,
				LastUpdated:     lastUpdated,
			})
		}

		var authEnv string
		var apiKey string
		if p.Spec.Auth != nil && p.Spec.Auth.Env != "" {
			authEnv = p.Spec.Auth.Env
			apiKey = os.Getenv(authEnv)
		}

		resp.Providers = append(resp.Providers, Provider{
			Name:         p.Metadata.Name,
			DisplayName:  displayName,
			Description:  p.Spec.Type,
			DefaultModel: p.Spec.DefaultModel,
			Endpoint:     p.Spec.Endpoint,
			AuthEnv:      authEnv,
			APIKey:       apiKey,
			Models:       models,
		})
	}

	sort.Slice(resp.Providers, func(i, j int) bool {
		return resp.Providers[i].Name < resp.Providers[j].Name
	})

	return resp, nil
}

// ListProvidersPresets returns a list of model provider presets in the workspace.
func (s *Service) ListProvidersPresets(ctx context.Context, req ListProvidersPresetsRequest) (*ListProvidersPresetsResponse, error) {
	presets := s.ws.ProvidersPresets()

	// Create a map of currently configured providers
	configured := make(map[string]*warp.ModelProvider)
	for _, cp := range s.ws.Providers() {
		configured[cp.Metadata.Name] = cp
	}

	resp := &ListProvidersPresetsResponse{
		Providers: make([]Provider, 0, len(presets)),
	}

	for _, p := range presets {
		displayName := p.Metadata.DisplayName
		if displayName == "" {
			displayName = p.Metadata.Name
		}

		models := make([]Model, 0, len(p.Spec.Models))
		for _, m := range p.Spec.Models {
			models = append(models, Model{
				ID:              m.ID,
				Name:            m.Name,
				Label:           m.Label,
				ContextWindow:   m.Limits.Context,
				MaxOutputTokens: m.Limits.Output,
			})
		}

		defaultModel := p.Spec.DefaultModel
		endpoint := p.Spec.Endpoint

		// Preload values from configured provider if it exists
		if cp, ok := configured[p.Metadata.Name]; ok {
			if cp.Spec.DefaultModel != "" {
				defaultModel = cp.Spec.DefaultModel
			}
			if cp.Spec.Endpoint != "" {
				endpoint = cp.Spec.Endpoint
			}
		}

		var authEnv string
		var apiKey string
		if p.Spec.Auth != nil && p.Spec.Auth.Env != "" {
			authEnv = p.Spec.Auth.Env
			apiKey = os.Getenv(authEnv)
		}

		resp.Providers = append(resp.Providers, Provider{
			Name:         p.Metadata.Name,
			DisplayName:  displayName,
			Description:  p.Spec.Type,
			DefaultModel: defaultModel,
			Endpoint:     endpoint,
			AuthEnv:      authEnv,
			APIKey:       apiKey,
			Models:       models,
		})
	}

	sort.Slice(resp.Providers, func(i, j int) bool {
		return resp.Providers[i].Name < resp.Providers[j].Name
	})

	return resp, nil
}

// ListToolsPresets returns a list of tool presets in the workspace.
func (s *Service) ListToolsPresets(ctx context.Context, req ListToolsPresetsRequest) (*ListToolsPresetsResponse, error) {
	tools := s.ws.ToolsPresets()
	resp := &ListToolsPresetsResponse{
		Tools: make([]Tool, 0, len(tools)),
	}

	for _, t := range tools {
		category := t.Metadata.Labels["category"]
		if category == "" {
			category = "General"
		}
		resp.Tools = append(resp.Tools, Tool{
			Name:        t.Metadata.Name,
			Description: t.Metadata.Description,
			Category:    category,
			Labels:      t.Metadata.Labels,
		})
	}

	sort.Slice(resp.Tools, func(i, j int) bool {
		if resp.Tools[i].Category != resp.Tools[j].Category {
			return resp.Tools[i].Category < resp.Tools[j].Category
		}
		return resp.Tools[i].Name < resp.Tools[j].Name
	})

	return resp, nil
}

// ListSessions returns a list of all active or saved sessions.
func (s *Service) ListSessions(ctx context.Context, req ListSessionsRequest) (*ListSessionsResponse, error) {
	if s.sm == nil {
		return &ListSessionsResponse{}, nil
	}
	sessions, err := s.sm.ListSessions(ctx, session.ListSessionsQuery{
		Limit:  req.Limit,
		Offset: req.Offset,
	})
	if err != nil {
		return nil, err
	}
	resp := &ListSessionsResponse{
		Sessions: make([]Session, len(sessions)),
	}
	for i, sess := range sessions {
		var apiMetrics *SessionMetrics
		if sess.LastTurnMetrics != nil {
			apiMetrics = &SessionMetrics{
				SystemTokens:               sess.LastTurnMetrics.SystemTokens,
				ToolsTokens:                sess.LastTurnMetrics.ToolsTokens,
				ToolResultTokens:           sess.LastTurnMetrics.ToolResultTokens,
				WorkspaceFileTokens:        sess.LastTurnMetrics.WorkspaceFileTokens,
				ChatTokens:                 sess.LastTurnMetrics.ChatTokens,
				PromptTokens:               sess.LastTurnMetrics.PromptTokens,
				CompletionTokens:           sess.LastTurnMetrics.CompletionTokens,
				TotalTokens:                sess.LastTurnMetrics.TotalTokens,
				EstimatedCostUSD:           sess.LastTurnMetrics.EstimatedCostUSD,
				CumulativePromptTokens:     sess.LastTurnMetrics.CumulativePromptTokens,
				CumulativeCompletionTokens: sess.LastTurnMetrics.CumulativeCompletionTokens,
				CumulativeTotalTokens:      sess.LastTurnMetrics.CumulativeTotalTokens,
				CumulativeCostUSD:          sess.LastTurnMetrics.CumulativeCostUSD,
			}
		}

		resp.Sessions[i] = Session{
			ID:              sess.ID,
			Title:           sess.Title,
			Settings:        sess.Settings,
			LastTurnMetrics: apiMetrics,
			CreatedAt:       sess.CreatedAt.Format(time.RFC3339),
			UpdatedAt:       sess.UpdatedAt.Format(time.RFC3339),
		}
	}
	return resp, nil
}

// CreateSession initializes a new session workspace.
func (s *Service) CreateSession(ctx context.Context, req CreateSessionRequest) (*CreateSessionResponse, error) {
	if s.sm == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	sess, err := s.sm.CreateSession(ctx, req.Title)
	if err != nil {
		return nil, err
	}
	return &CreateSessionResponse{
		Session: Session{
			ID:        sess.ID,
			Title:     sess.Title,
			Settings:  sess.Settings,
			CreatedAt: sess.CreatedAt.Format(time.RFC3339),
			UpdatedAt: sess.UpdatedAt.Format(time.RFC3339),
		},
	}, nil
}

// ConfigureSession updates the agent/provider/model configuration for an active session.
func (s *Service) ConfigureSession(ctx context.Context, req ConfigureSessionRequest) (*ConfigureSessionResponse, error) {
	if s.sm == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}

	sess, err := s.sm.GetSession(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}

	// Read existing or incoming settings config
	var newSettings model.SessionSettings
	if req.Settings != nil {
		newSettings = *req.Settings
	} else {
		newSettings = sess.Settings
	}

	if req.AgentName != "" {
		newSettings.AgentName = req.AgentName
	}
	if req.ProviderName != "" {
		newSettings.ProviderName = req.ProviderName
	}
	if req.ModelName != "" {
		newSettings.ModelName = req.ModelName
	}

	// Validate or adapt thinking settings based on model capabilities resolved from dynamic providers list
	if newSettings.Thinking != nil {
		var activeModel *Model
		providersResp, err := s.ListProviders(ctx, ListProvidersRequest{})
		if err == nil && providersResp != nil {
			for _, p := range providersResp.Providers {
				if p.Name == newSettings.ProviderName {
					for _, m := range p.Models {
						if m.ID == newSettings.ModelName {
							activeModel = &m
							break
						}
					}
					break
				}
			}
		}

		if activeModel != nil {
			supportsOption := func(optType string) bool {
				if !activeModel.Capabilities.Reasoning {
					return false
				}
				if optType == "toggle" || optType == "enabled" {
					if len(activeModel.Capabilities.ReasoningOptions) == 0 {
						return true
					}
					for _, opt := range activeModel.Capabilities.ReasoningOptions {
						if opt.Type == "toggle" || opt.Type == "enabled" {
							return true
						}
					}
					return false
				}
				for _, opt := range activeModel.Capabilities.ReasoningOptions {
					if opt.Type == optType {
						return true
					}
					if optType == "budget" && (opt.Type == "budget_tokens" || opt.Type == "budget") {
						return true
					}
				}
				return false
			}

			if req.ModelName != "" {
				// We are switching models. Clean/adapt existing settings to the new model's capabilities.
				if !activeModel.Capabilities.Reasoning {
					newSettings.Thinking = nil
				} else {
					if newSettings.Thinking.Enabled != nil && *newSettings.Thinking.Enabled && !supportsOption("toggle") {
						enabledVal := true
						newSettings.Thinking.Enabled = &enabledVal
					}
					if newSettings.Thinking.Adaptive != nil && *newSettings.Thinking.Adaptive && !supportsOption("adaptive") {
						newSettings.Thinking.Adaptive = nil
					}
					if newSettings.Thinking.Effort != nil && !supportsOption("effort") {
						newSettings.Thinking.Effort = nil
					}
					if newSettings.Thinking.Budget != nil && !supportsOption("budget") {
						newSettings.Thinking.Budget = nil
					}
				}
			} else {
				// Strict validation when explicitly modifying thinking settings for the current model
				tcfg := newSettings.Thinking
				if !activeModel.Capabilities.Reasoning {
					if (tcfg.Enabled != nil && *tcfg.Enabled) || tcfg.Effort != nil || tcfg.Budget != nil || (tcfg.Adaptive != nil && *tcfg.Adaptive) {
						return nil, fmt.Errorf("active model %q does not support reasoning/thinking", newSettings.ModelName)
					}
				} else {
					if tcfg.Enabled != nil && !supportsOption("toggle") {
						return nil, fmt.Errorf("active model %q does not support toggling reasoning on/off", newSettings.ModelName)
					}
					if tcfg.Adaptive != nil && *tcfg.Adaptive && !supportsOption("adaptive") {
						return nil, fmt.Errorf("active model %q does not support adaptive reasoning", newSettings.ModelName)
					}
					if tcfg.Effort != nil && !supportsOption("effort") {
						return nil, fmt.Errorf("active model %q does not support reasoning effort configuration", newSettings.ModelName)
					}
					if tcfg.Budget != nil && !supportsOption("budget") {
						return nil, fmt.Errorf("active model %q does not support token budget configuration", newSettings.ModelName)
					}
				}
			}
		} else {
			// If we switched to an unknown model (e.g. not in providers list), clear thinking settings to be safe
			if req.ModelName != "" {
				newSettings.Thinking = nil
			}
		}
	}

	// Validate temperature settings based on model capabilities
	if req.Settings != nil && req.Settings.Temperature != nil {
		var activeModel *Model
		providersResp, err := s.ListProviders(ctx, ListProvidersRequest{})
		if err == nil && providersResp != nil {
			for _, p := range providersResp.Providers {
				if p.Name == newSettings.ProviderName {
					for _, m := range p.Models {
						if m.ID == newSettings.ModelName {
							activeModel = &m
							break
						}
					}
					break
				}
			}
		}

		if activeModel != nil && !activeModel.Capabilities.Temperature {
			// Explicitly reject setting a temperature when the active model does not support it
			return nil, fmt.Errorf("active model %q does not support temperature configuration", newSettings.ModelName)
		}
	}

	cfg := session.SessionConfig{
		Settings: newSettings,
	}

	if err := s.sm.UpdateSessionConfig(ctx, req.SessionID, cfg); err != nil {
		return nil, err
	}

	return &ConfigureSessionResponse{Success: true}, nil
}

// DeleteSession terminates and purges a session workspace.
func (s *Service) DeleteSession(ctx context.Context, req DeleteSessionRequest) (*DeleteSessionResponse, error) {
	if s.sm == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	if err := s.sm.DeleteSession(ctx, req.ID); err != nil {
		return nil, err
	}
	return &DeleteSessionResponse{Success: true}, nil
}

// RenameSession updates the title of an existing session.
func (s *Service) RenameSession(ctx context.Context, req RenameSessionRequest) (*RenameSessionResponse, error) {
	if s.sm == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	if err := s.sm.RenameSession(ctx, req.ID, req.Title); err != nil {
		return nil, err
	}
	return &RenameSessionResponse{Success: true}, nil
}

// ArchiveSession soft-removes a session from the active list.
func (s *Service) ArchiveSession(ctx context.Context, req ArchiveSessionRequest) (*ArchiveSessionResponse, error) {
	if s.sm == nil {
		return nil, fmt.Errorf("session manager not initialized")

	}
	if err := s.sm.ArchiveSession(ctx, req.ID); err != nil {
		return nil, err
	}
	return &ArchiveSessionResponse{Success: true}, nil
}

// SendMessage delivers a message to a session workspace's agent graph.
func (s *Service) SendMessage(ctx context.Context, req SendMessageRequest) (*SendMessageResponse, error) {
	if s.sm == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	refs := make([]resolver.Reference, len(req.References))
	for i, r := range req.References {
		refs[i] = r.FromPayload()
	}
	if err := s.sm.SendMessage(ctx, req.SessionID, req.Text, refs); err != nil {
		return nil, err
	}
	return &SendMessageResponse{Success: true}, nil
}

// CancelTurn cancels the current active agent run for the session.
func (s *Service) CancelTurn(ctx context.Context, req CancelTurnRequest) (*CancelTurnResponse, error) {
	if s.sm == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	if err := s.sm.CancelTurn(ctx, req.SessionID); err != nil {
		return nil, err
	}
	return &CancelTurnResponse{Success: true}, nil
}

// RetryTurn retries execution of the last user turn.
func (s *Service) RetryTurn(ctx context.Context, req RetryTurnRequest) (*RetryTurnResponse, error) {
	if s.sm == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	if err := s.sm.RetryTurn(ctx, req.SessionID); err != nil {
		return nil, err
	}
	return &RetryTurnResponse{Success: true}, nil
}

// ForceCompaction triggers a forced compaction run for the session.
func (s *Service) ForceCompaction(ctx context.Context, req ForceCompactionRequest) (*ForceCompactionResponse, error) {
	if s.sm == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	if err := s.sm.ForceCompaction(ctx, req.SessionID); err != nil {
		return nil, err
	}
	return &ForceCompactionResponse{Success: true}, nil
}

// GetSessionMessages returns the message log of a session.
func (s *Service) GetSessionMessages(ctx context.Context, req GetSessionMessagesRequest) (*GetSessionMessagesResponse, error) {
	if s.sm == nil {
		return &GetSessionMessagesResponse{}, nil
	}
	msgs, err := s.sm.GetMessages(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}

	queuedMsgs, err := s.sm.GetQueuedMessages(req.SessionID)
	if err != nil {
		return nil, err
	}

	return &GetSessionMessagesResponse{
		Messages:       msgs,
		QueuedMessages: queuedMsgs,
	}, nil
}

// GetInputHistory returns a list of unique user prompts submitted in the workspace.
func (s *Service) GetInputHistory(ctx context.Context, req GetInputHistoryRequest) (*GetInputHistoryResponse, error) {
	if s.sm == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	inputs, err := s.sm.GetUserMessageHistory(ctx, req.Query, limit)
	if err != nil {
		return nil, err
	}
	return &GetInputHistoryResponse{Inputs: inputs}, nil
}

// WatchSessionMessages establishes a stream that yields updated message lists.
func (s *Service) WatchSessionMessages(ctx context.Context, req GetSessionMessagesRequest) iter.Seq2[*GetSessionMessagesResponse, error] {
	return func(yield func(*GetSessionMessagesResponse, error) bool) {
		ch, cleanup := s.sm.SubscribeMessages(req.SessionID)
		notifyCh := make(chan struct{}, 1)
		const throttleDuration = 100 * time.Millisecond
		var lastYield time.Time
		var timer *time.Timer
		var pendingUpdate bool

		defer func() {
			if timer != nil {
				timer.Stop()
			}
			cleanup()
		}()

		// Helper function to perform the yield on the stream loop goroutine
		doYield := func() bool {
			resp, err := s.GetSessionMessages(ctx, req)
			if err != nil {
				return yield(nil, err)
			}
			lastYield = time.Now()
			pendingUpdate = false
			return yield(resp, nil)
		}

		processUpdate := func() bool {
			now := time.Now()
			elapsed := now.Sub(lastYield)

			if elapsed >= throttleDuration {
				if timer != nil {
					timer.Stop()
					timer = nil
				}
				if !doYield() {
					return false
				}
			} else {
				if !pendingUpdate {
					pendingUpdate = true
					remaining := throttleDuration - elapsed
					if timer != nil {
						timer.Stop()
					}
					timer = time.AfterFunc(remaining, func() {
						select {
						case notifyCh <- struct{}{}:
						default:
						}
					})
				}
			}
			return true
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-s.sm.Done():
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
				if !processUpdate() {
					return
				}
			case <-notifyCh:
				if !processUpdate() {
					return
				}
			}
		}
	}
}

// GetSessionState queries the active execution status of the session agent.
func (s *Service) GetSessionState(ctx context.Context, req GetSessionStateRequest) (*GetSessionStateResponse, error) {
	if s.sm == nil {
		var apiSuggestions []LspSuggestion
		if s.lspManager != nil {
			suggs := s.lspManager.GetSuggestions()
			for _, sug := range suggs {
				apiSuggestions = append(apiSuggestions, LspSuggestion{
					Language:   sug.Language,
					ServerName: sug.ServerName,
					Command:    sug.Command,
				})
			}
		}
		var apiMcpRequests []PendingMcpRequest
		for _, pr := range mcp.ActiveRequests.List() {
			apiMcpRequests = append(apiMcpRequests, PendingMcpRequest{
				ID:         pr.ID,
				Type:       pr.Type,
				ServerName: pr.ServerName,
				Message:    pr.Message,
				URL:        pr.URL,
				Schema:     pr.Schema,
			})
		}
		var mode permissions.PermissionMode
		if pm, err := s.newPermissionManager(ctx, ""); err == nil {
			mode = pm.GetMode(ctx)
		} else {
			mode = permissions.ModeDefault
		}
		return &GetSessionStateResponse{
			Status:                "idle",
			PendingLspSuggestions: apiSuggestions,
			PendingMcpRequests:    apiMcpRequests,
			PermissionMode:        mode,
		}, nil
	}
	status, errStr, isGen, isCompacting, pendingAuths, pendingQuestions, elapsed := s.sm.GetSessionState(ctx, req.SessionID)

	var runningTasks []RunningTaskInfo
	tasks := s.sm.ListTasks(req.SessionID)
	for _, t := range tasks {
		if t.Status == tools.StatusRunning {
			runningTasks = append(runningTasks, RunningTaskInfo{
				ID:      t.ID,
				Name:    t.Name,
				Type:    t.Type,
				Details: t.Details,
			})
		}
	}

	var apiTodos []Todo
	todos, err := s.sm.ListTodos(ctx, req.SessionID)
	if err == nil {
		for _, t := range todos {
			apiTodos = append(apiTodos, Todo{
				Description: t.Description,
				Status:      t.Status,
				ActiveText:  t.ActiveText,
			})
		}
	}

	var apiSuggestions []LspSuggestion
	if s.lspManager != nil {
		suggs := s.lspManager.GetSuggestions()
		for _, sug := range suggs {
			apiSuggestions = append(apiSuggestions, LspSuggestion{
				Language:   sug.Language,
				ServerName: sug.ServerName,
				Command:    sug.Command,
			})
		}
	}

	var apiMcpRequests []PendingMcpRequest
	for _, pr := range mcp.ActiveRequests.List() {
		apiMcpRequests = append(apiMcpRequests, PendingMcpRequest{
			ID:         pr.ID,
			Type:       pr.Type,
			ServerName: pr.ServerName,
			Message:    pr.Message,
			URL:        pr.URL,
			Schema:     pr.Schema,
		})
	}

	elapsedSeconds := int64(elapsed.Seconds())

	var mode permissions.PermissionMode
	if pm, err := s.newPermissionManager(ctx, req.SessionID); err == nil {
		mode = pm.GetMode(ctx)
	} else {
		mode = permissions.ModeDefault
	}

	var apiQuestions []PendingQuestion
	for _, pq := range pendingQuestions {
		apiQuestions = append(apiQuestions, PendingQuestion{
			ToolCallID:    pq.ToolCallID,
			Question:      pq.Question,
			Options:       pq.Options,
			IsMultiSelect: pq.IsMultiSelect,
		})
	}

	resp := &GetSessionStateResponse{
		Status:                string(status),
		Error:                 errStr,
		IsGenerating:          isGen,
		IsCompacting:          isCompacting,
		ThinkingDuration:      elapsedSeconds,
		RunningTasks:          runningTasks,
		Todos:                 apiTodos,
		PendingAuthorizations: pendingAuths,
		PendingQuestions:      apiQuestions,
		PendingLspSuggestions: apiSuggestions,
		PendingMcpRequests:    apiMcpRequests,
		PermissionMode:        mode,
	}

	// Populate last turn metrics from active session.
	if sess, err := s.sm.GetSession(ctx, req.SessionID); err == nil && sess != nil && sess.LastTurnMetrics != nil {
		resp.LastTurnMetrics = &SessionMetrics{
			SystemTokens:               sess.LastTurnMetrics.SystemTokens,
			ToolsTokens:                sess.LastTurnMetrics.ToolsTokens,
			ToolResultTokens:           sess.LastTurnMetrics.ToolResultTokens,
			WorkspaceFileTokens:        sess.LastTurnMetrics.WorkspaceFileTokens,
			ChatTokens:                 sess.LastTurnMetrics.ChatTokens,
			PromptTokens:               sess.LastTurnMetrics.PromptTokens,
			CompletionTokens:           sess.LastTurnMetrics.CompletionTokens,
			TotalTokens:                sess.LastTurnMetrics.TotalTokens,
			EstimatedCostUSD:           sess.LastTurnMetrics.EstimatedCostUSD,
			CumulativePromptTokens:     sess.LastTurnMetrics.CumulativePromptTokens,
			CumulativeCompletionTokens: sess.LastTurnMetrics.CumulativeCompletionTokens,
			CumulativeTotalTokens:      sess.LastTurnMetrics.CumulativeTotalTokens,
			CumulativeCostUSD:          sess.LastTurnMetrics.CumulativeCostUSD,
		}
	}

	return resp, nil
}

// ResolveMcpRequest resolves a pending MCP authorization or elicitation request.
func (s *Service) ResolveMcpRequest(ctx context.Context, req ResolveMcpRequest) (*ResolveMcpResponse, error) {
	pr := mcp.ActiveRequests.Get(req.RequestID)
	if pr == nil {
		return nil, fmt.Errorf("pending MCP request %q not found", req.RequestID)
	}

	switch pr.Type {
	case "oauth":
		if req.Action == "cancel" {
			pr.ResponseChan <- fmt.Errorf("oauth flow cancelled by user")
			return &ResolveMcpResponse{Success: true}, nil
		}
		// If custom manual authorization code is provided, try returning it
		if req.Code != "" {
			pr.ResponseChan <- &auth.AuthorizationResult{
				Code:  req.Code,
				State: req.State,
			}
			return &ResolveMcpResponse{Success: true}, nil
		}
		pr.ResponseChan <- fmt.Errorf("oauth flow cancelled by user")
		return &ResolveMcpResponse{Success: true}, nil
	case "elicitation":
		if req.Action == "reject" || req.Action == "cancel" {
			pr.ResponseChan <- fmt.Errorf("elicitation cancelled by user")
			return &ResolveMcpResponse{Success: true}, nil
		}

		res := &mcpsdk.ElicitResult{
			Action:  req.Action,
			Content: req.Content,
		}
		pr.ResponseChan <- res
		return &ResolveMcpResponse{Success: true}, nil
	}

	return nil, fmt.Errorf("unknown pending request type %q", pr.Type)
}

// SubmitAuthorizationDecision submits a user permission decision and resumes the agent.
func (s *Service) SubmitAuthorizationDecision(ctx context.Context, req SubmitAuthorizationDecisionRequest) (*SubmitAuthorizationDecisionResponse, error) {
	if s.sm == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	decisions := req.Decisions
	if len(decisions) == 0 {
		decisions = []permissions.AuthorizationDecision{req.Decision}
	}
	if err := s.sm.SubmitAuthorizationDecision(ctx, req.SessionID, decisions...); err != nil {
		return nil, err
	}
	return &SubmitAuthorizationDecisionResponse{Success: true}, nil
}

// SubmitQuestionAnswers submits user question answers and resumes the agent.
func (s *Service) SubmitQuestionAnswers(ctx context.Context, req SubmitQuestionAnswersRequest) (*SubmitQuestionAnswersResponse, error) {
	if s.sm == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	var sessionAnswers []tools.QuestionAnswer
	for _, ans := range req.Answers {
		sessionAnswers = append(sessionAnswers, tools.QuestionAnswer{
			ToolCallID: ans.ToolCallID,
			Selected:   ans.Selected,
			WriteIn:    ans.WriteIn,
		})
	}
	if err := s.sm.SubmitQuestionAnswers(ctx, req.SessionID, sessionAnswers); err != nil {
		return nil, err
	}
	return &SubmitQuestionAnswersResponse{Success: true}, nil
}

// SetPermissionMode updates the active permission mode for a session or workspace/global.
func (s *Service) SetPermissionMode(ctx context.Context, req SetPermissionModeRequest) (*SetPermissionModeResponse, error) {
	pm, err := s.newPermissionManager(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}

	if err := pm.SaveMode(ctx, req.Scope, req.Mode); err != nil {
		return nil, err
	}

	return &SetPermissionModeResponse{Success: true}, nil
}

// GetPermissions retrieves all stored permissions across all active scopes.
func (s *Service) GetPermissions(ctx context.Context, req GetPermissionsRequest) (*GetPermissionsResponse, error) {
	pm, err := s.newPermissionManager(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}

	all, err := pm.GetAllPermissions(ctx)
	if err != nil {
		return nil, err
	}

	permsMap := make(map[string][]permissions.Permission)
	for scope, list := range all {
		permsMap[string(scope)] = list
	}

	return &GetPermissionsResponse{
		Permissions: permsMap,
	}, nil
}

// DeletePermission removes a stored permission from the given scope.
func (s *Service) DeletePermission(ctx context.Context, req DeletePermissionRequest) (*DeletePermissionResponse, error) {
	pm, err := s.newPermissionManager(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}

	if err := pm.DeletePermission(ctx, req.Scope, req.Permission); err != nil {
		return nil, err
	}

	return &DeletePermissionResponse{Success: true}, nil
}

// GetTokenAnalytics aggregates token usage statistics.
func (s *Service) GetTokenAnalytics(ctx context.Context, req GetTokenAnalyticsRequest) (*GetTokenAnalyticsResponse, error) {
	resp := &GetTokenAnalyticsResponse{
		ProvidersList: []string{},
		DailyActivity: []DailyActivity{},
		ByProject:     []ByProjectStats{},
		ByModel:       []ByModelStats{},
		ByAgent:       []ByAgentStats{},
		Tools:         []ToolAnalytics{},
	}

	if s.metricsStore == nil {
		return resp, nil
	}

	result, err := s.metricsStore.GetTokenAnalytics(ctx, req.Timeframe, req.ProviderFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch token analytics: %w", err)
	}

	resp.GlobalStats = result.GlobalStats
	resp.DailyActivity = result.DailyActivity
	resp.SummaryBreakdown = result.SummaryBreakdown
	resp.ByProject = result.ByProject
	resp.ByModel = result.ByModel
	resp.ByAgent = result.ByAgent
	resp.Tools = result.Tools
	resp.ProvidersList = result.ProvidersList

	return resp, nil
}

// ConfigureLsp configures the LSP preset for a given language.
func (s *Service) ConfigureLsp(ctx context.Context, req ConfigureLspRequest) (*ConfigureLspResponse, error) {
	if s.lspManager == nil {
		return nil, fmt.Errorf("LSP manager is not initialized")
	}

	preset, ok := lsp.Presets[req.Language]
	if !ok {
		return nil, fmt.Errorf("unknown language preset: %s", req.Language)
	}

	cfg, err := lsp.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load LSP config: %w", err)
	}

	// Check if already configured
	alreadyConfigured := false
	for _, srv := range cfg.Servers {
		if srv.Name == preset.Name {
			alreadyConfigured = true
			break
		}
	}

	if !alreadyConfigured {
		cfg.Servers = append(cfg.Servers, preset)
		if err := lsp.SaveConfig(cfg); err != nil {
			return nil, fmt.Errorf("failed to save LSP config: %w", err)
		}
	}

	// Dismiss the suggestion since it's now configured
	s.lspManager.DismissSuggestion(req.Language)

	// Restart client
	wsCfg, err := s.ws.GetWorkspaceConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace config: %w", err)
	}
	if err := s.lspManager.RestartClient(ctx, wsCfg.CWD); err != nil {
		return nil, fmt.Errorf("failed to restart LSP client: %w", err)
	}

	return &ConfigureLspResponse{Success: true}, nil
}

// DismissLspSuggestion dismisses a pending LSP suggestion so it won't be recommended again.
func (s *Service) DismissLspSuggestion(ctx context.Context, req DismissLspSuggestionRequest) (*DismissLspSuggestionResponse, error) {
	if s.lspManager == nil {
		return nil, fmt.Errorf("LSP manager is not initialized")
	}
	s.lspManager.DismissSuggestion(req.Language)
	return &DismissLspSuggestionResponse{Success: true, Message: "Suggestion dismissed"}, nil
}

// GetLspStatus retrieves the status of all configured LSP servers.
func (s *Service) GetLspStatus(ctx context.Context, req GetLspStatusRequest) (*GetLspStatusResponse, error) {
	if s.lspManager == nil {
		return &GetLspStatusResponse{Servers: []LspServerInfo{}}, nil
	}

	statusList := s.lspManager.GetStatus()
	var servers []LspServerInfo
	for _, st := range statusList {
		servers = append(servers, LspServerInfo{
			Name:      st.Name,
			Command:   st.Command,
			FileTypes: st.FileTypes,
			IsRunning: st.IsRunning,
		})
	}
	return &GetLspStatusResponse{Servers: servers}, nil
}

// GetMcpStatus retrieves the status of all configured MCP servers.
func (s *Service) GetMcpStatus(ctx context.Context, req GetMcpStatusRequest) (*GetMcpStatusResponse, error) {
	mcpMgr := s.sm.McpManager()
	if mcpMgr == nil {
		return &GetMcpStatusResponse{Servers: []McpServerInfo{}}, nil
	}

	mcps := s.ws.MCPs()
	var servers []McpServerInfo

	for _, mcpResource := range mcps {
		serverName := mcpResource.GetName()
		spec := mcpResource.Spec

		var transport string
		if strings.ToLower(spec.Type) == "sse" {
			transport = "http"
		} else {
			transport = "stdio"
		}

		var envKeys []string
		for k := range spec.Env {
			envKeys = append(envKeys, k)
		}
		sort.Strings(envKeys)

		configYAML, _ := warp.Format(mcpResource)

		info := McpServerInfo{
			Name:        serverName,
			Type:        transport,
			Command:     spec.Command,
			URL:         spec.Endpoint,
			EnvKeys:     envKeys,
			Description: mcpResource.Metadata.Description,
			Config:      string(configYAML),
		}

		if spec.Annotations != nil {
			info.IsDangerous = spec.Annotations.IsDangerous
			info.IsReadOnly = spec.Annotations.IsReadOnly
			info.IsOpenWorld = spec.Annotations.IsOpenWorld
			info.IsIdempotent = spec.Annotations.IsIdempotent
			info.UserHint = spec.Annotations.UserHint
		}

		if mcpMgr.MultiClient() != nil {
			// Try to query tools to see if it is running and what tools it exposes
			fetchedTools, err := mcpMgr.MultiClient().Tools(ctx, serverName)
			if err != nil {
				info.IsRunning = false
				info.Error = err.Error()
			} else {
				info.IsRunning = true
				cleanName := func(s string) string {
					return strings.ReplaceAll(s, "-", "_")
				}
				sNameCleaned := cleanName(serverName)
				for _, lt := range fetchedTools {
					var anno tool.Annotation
					if mcpResource.Spec.Annotations != nil {
						anno.IsDangerous = mcpResource.Spec.Annotations.IsDangerous
						anno.IsOpenWorld = mcpResource.Spec.Annotations.IsOpenWorld
						anno.IsReadOnly = mcpResource.Spec.Annotations.IsReadOnly
						anno.IsIdempotent = mcpResource.Spec.Annotations.IsIdempotent
						anno.UserHint = mcpResource.Spec.Annotations.UserHint
					}
					if override, ok := mcpResource.Spec.Overrides[lt.Definition.Name]; ok {
						anno.IsDangerous = override.IsDangerous
						anno.IsOpenWorld = override.IsOpenWorld
						anno.IsReadOnly = override.IsReadOnly
						anno.IsIdempotent = override.IsIdempotent
						anno.UserHint = override.UserHint
					}

					info.Tools = append(info.Tools, McpTool{
						Name:         fmt.Sprintf("mcp__%s__%s", sNameCleaned, lt.Definition.Name),
						Description:  lt.Definition.Description,
						IsDangerous:  anno.IsDangerous,
						IsReadOnly:   anno.IsReadOnly,
						IsOpenWorld:  anno.IsOpenWorld,
						IsIdempotent: anno.IsIdempotent,
						UserHint:     anno.UserHint,
					})
				}

				if srvInfo, err := mcpMgr.MultiClient().Info(ctx, serverName); err == nil && srvInfo != nil {
					info.Title = srvInfo.Title
					info.Version = srvInfo.Version
					info.WebsiteURL = srvInfo.WebsiteURL
					info.Instructions = srvInfo.Instructions
					info.Capabilities = McpCapabilities{
						Completions: srvInfo.Capabilities.Completions,
						Logging:     srvInfo.Capabilities.Logging,
						Prompts:     srvInfo.Capabilities.Prompts != nil,
						Resources:   srvInfo.Capabilities.Resources != nil,
						Tools:       srvInfo.Capabilities.Tools != nil,
					}

					if srvInfo.Capabilities.Prompts != nil {
						if fetchedPrompts, err := mcpMgr.MultiClient().Prompts(ctx, serverName); err == nil {
							for _, p := range fetchedPrompts {
								var args []McpPromptArgument
								for _, arg := range p.Arguments {
									args = append(args, McpPromptArgument{
										Name:        arg.Name,
										Title:       arg.Title,
										Description: arg.Description,
										Required:    arg.Required,
									})
								}
								info.Prompts = append(info.Prompts, McpPrompt{
									Name:        p.Name,
									Title:       p.Title,
									Description: p.Description,
									Arguments:   args,
								})
							}
						}
					}

					if srvInfo.Capabilities.Resources != nil {
						if fetchedResources, err := mcpMgr.MultiClient().Resources(ctx, serverName); err == nil {
							for _, r := range fetchedResources {
								info.Resources = append(info.Resources, McpResource{
									Name:        r.Name,
									Title:       r.Title,
									Description: r.Description,
									MIMEType:    r.MIMEType,
									URI:         r.URI,
								})
							}
						}
						if fetchedTemplates, err := mcpMgr.MultiClient().ResourceTemplates(ctx, serverName); err == nil {
							for _, rt := range fetchedTemplates {
								info.ResourceTemplates = append(info.ResourceTemplates, McpResourceTemplate{
									Name:        rt.Name,
									Title:       rt.Title,
									Description: rt.Description,
									MIMEType:    rt.MIMEType,
									URITemplate: rt.URITemplate,
								})
							}
						}
					}
				}
			}
		}

		servers = append(servers, info)
	}

	return &GetMcpStatusResponse{Servers: servers}, nil
}

// RestartLsp shuts down and recreates all active LSP clients for the workspace.
func (s *Service) RestartLsp(ctx context.Context, req RestartLspRequest) (*RestartLspResponse, error) {
	if s.lspManager == nil {
		return &RestartLspResponse{Success: false}, fmt.Errorf("LSP manager is not initialized")
	}

	cfg, err := s.ws.GetWorkspaceConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace config: %w", err)
	}

	if err := s.lspManager.RestartClient(ctx, cfg.CWD); err != nil {
		return nil, fmt.Errorf("failed to restart LSP client: %w", err)
	}

	return &RestartLspResponse{Success: true}, nil
}

// GetLspDiagnosticCounts retrieves diagnostic counts for the workspace.
func (s *Service) GetLspDiagnosticCounts(ctx context.Context, req GetLspDiagnosticCountsRequest) (*GetLspDiagnosticCountsResponse, error) {
	if s.lspManager == nil {
		return &GetLspDiagnosticCountsResponse{}, nil
	}

	cfg, err := s.ws.GetWorkspaceConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace config: %w", err)
	}

	errors, warnings, infos, err := s.lspManager.GetDiagnosticCounts(ctx, cfg.CWD)
	if err != nil {
		return &GetLspDiagnosticCountsResponse{}, nil
	}

	return &GetLspDiagnosticCountsResponse{
		Errors:   errors,
		Warnings: warnings,
		Infos:    infos,
	}, nil
}

// GetLspDiagnostics retrieves detailed LSP diagnostics.
func (s *Service) GetLspDiagnostics(ctx context.Context, req GetLspDiagnosticsRequest) (*GetLspDiagnosticsResponse, error) {
	if s.lspManager == nil {
		return &GetLspDiagnosticsResponse{Diagnostics: []LspDiagnosticItem{}}, nil
	}

	cfg, err := s.ws.GetWorkspaceConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace config: %w", err)
	}

	targetPath := req.Path
	if targetPath == "" {
		targetPath = cfg.CWD
	} else if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(cfg.CWD, targetPath)
	}

	client, err := s.lspManager.GetClient(ctx, cfg.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to get LSP client: %w", err)
	}

	diags, err := client.GetDiagnostics(ctx, targetPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get diagnostics: %w", err)
	}

	var items []LspDiagnosticItem
	for _, d := range diags {
		var severity string
		if d.Severity != nil {
			switch *d.Severity {
			case 1:
				severity = "error"
			case 2:
				severity = "warning"
			case 3:
				severity = "info"
			case 4:
				severity = "hint"
			}
		}

		relPath, err := filepath.Rel(cfg.CWD, d.Path)
		if err != nil {
			relPath = d.Path
		}

		var msg string
		if d.Message.String != nil {
			msg = *d.Message.String
		} else if d.Message.MarkupContent != nil {
			msg = d.Message.MarkupContent.Value
		}

		items = append(items, LspDiagnosticItem{
			Path:     relPath,
			Message:  msg,
			Severity: severity,
			Line:     int(d.Range.Start.Line),
			Char:     int(d.Range.Start.Character),
		})
	}

	return &GetLspDiagnosticsResponse{Diagnostics: items}, nil
}

// LspSymbols searches using LSP.
func (s *Service) LspSymbols(ctx context.Context, req LspSymbolsRequest) (*LspSymbolsResponse, error) {
	if s.lspManager == nil {
		return &LspSymbolsResponse{Results: []LspSymbolsItem{}}, nil
	}

	cfg, err := s.ws.GetWorkspaceConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace config: %w", err)
	}

	client, err := s.lspManager.GetClient(ctx, cfg.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to get LSP client: %w", err)
	}

	results, err := client.Search(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to search LSP: %w", err)
	}

	var items []LspSymbolsItem
	for _, sym := range results {
		var docURI string
		var line, char int

		if sym.Location.Location != nil {
			docURI = sym.Location.Location.URI
			line = int(sym.Location.Location.Range.Start.Line)
			char = int(sym.Location.Location.Range.Start.Character)
		} else if sym.Location.LocationUriOnly != nil {
			docURI = sym.Location.LocationUriOnly.URI
		}

		var filePath string
		if strings.HasPrefix(docURI, "file://") {
			filePath = filepath.FromSlash(docURI[7:])
		} else {
			filePath = docURI
		}

		relPath, err := filepath.Rel(cfg.CWD, filePath)
		if err != nil {
			relPath = filePath
		}

		var containerName string
		if sym.ContainerName != nil {
			containerName = *sym.ContainerName
		}

		kindStr := fmt.Sprintf("Kind(%d)", sym.Kind)
		switch sym.Kind {
		case 1:
			kindStr = "File"
		case 2:
			kindStr = "Module"
		case 3:
			kindStr = "Namespace"
		case 4:
			kindStr = "Package"
		case 5:
			kindStr = "Class"
		case 6:
			kindStr = "Method"
		case 7:
			kindStr = "Property"
		case 8:
			kindStr = "Field"
		case 9:
			kindStr = "Constructor"
		case 10:
			kindStr = "Enum"
		case 11:
			kindStr = "Interface"
		case 12:
			kindStr = "Function"
		case 13:
			kindStr = "Variable"
		case 14:
			kindStr = "Constant"
		case 15:
			kindStr = "String"
		case 16:
			kindStr = "Number"
		case 17:
			kindStr = "Boolean"
		case 18:
			kindStr = "Array"
		case 19:
			kindStr = "Object"
		case 20:
			kindStr = "Key"
		case 21:
			kindStr = "Null"
		case 22:
			kindStr = "EnumMember"
		case 23:
			kindStr = "Struct"
		case 24:
			kindStr = "Event"
		case 25:
			kindStr = "Operator"
		case 26:
			kindStr = "TypeParameter"
		}

		items = append(items, LspSymbolsItem{
			Name:          sym.Name,
			Kind:          kindStr,
			Path:          relPath,
			Line:          line,
			Char:          char,
			ContainerName: containerName,
		})
	}

	return &LspSymbolsResponse{Results: items}, nil
}

// GetFileChanges returns summaries of all modified files for a session.
func (s *Service) GetFileChanges(ctx context.Context, req GetFileChangesRequest) (*GetFileChangesResponse, error) {
	ft, err := s.sm.FileTracker(req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get file tracker: %w", err)
	}

	sums, err := ft.Summary(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get summary: %w", err)
	}

	// Sort summaries by path for deterministic UI
	sort.Slice(sums, func(i, j int) bool {
		return sums[i].Path < sums[j].Path
	})

	apiSums := make([]FileChangeSummary, len(sums))
	for i, sum := range sums {
		apiSums[i] = FileChangeSummary{
			Path:          sum.Path,
			Kind:          string(sum.Kind),
			TotalEdits:    sum.TotalEdits,
			NetAdditions:  sum.NetAdditions,
			NetDeletions:  sum.NetDeletions,
			LastChangedAt: sum.LastChangedAt,
		}
	}

	return &GetFileChangesResponse{Changes: apiSums}, nil
}

// GetFileJournal returns all journal entries (history of changes) for a specific file.
func (s *Service) GetFileJournal(ctx context.Context, req GetFileJournalRequest) (*GetFileJournalResponse, error) {
	ft, err := s.sm.FileTracker(req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get file tracker: %w", err)
	}

	entries, err := ft.ReadJournal(ctx, req.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to read journal: %w", err)
	}

	apiEntries := make([]JournalEntryItem, len(entries))
	for i, entry := range entries {
		apiEntries[i] = JournalEntryItem{
			Timestamp: entry.Timestamp,
			ToolName:  entry.ToolName,
			Kind:      string(entry.Kind),
			Content:   entry.Content,
			Additions: entry.Additions,
			Deletions: entry.Deletions,
			Diff:      entry.Diff,
		}
	}

	return &GetFileJournalResponse{Entries: apiEntries}, nil
}

// RevertFile reverts a file to its pre-session state.
func (s *Service) RevertFile(ctx context.Context, req RevertFileRequest) (*RevertFileResponse, error) {
	ft, err := s.sm.FileTracker(req.SessionID)
	if err != nil {
		return &RevertFileResponse{Success: false, Error: err.Error()}, nil
	}

	if !req.Force {
		conflict, err := ft.CheckConflict(ctx, req.Path)
		if err != nil {
			return &RevertFileResponse{Success: false, Error: err.Error()}, nil
		}
		if conflict {
			return &RevertFileResponse{Success: false, Error: "conflict"}, nil
		}
	}

	if err := ft.RevertToBaseline(ctx, req.Path, req.Force); err != nil {
		return &RevertFileResponse{Success: false, Error: err.Error()}, nil
	}

	return &RevertFileResponse{Success: true}, nil
}

// GetCachedFile returns the contents of a session-cached file.
func (s *Service) GetCachedFile(ctx context.Context, req GetCachedFileRequest) (*GetCachedFileResponse, error) {
	cfg, err := s.ws.GetWorkspaceConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace config: %w", err)
	}

	storage := session.NewLocalFileStorage(cfg.CWD, req.SessionID)
	rc, err := storage.Get(ctx, req.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to get cached file: %w", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("failed to read cached file: %w", err)
	}

	return &GetCachedFileResponse{Content: string(data)}, nil
}

// RestartMcp restarts a specific MCP server by name.
func (s *Service) RestartMcp(ctx context.Context, req RestartMcpRequest) (*RestartMcpResponse, error) {
	mcpMgr := s.sm.McpManager()
	if mcpMgr == nil {
		return nil, fmt.Errorf("MCP manager is not initialized")
	}

	if mcpMgr.MultiClient() == nil {
		return nil, fmt.Errorf("MCP MultiClient is not initialized")
	}

	err := mcpMgr.MultiClient().Restart(ctx, req.ServerName)
	if err != nil {
		return &RestartMcpResponse{Success: false, Error: err.Error()}, nil
	}

	return &RestartMcpResponse{Success: true}, nil
}

// DequeueFrom removes the message with the given messageID and all subsequent messages from the inbox.
func (s *Service) DequeueFrom(ctx context.Context, req DequeueFromRequest) (*DequeueFromResponse, error) {
	if s.sm == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	msgs, err := s.sm.DequeueFrom(req.SessionID, req.MessageID)
	if err != nil {
		return nil, err
	}
	serialized, err := serializeMessages(msgs)
	if err != nil {
		return nil, err
	}
	return &DequeueFromResponse{Messages: serialized}, nil
}

// EnqueueMessages appends the provided messages to the end of the session's inbox.
func (s *Service) EnqueueMessages(ctx context.Context, req EnqueueMessagesRequest) (*EnqueueMessagesResponse, error) {
	if s.sm == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	msgs, err := parseJSONMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	if err := s.sm.EnqueueMessages(req.SessionID, msgs); err != nil {
		return nil, err
	}
	return &EnqueueMessagesResponse{Success: true}, nil
}

// ClearQueue removes all queued messages from the session's inbox.
func (s *Service) ClearQueue(ctx context.Context, req ClearQueueRequest) (*ClearQueueResponse, error) {
	if s.sm == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	if err := s.sm.ClearQueue(req.SessionID); err != nil {
		return nil, err
	}
	return &ClearQueueResponse{Success: true}, nil
}

// RemoveQueuedMessage filters out the specific message ID from the session's inbox.
func (s *Service) RemoveQueuedMessage(ctx context.Context, req RemoveQueuedMessageRequest) (*RemoveQueuedMessageResponse, error) {
	if s.sm == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	if err := s.sm.RemoveQueuedMessage(req.SessionID, req.MessageID); err != nil {
		return nil, err
	}
	return &RemoveQueuedMessageResponse{Success: true}, nil
}

// SendQueued triggers the graph to resume if the session is in StatusIdle and has queued messages.
func (s *Service) SendQueued(ctx context.Context, req SendQueuedRequest) (*SendQueuedResponse, error) {
	if s.sm == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}
	if err := s.sm.SendQueued(ctx, req.SessionID); err != nil {
		return nil, err
	}
	return &SendQueuedResponse{Success: true}, nil
}

func parseJSONMessages(serialized []string) ([]message.Message, error) {
	if len(serialized) == 0 {
		return nil, nil
	}
	var buf []byte
	buf = append(buf, '[')
	for i, s := range serialized {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, s...)
	}
	buf = append(buf, ']')
	var list message.MessageList
	if err := json.Unmarshal(buf, &list); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message list: %w", err)
	}
	return list, nil
}

func serializeMessages(msgs []message.Message) ([]string, error) {
	if len(msgs) == 0 {
		return nil, nil
	}
	res := make([]string, len(msgs))
	for i, msg := range msgs {
		list := message.MessageList{msg}
		data, err := json.Marshal(list)
		if err != nil {
			return nil, err
		}
		if len(data) >= 2 && data[0] == '[' && data[len(data)-1] == ']' {
			data = data[1 : len(data)-1]
		}
		res[i] = string(data)
	}
	return res, nil
}
