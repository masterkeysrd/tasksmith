package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/metrics"
	"github.com/masterkeysrd/tasksmith/internal/session"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
	"github.com/masterkeysrd/warp"
)

type Workspace interface {
	Projects() []*warp.Project
	Agents() []*warp.Agent
	Providers() []*warp.ModelProvider
	ProvidersPresets() []*warp.ModelProvider
	ToolsPresets() []*warp.Tool
	Initialize(ctx context.Context, opts workspace.InitializationOptions) error
	GetWorkspaceConfig(ctx context.Context) (workspace.WorkspaceConfig, error)
}

// Service provides methods to interact with the workspace through API types.
type Service struct {
	ws           Workspace
	sm           *session.Manager
	metricsStore *metrics.Store
}

// NewService creates a new API service.
func NewService(ws Workspace, sm *session.Manager, metricsStore *metrics.Store) *Service {
	return &Service{
		ws:           ws,
		sm:           sm,
		metricsStore: metricsStore,
	}
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
	}
	if err := s.ws.Initialize(ctx, opts); err != nil {
		return nil, err
	}
	return &InitializeWorkspaceResponse{Success: true}, nil
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

	return resp, nil
}

// ListAgents returns a list of agents in the workspace.
func (s *Service) ListAgents(ctx context.Context, req ListAgentsRequest) (*ListAgentsResponse, error) {
	agents := s.ws.Agents()
	resp := &ListAgentsResponse{
		Agents: make([]Agent, 0, len(agents)),
	}

	for _, a := range agents {
		resp.Agents = append(resp.Agents, Agent{
			Name:        a.Metadata.Name,
			Description: a.Metadata.Description,
		})
	}

	return resp, nil
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

		models := make([]Model, 0, len(p.Spec.Models))
		for _, m := range p.Spec.Models {
			models = append(models, Model{
				ID:            m.ID,
				Name:          m.Name,
				Label:         m.Label,
				ContextWindow: m.Limits.Context,
			})
		}

		resp.Providers = append(resp.Providers, Provider{
			Name:         p.Metadata.Name,
			DisplayName:  displayName,
			Description:  p.Spec.Type,
			DefaultModel: p.Spec.DefaultModel,
			Endpoint:     p.Spec.Endpoint,
			// AuthEnv:      p.Spec.Auth.Env,
			Models: models,
		})
	}

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
				ID:            m.ID,
				Name:          m.Name,
				Label:         m.Label,
				ContextWindow: m.Limits.Context,
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

	return resp, nil
}

// ListSessions returns a list of all active or saved sessions.
func (s *Service) ListSessions(ctx context.Context, req ListSessionsRequest) (*ListSessionsResponse, error) {
	if s.sm == nil {
		return &ListSessionsResponse{}, nil
	}
	sessions, err := s.sm.ListSessions(ctx)
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
			AgentName:       sess.AgentName,
			ProviderName:    sess.ProviderName,
			ModelName:       sess.ModelName,
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
			ID:           sess.ID,
			Title:        sess.Title,
			AgentName:    sess.AgentName,
			ProviderName: sess.ProviderName,
			ModelName:    sess.ModelName,
			CreatedAt:    sess.CreatedAt.Format(time.RFC3339),
			UpdatedAt:    sess.UpdatedAt.Format(time.RFC3339),
		},
	}, nil
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
	if err := s.sm.SendMessage(ctx, req.SessionID, req.Text); err != nil {
		return nil, err
	}
	return &SendMessageResponse{Success: true}, nil
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

	resp := &GetSessionMessagesResponse{
		Messages: make([]string, len(msgs)),
	}
	for i, msg := range msgs {
		list := message.MessageList{msg}
		data, err := json.Marshal(list)
		if err != nil {
			return nil, err
		}
		if len(data) >= 2 && data[0] == '[' && data[len(data)-1] == ']' {
			data = data[1 : len(data)-1]
		}
		resp.Messages[i] = string(data)
	}

	// Fetch in-memory queued messages
	queuedMsgs, err := s.sm.GetQueuedMessages(req.SessionID)
	if err != nil {
		return nil, err
	}
	if len(queuedMsgs) > 0 {
		resp.QueuedMessages = make([]string, len(queuedMsgs))
		for i, msg := range queuedMsgs {
			list := message.MessageList{msg}
			data, err := json.Marshal(list)
			if err != nil {
				return nil, err
			}
			if len(data) >= 2 && data[0] == '[' && data[len(data)-1] == ']' {
				data = data[1 : len(data)-1]
			}
			resp.QueuedMessages[i] = string(data)
		}
	}

	log.Info("GetSessionMessages debug",
		log.String("session", req.SessionID),
		log.Int("history", len(resp.Messages)),
		log.Int("queued", len(resp.QueuedMessages)))

	return resp, nil
}

// GetSessionState queries the active execution status of the session agent.
func (s *Service) GetSessionState(ctx context.Context, req GetSessionStateRequest) (*GetSessionStateResponse, error) {
	if s.sm == nil {
		return &GetSessionStateResponse{Status: "idle"}, nil
	}
	status, errStr, isGen := s.sm.GetSessionState(req.SessionID)

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

	return &GetSessionStateResponse{
		Status:       string(status),
		Error:        errStr,
		IsGenerating: isGen,
		RunningTasks: runningTasks,
		Todos:        apiTodos,
	}, nil
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
