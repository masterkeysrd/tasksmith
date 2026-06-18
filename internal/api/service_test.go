package api

import (
	"context"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/workspace"
	"github.com/masterkeysrd/warp"
)

type mockWorkspace struct {
	projects         []*warp.Project
	agents           []*warp.Agent
	providers        []*warp.ModelProvider
	providersPresets []*warp.ModelProvider
	toolsPresets     []*warp.Tool
}

func (m *mockWorkspace) Projects() []*warp.Project {
	return m.projects
}

func (m *mockWorkspace) Agents() []*warp.Agent {
	return m.agents
}

func (m *mockWorkspace) Providers() []*warp.ModelProvider {
	return m.providers
}

func (m *mockWorkspace) ProvidersPresets() []*warp.ModelProvider {
	return m.providersPresets
}

func (m *mockWorkspace) ToolsPresets() []*warp.Tool {
	return m.toolsPresets
}

func (m *mockWorkspace) Initialize(ctx context.Context, opts workspace.InitializationOptions) error {
	return nil
}

func TestService(t *testing.T) {
	mockWS := &mockWorkspace{
		projects: []*warp.Project{
			{Name: "p1", Path: "/path/1"},
		},
		agents: []*warp.Agent{
			{
				BaseResource: warp.BaseResource{
					Metadata: warp.Metadata{Name: "a1", Description: "d1"},
				},
			},
		},
		providers: []*warp.ModelProvider{
			{
				BaseResource: warp.BaseResource{
					Metadata: warp.Metadata{Name: "pr1", DisplayName: "Provider 1"},
				},
				Spec: warp.ModelProviderSpec{Type: "openai"},
			},
		},
		providersPresets: []*warp.ModelProvider{
			{
				BaseResource: warp.BaseResource{
					Metadata: warp.Metadata{Name: "preset1", DisplayName: "Preset 1"},
				},
				Spec: warp.ModelProviderSpec{Type: "anthropic"},
			},
		},
		toolsPresets: []*warp.Tool{
			{
				BaseResource: warp.BaseResource{
					Metadata: warp.Metadata{
						Name:        "tool1",
						Description: "desc1",
						Labels:      map[string]string{"category": "cat1"},
					},
				},
			},
		},
	}

	svc := NewService(mockWS)
	ctx := context.Background()

	t.Run("ListProjects", func(t *testing.T) {
		resp, err := svc.ListProjects(ctx, ListProjectsRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Projects) != 1 {
			t.Errorf("expected 1 project, got %d", len(resp.Projects))
		}
		if resp.Projects[0].Name != "p1" {
			t.Errorf("expected name p1, got %s", resp.Projects[0].Name)
		}
	})

	t.Run("ListAgents", func(t *testing.T) {
		resp, err := svc.ListAgents(ctx, ListAgentsRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Agents) != 1 {
			t.Errorf("expected 1 agent, got %d", len(resp.Agents))
		}
		if resp.Agents[0].Name != "a1" {
			t.Errorf("expected name a1, got %s", resp.Agents[0].Name)
		}
	})

	t.Run("ListProviders", func(t *testing.T) {
		resp, err := svc.ListProviders(ctx, ListProvidersRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Providers) != 1 {
			t.Errorf("expected 1 provider, got %d", len(resp.Providers))
		}
		if resp.Providers[0].DisplayName != "Provider 1" {
			t.Errorf("expected DisplayName 'Provider 1', got %s", resp.Providers[0].DisplayName)
		}
	})

	t.Run("ListProvidersPresets", func(t *testing.T) {
		resp, err := svc.ListProvidersPresets(ctx, ListProvidersPresetsRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Providers) != 1 {
			t.Errorf("expected 1 provider preset, got %d", len(resp.Providers))
		}
		if resp.Providers[0].Name != "preset1" {
			t.Errorf("expected name preset1, got %s", resp.Providers[0].Name)
		}
	})

	t.Run("ListToolsPresets", func(t *testing.T) {
		resp, err := svc.ListToolsPresets(ctx, ListToolsPresetsRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Tools) != 1 {
			t.Errorf("expected 1 tool preset, got %d", len(resp.Tools))
		}
		if resp.Tools[0].Name != "tool1" {
			t.Errorf("expected name tool1, got %s", resp.Tools[0].Name)
		}
		if resp.Tools[0].Category != "cat1" {
			t.Errorf("expected category cat1, got %s", resp.Tools[0].Category)
		}
	})
}
