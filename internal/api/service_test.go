package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/agent/model"
	coredb "github.com/masterkeysrd/tasksmith/internal/core/db"
	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/filetrack"
	"github.com/masterkeysrd/tasksmith/internal/session"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
	"github.com/masterkeysrd/warp"
)

type mockWorkspace struct {
	projects         []*warp.Project
	agents           []*warp.Agent
	providers        []*warp.ModelProvider
	providersPresets []*warp.ModelProvider
	toolsPresets     []*warp.Tool
	mcps             []*warp.MCP
	cwd              string
	initOpts         workspace.InitializationOptions
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

func (m *mockWorkspace) MCPs() []*warp.MCP {
	return m.mcps
}

func (m *mockWorkspace) Initialize(ctx context.Context, opts workspace.InitializationOptions) error {
	m.initOpts = opts
	return nil
}

func (m *mockWorkspace) GetWorkspaceConfig(ctx context.Context) (workspace.WorkspaceConfig, error) {
	return workspace.WorkspaceConfig{
		CWD: m.cwd,
	}, nil
}

func (m *mockWorkspace) ResolveDefaults(ctx context.Context) (agentName, providerName, modelName string, err error) {
	return "main", "", "", nil
}

func (m *mockWorkspace) ResolveAgent(ctx context.Context, ref string) (*warp.ResolvedAgent, error) {
	return nil, nil
}

func (m *mockWorkspace) AuthorizeTools(ctx context.Context, tools []string) error {
	return nil
}

func (m *mockWorkspace) ListResources(opts warp.QueryOptions) []warp.Resource {
	var results []warp.Resource
	for _, k := range opts.Kinds {
		switch k {
		case warp.KindAgent:
			for _, a := range m.agents {
				if opts.Filter == nil || opts.Filter(a) {
					results = append(results, a)
				}
			}
		case warp.KindModelProvider:
			for _, p := range m.providers {
				if opts.Filter == nil || opts.Filter(p) {
					results = append(results, p)
				}
			}
		case warp.KindMCP:
			for _, mc := range m.mcps {
				if opts.Filter == nil || opts.Filter(mc) {
					results = append(results, mc)
				}
			}
		}
	}
	return results
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

	svc := NewService(mockWS, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("InitializeWorkspace", func(t *testing.T) {
		req := InitializeWorkspaceRequest{
			ProjectName:      "test-p",
			SelectedProvider: "openai",
			TrustOnly:        true,
		}
		_, err := svc.InitializeWorkspace(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mockWS.initOpts.ProjectName != "test-p" {
			t.Errorf("expected ProjectName 'test-p', got %s", mockWS.initOpts.ProjectName)
		}
		if mockWS.initOpts.SelectedProvider != "openai" {
			t.Errorf("expected SelectedProvider 'openai', got %s", mockWS.initOpts.SelectedProvider)
		}
		if !mockWS.initOpts.TrustOnly {
			t.Error("expected TrustOnly to be true")
		}
	})

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
		tempConfigDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tempConfigDir)
		xdg.ClearCache()

		mgr := lsp.NewManager()
		svcWithLsp := NewService(mockWS, nil, nil, mgr, nil)

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

func TestRevertFileAPI(t *testing.T) {
	tmpDir := t.TempDir()

	db, err := coredb.Open(tmpDir, "tasksmith.db")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	checkpointsDB, err := coredb.Open(tmpDir, "checkpoints.db")
	if err != nil {
		t.Fatalf("failed to open checkpoints database: %v", err)
	}
	defer checkpointsDB.Close()

	store, err := session.NewSQLiteStore(db, checkpointsDB)
	if err != nil {
		t.Fatalf("failed to initialize sqlite store: %v", err)
	}

	wsMock := &mockWorkspace{cwd: tmpDir}
	ws := workspace.New(tmpDir)
	wsMock.cwd = ws.CWD()

	manager := session.NewManager(session.ManagerConfig{
		Store:     store,
		Workspace: ws,
	})

	svc := NewService(wsMock, manager, nil, nil, nil)
	ctx := context.Background()

	sess, err := manager.CreateSession(ctx, "Test Session")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	filePath := "test.txt"
	absPath := filepath.Join(tmpDir, filePath)
	if err := os.WriteFile(absPath, []byte("original content"), 0644); err != nil {
		t.Fatalf("failed to write original content: %v", err)
	}

	ft, err := manager.FileTracker(sess.ID)
	if err != nil {
		t.Fatalf("failed to get file tracker: %v", err)
	}

	// Modify file on disk to simulate agent edit
	if err := os.WriteFile(absPath, []byte("agent edited content"), 0644); err != nil {
		t.Fatalf("failed to write agent content: %v", err)
	}

	err = ft.Record(ctx, filetrack.Change{
		ToolName:  "edit",
		Path:      filePath,
		Kind:      filetrack.Modified,
		Additions: 1,
		Deletions: 1,
	}, "diff content", "original content")
	if err != nil {
		t.Fatalf("failed to record change: %v", err)
	}

	// Simulate user manual edit
	if err := os.WriteFile(absPath, []byte("user manual edited content"), 0644); err != nil {
		t.Fatalf("failed to write user content: %v", err)
	}

	// Try to revert without forcing - should return conflict error
	revertResp, err := svc.RevertFile(ctx, RevertFileRequest{
		SessionID: sess.ID,
		Path:      filePath,
		Force:     false,
	})
	if err != nil {
		t.Fatalf("RevertFile failed: %v", err)
	}
	if revertResp.Success || revertResp.Error != "conflict" {
		t.Errorf("expected conflict error, got success=%t, err=%q", revertResp.Success, revertResp.Error)
	}

	// Force revert - should succeed and restore original content
	revertResp2, err := svc.RevertFile(ctx, RevertFileRequest{
		SessionID: sess.ID,
		Path:      filePath,
		Force:     true,
	})
	if err != nil {
		t.Fatalf("RevertFile with force failed: %v", err)
	}
	if !revertResp2.Success {
		t.Errorf("expected force revert success, got error: %s", revertResp2.Error)
	}

	// Verify file is restored to baseline
	data, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("failed to read reverted file: %v", err)
	}
	if string(data) != "original content" {
		t.Errorf("expected 'original content', got %q", string(data))
	}
}

func TestConfigureSession(t *testing.T) {
	tmpDir := t.TempDir()

	db, err := coredb.Open(tmpDir, "tasksmith.db")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	checkpointsDB, err := coredb.Open(tmpDir, "checkpoints.db")
	if err != nil {
		t.Fatalf("failed to open checkpoints database: %v", err)
	}
	defer checkpointsDB.Close()

	store, err := session.NewSQLiteStore(db, checkpointsDB)
	if err != nil {
		t.Fatalf("failed to initialize sqlite store: %v", err)
	}

	tempFalse := false
	tempTrue := true
	wsMock := &mockWorkspace{
		cwd: tmpDir,
		providers: []*warp.ModelProvider{
			{
				BaseResource: warp.BaseResource{
					Metadata: warp.Metadata{Name: "openai", DisplayName: "OpenAI"},
				},
				Spec: warp.ModelProviderSpec{
					Type: "openai",
					Models: []warp.ProviderModel{
						{
							ID:    "gpt-4o",
							Name:  "gpt-4o",
							Label: "GPT-4o",
							Capabilities: &warp.ProviderModelCapabilities{
								Temperature: &tempTrue,
							},
						},
						{
							ID:    "o1-mini",
							Name:  "o1-mini",
							Label: "o1-mini",
							Capabilities: &warp.ProviderModelCapabilities{
								Temperature: &tempFalse,
							},
						},
					},
				},
			},
		},
	}

	ws := workspace.New(tmpDir)
	wsMock.cwd = ws.CWD()

	manager := session.NewManager(session.ManagerConfig{
		Store:     store,
		Workspace: ws,
	})

	svc := NewService(wsMock, manager, nil, nil, nil)
	ctx := context.Background()

	sess, err := manager.CreateSession(ctx, "Test Session")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	tempVal := 0.7
	_, err = svc.ConfigureSession(ctx, ConfigureSessionRequest{
		SessionID: sess.ID,
		Settings: &model.SessionSettings{
			ProviderName: "openai",
			ModelName:    "gpt-4o",
			Temperature:  &tempVal,
		},
	})
	if err != nil {
		t.Fatalf("expected success configuring temperature on gpt-4o, got: %v", err)
	}

	_, err = svc.ConfigureSession(ctx, ConfigureSessionRequest{
		SessionID: sess.ID,
		Settings: &model.SessionSettings{
			ProviderName: "openai",
			ModelName:    "o1-mini",
			Temperature:  &tempVal,
		},
	})
	if err == nil {
		t.Fatal("expected error when setting temperature on o1-mini, got nil")
	}

	_, err = svc.ConfigureSession(ctx, ConfigureSessionRequest{
		SessionID: sess.ID,
		Settings: &model.SessionSettings{
			ProviderName: "openai",
			ModelName:    "o1-mini",
		},
	})
	if err != nil {
		t.Fatalf("expected success switching model without setting temperature, got: %v", err)
	}

	t.Run("GetSessionStateLastTurnMetrics", func(t *testing.T) {
		sess2, err := manager.CreateSession(ctx, "Test Session For Metrics")
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}

		metrics := session.SessionMetrics{
			PromptTokens:               100,
			CompletionTokens:           50,
			TotalTokens:                150,
			EstimatedCostUSD:           0.005,
			CumulativePromptTokens:     1000,
			CumulativeCompletionTokens: 500,
			CumulativeTotalTokens:      1500,
			CumulativeCostUSD:          0.05,
		}
		err = store.UpdateSessionMetrics(ctx, sess2.ID, metrics)
		if err != nil {
			t.Fatalf("failed to update session metrics: %v", err)
		}

		resp, err := svc.GetSessionState(ctx, GetSessionStateRequest{SessionID: sess2.ID})
		if err != nil {
			t.Fatalf("failed to get session state: %v", err)
		}

		if resp.LastTurnMetrics == nil {
			t.Fatal("expected LastTurnMetrics to be populated, got nil")
		}

		if resp.LastTurnMetrics.CumulativeTotalTokens != 1500 {
			t.Errorf("expected CumulativeTotalTokens 1500, got %d", resp.LastTurnMetrics.CumulativeTotalTokens)
		}
	})
}
