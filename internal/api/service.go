package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
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
	lspManager   *lsp.Manager
}

// NewService creates a new API service.
func NewService(ws Workspace, sm *session.Manager, metricsStore *metrics.Store, lspManager *lsp.Manager) *Service {
	return &Service{
		ws:           ws,
		sm:           sm,
		metricsStore: metricsStore,
		lspManager:   lspManager,
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

	sort.Slice(resp.Projects, func(i, j int) bool {
		return resp.Projects[i].Name < resp.Projects[j].Name
	})

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

	sort.Slice(resp.Agents, func(i, j int) bool {
		return resp.Agents[i].Name < resp.Agents[j].Name
	})

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
				ID:              m.ID,
				Name:            m.Name,
				Label:           m.Label,
				ContextWindow:   m.Limits.Context,
				MaxOutputTokens: m.Limits.Output,
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
		return &GetSessionStateResponse{
			Status:                "idle",
			PendingLspSuggestions: apiSuggestions,
		}, nil
	}
	status, errStr, isGen, pendingAuths := s.sm.GetSessionState(ctx, req.SessionID)

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

	return &GetSessionStateResponse{
		Status:                string(status),
		Error:                 errStr,
		IsGenerating:          isGen,
		RunningTasks:          runningTasks,
		Todos:                 apiTodos,
		PendingAuthorizations: pendingAuths,
		PendingLspSuggestions: apiSuggestions,
	}, nil
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

// LspSearch searches using LSP.
func (s *Service) LspSearch(ctx context.Context, req LspSearchRequest) (*LspSearchResponse, error) {
	if s.lspManager == nil {
		return &LspSearchResponse{Results: []LspSearchItem{}}, nil
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

	var items []LspSearchItem
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

		items = append(items, LspSearchItem{
			Name:          sym.Name,
			Kind:          kindStr,
			Path:          relPath,
			Line:          line,
			Char:          char,
			ContainerName: containerName,
		})
	}

	return &LspSearchResponse{Results: items}, nil
}
