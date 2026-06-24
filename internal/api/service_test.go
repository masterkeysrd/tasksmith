package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
	"github.com/masterkeysrd/warp"
)

type mockWorkspace struct {
	projects         []*warp.Project
	agents           []*warp.Agent
	providers        []*warp.ModelProvider
	providersPresets []*warp.ModelProvider
	toolsPresets     []*warp.Tool
	cwd              string
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

func (m *mockWorkspace) GetWorkspaceConfig(ctx context.Context) (workspace.WorkspaceConfig, error) {
	return workspace.WorkspaceConfig{
		CWD: m.cwd,
	}, nil
}

func TestService(t *testing.T) {
	mockWS := &mockWorkspace{
		cwd: t.TempDir(),
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

	svc := NewService(mockWS, nil, nil, nil)
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

	t.Run("GetTokenAnalytics", func(t *testing.T) {
		resp, err := svc.GetTokenAnalytics(ctx, GetTokenAnalyticsRequest{
			Timeframe: "7days",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if len(resp.ProvidersList) != 0 {
			t.Errorf("expected empty providers list, got %v", resp.ProvidersList)
		}
		if resp.GlobalStats.TotalCalls != 0 {
			t.Errorf("expected 0 total calls, got %d", resp.GlobalStats.TotalCalls)
		}
	})

	t.Run("GetSessionStateNilManager", func(t *testing.T) {
		resp, err := svc.GetSessionState(ctx, GetSessionStateRequest{SessionID: "s1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Status != "idle" {
			t.Errorf("expected status idle, got %s", resp.Status)
		}
	})

	t.Run("SubmitAuthorizationDecisionNilManager", func(t *testing.T) {
		_, err := svc.SubmitAuthorizationDecision(ctx, SubmitAuthorizationDecisionRequest{
			SessionID: "s1",
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("ConfigureAndDismissLsp", func(t *testing.T) {
		origXDG := os.Getenv("XDG_CONFIG_HOME")
		tempConfigDir := t.TempDir()
		os.Setenv("XDG_CONFIG_HOME", tempConfigDir)
		defer os.Setenv("XDG_CONFIG_HOME", origXDG)

		xdg.ClearCache()

		mgr := lsp.NewManager()
		svcWithLsp := NewService(mockWS, nil, nil, mgr)

		// Add dummy file change to trigger suggestion for "go"
		absGo, _ := filepath.Abs("main.go")
		t.Logf("absGo: %s", absGo)
		mgr.NotifyFileChanged(ctx, "main.go", "package main")

		// Retrieve session state and verify suggestion is present
		stateResp, err := svcWithLsp.GetSessionState(ctx, GetSessionStateRequest{SessionID: "s1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		t.Logf("PendingLspSuggestions: %+v", stateResp.PendingLspSuggestions)
		foundGo := false
		for _, sug := range stateResp.PendingLspSuggestions {
			if sug.Language == "go" {
				foundGo = true
				break
			}
		}
		if !foundGo {
			t.Error("expected 'go' suggestion in session state")
		}

		// Configure LSP
		confResp, err := svcWithLsp.ConfigureLsp(ctx, ConfigureLspRequest{Language: "go"})
		if err != nil {
			t.Fatalf("unexpected error configuring lsp: %v", err)
		}
		if !confResp.Success {
			t.Error("expected configuration success")
		}

		// Check suggestion is dismissed after configuration
		stateResp2, err := svcWithLsp.GetSessionState(ctx, GetSessionStateRequest{SessionID: "s1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, sug := range stateResp2.PendingLspSuggestions {
			if sug.Language == "go" {
				t.Error("expected 'go' suggestion to be dismissed after configuration")
			}
		}

		// Configure unknown language preset
		_, err = svcWithLsp.ConfigureLsp(ctx, ConfigureLspRequest{Language: "unknown_lang"})
		if err == nil {
			t.Error("expected error configuring unknown preset")
		}

		// Trigger suggestion for another language, e.g. "python"
		mgr.NotifyFileChanged(ctx, "main.py", "import sys")

		// Dismiss suggestion
		dismissResp, err := svcWithLsp.DismissLspSuggestion(ctx, DismissLspSuggestionRequest{Language: "python"})
		if err != nil {
			t.Fatalf("unexpected error dismissing: %v", err)
		}
		if !dismissResp.Success {
			t.Error("expected dismiss success")
		}

		// Check suggestion is dismissed
		stateResp3, err := svcWithLsp.GetSessionState(ctx, GetSessionStateRequest{SessionID: "s1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, sug := range stateResp3.PendingLspSuggestions {
			if sug.Language == "python" {
				t.Error("expected 'python' suggestion to be dismissed")
			}
		}
	})
}
