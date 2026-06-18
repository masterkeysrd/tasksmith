package api

import (
	"context"
	"os"

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
	ws Workspace
}

// NewService creates a new API service.
func NewService(ws Workspace) *Service {
	return &Service{ws: ws}
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
	resp := &ListProvidersResponse{
		Providers: make([]Provider, 0, len(providers)),
	}

	for _, p := range providers {
		displayName := p.Metadata.DisplayName
		if displayName == "" {
			displayName = p.Metadata.Name
		}

		models := make([]Model, 0, len(p.Spec.Models))
		for _, m := range p.Spec.Models {
			models = append(models, Model{
				ID:    m.ID,
				Name:  m.Name,
				Label: m.Label,
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
				ID:    m.ID,
				Name:  m.Name,
				Label: m.Label,
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
		if p.Spec.Auth != nil {
			if envName, ok := p.Spec.Auth["env"]; ok {
				authEnv = envName
				apiKey = os.Getenv(envName)
			}
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
