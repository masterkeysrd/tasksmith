package api

import (
	"context"

	"github.com/masterkeysrd/warp"
)

type Workspace interface {
	Projects() []*warp.Project
	Agents() []*warp.Agent
	Providers() []*warp.ModelProvider
	ProvidersPresets() []*warp.ModelProvider
	ToolsPresets() []*warp.Tool
}

// Service provides methods to interact with the workspace through API types.
type Service struct {
	ws Workspace
}

// NewService creates a new API service.
func NewService(ws Workspace) *Service {
	return &Service{ws: ws}
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
		resp.Providers = append(resp.Providers, Provider{
			Name:        p.Metadata.Name,
			DisplayName: displayName,
			Description: p.Spec.Type,
		})
	}

	return resp, nil
}

// ListProvidersPresets returns a list of model provider presets in the workspace.
func (s *Service) ListProvidersPresets(ctx context.Context, req ListProvidersPresetsRequest) (*ListProvidersPresetsResponse, error) {
	providers := s.ws.ProvidersPresets()
	resp := &ListProvidersPresetsResponse{
		Providers: make([]Provider, 0, len(providers)),
	}

	for _, p := range providers {
		displayName := p.Metadata.DisplayName
		if displayName == "" {
			displayName = p.Metadata.Name
		}
		resp.Providers = append(resp.Providers, Provider{
			Name:        p.Metadata.Name,
			DisplayName: displayName,
			Description: p.Spec.Type,
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
		resp.Tools = append(resp.Tools, Tool{
			Name:        t.Metadata.Name,
			Description: t.Metadata.Description,
			Category:    t.Metadata.Labels["category"],
			Labels:      t.Metadata.Labels,
		})
	}

	return resp, nil
}
