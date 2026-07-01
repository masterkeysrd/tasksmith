package welcome

import (
	"context"
	"iter"
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/tasksmith/internal/api"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type mockClient struct {
	tuiapi.Client
}

func (m *mockClient) ListProjects(ctx context.Context, req api.ListProjectsRequest) (*api.ListProjectsResponse, error) {
	return &api.ListProjectsResponse{
		Projects: []api.Project{
			{Name: "test-project", DisplayName: "Test Project", Path: "/path/to/test"},
		},
	}, nil
}

func (m *mockClient) ListAgents(ctx context.Context, req api.ListAgentsRequest) (*api.ListAgentsResponse, error) {
	return &api.ListAgentsResponse{
		Agents: []api.Agent{
			{Name: "test-agent", Description: "Test Agent Desc"},
		},
	}, nil
}

func (m *mockClient) ListProviders(ctx context.Context, req api.ListProvidersRequest) (*api.ListProvidersResponse, error) {
	return &api.ListProvidersResponse{
		Providers: []api.Provider{
			{Name: "test-provider", DisplayName: "Test Provider"},
		},
	}, nil
}

func (m *mockClient) ListProvidersPresets(ctx context.Context, req api.ListProvidersPresetsRequest) (*api.ListProvidersPresetsResponse, error) {
	return &api.ListProvidersPresetsResponse{}, nil
}

func (m *mockClient) ListToolsPresets(ctx context.Context, req api.ListToolsPresetsRequest) (*api.ListToolsPresetsResponse, error) {
	return &api.ListToolsPresetsResponse{}, nil
}

func (m *mockClient) InitializeWorkspace(ctx context.Context, req api.InitializeWorkspaceRequest) (*api.InitializeWorkspaceResponse, error) {
	return &api.InitializeWorkspaceResponse{Success: true}, nil
}

func (m *mockClient) GetWorkspaceConfig(ctx context.Context, req api.GetWorkspaceConfigRequest) (*api.GetWorkspaceConfigResponse, error) {
	return &api.GetWorkspaceConfigResponse{
		Name:            "test-workspace",
		DefaultProvider: "test-provider",
		IsConfigured:    true,
	}, nil
}

func (m *mockClient) ListSessions(ctx context.Context, req api.ListSessionsRequest) (*api.ListSessionsResponse, error) {
	return &api.ListSessionsResponse{}, nil
}

func (m *mockClient) CreateSession(ctx context.Context, req api.CreateSessionRequest) (*api.CreateSessionResponse, error) {
	return &api.CreateSessionResponse{
		Session: api.Session{ID: "test-session-id", Title: req.Title},
	}, nil
}

func (m *mockClient) ConfigureSession(ctx context.Context, req api.ConfigureSessionRequest) (*api.ConfigureSessionResponse, error) {
	return &api.ConfigureSessionResponse{Success: true}, nil
}

func (m *mockClient) DeleteSession(ctx context.Context, req api.DeleteSessionRequest) (*api.DeleteSessionResponse, error) {
	return &api.DeleteSessionResponse{Success: true}, nil
}

func (m *mockClient) RenameSession(ctx context.Context, req api.RenameSessionRequest) (*api.RenameSessionResponse, error) {
	return &api.RenameSessionResponse{Success: true}, nil
}

func (m *mockClient) ArchiveSession(ctx context.Context, req api.ArchiveSessionRequest) (*api.ArchiveSessionResponse, error) {
	return &api.ArchiveSessionResponse{Success: true}, nil
}

func (m *mockClient) SendMessage(ctx context.Context, req api.SendMessageRequest) (*api.SendMessageResponse, error) {
	return &api.SendMessageResponse{Success: true}, nil
}

func (m *mockClient) GetSessionMessages(ctx context.Context, req api.GetSessionMessagesRequest) (*api.GetSessionMessagesResponse, error) {
	return &api.GetSessionMessagesResponse{}, nil
}

func (m *mockClient) WatchSessionMessages(ctx context.Context, req api.GetSessionMessagesRequest) iter.Seq2[*api.GetSessionMessagesResponse, error] {
	return func(yield func(*api.GetSessionMessagesResponse, error) bool) {}
}

func (m *mockClient) GetSessionState(ctx context.Context, req api.GetSessionStateRequest) (*api.GetSessionStateResponse, error) {
	return &api.GetSessionStateResponse{Status: "idle"}, nil
}

func (m *mockClient) SubmitAuthorizationDecision(ctx context.Context, req api.SubmitAuthorizationDecisionRequest) (*api.SubmitAuthorizationDecisionResponse, error) {
	return &api.SubmitAuthorizationDecisionResponse{Success: true}, nil
}

func (m *mockClient) ResolveMcpRequest(ctx context.Context, req api.ResolveMcpRequest) (*api.ResolveMcpResponse, error) {
	return &api.ResolveMcpResponse{Success: true}, nil
}

func (m *mockClient) GetTokenAnalytics(ctx context.Context, req api.GetTokenAnalyticsRequest) (*api.GetTokenAnalyticsResponse, error) {
	return &api.GetTokenAnalyticsResponse{}, nil
}

func (m *mockClient) ConfigureLsp(ctx context.Context, req api.ConfigureLspRequest) (*api.ConfigureLspResponse, error) {
	return &api.ConfigureLspResponse{Success: true}, nil
}

func (m *mockClient) DismissLspSuggestion(ctx context.Context, req api.DismissLspSuggestionRequest) (*api.DismissLspSuggestionResponse, error) {
	return &api.DismissLspSuggestionResponse{Success: true}, nil
}

func (m *mockClient) GetLspStatus(ctx context.Context, req api.GetLspStatusRequest) (*api.GetLspStatusResponse, error) {
	return &api.GetLspStatusResponse{}, nil
}

func (m *mockClient) RestartLsp(ctx context.Context, req api.RestartLspRequest) (*api.RestartLspResponse, error) {
	return &api.RestartLspResponse{Success: true}, nil
}

func (m *mockClient) RestartMcp(ctx context.Context, req api.RestartMcpRequest) (*api.RestartMcpResponse, error) {
	return &api.RestartMcpResponse{Success: true}, nil
}

func (m *mockClient) GetLspDiagnosticCounts(ctx context.Context, req api.GetLspDiagnosticCountsRequest) (*api.GetLspDiagnosticCountsResponse, error) {
	return &api.GetLspDiagnosticCountsResponse{}, nil
}

func (m *mockClient) GetLspDiagnostics(ctx context.Context, req api.GetLspDiagnosticsRequest) (*api.GetLspDiagnosticsResponse, error) {
	return &api.GetLspDiagnosticsResponse{}, nil
}

func (m *mockClient) LspSymbols(ctx context.Context, req api.LspSymbolsRequest) (*api.LspSymbolsResponse, error) {
	return &api.LspSymbolsResponse{}, nil
}

func (m *mockClient) GetFileChanges(ctx context.Context, req api.GetFileChangesRequest) (*api.GetFileChangesResponse, error) {
	return &api.GetFileChangesResponse{}, nil
}

func (m *mockClient) GetFileJournal(ctx context.Context, req api.GetFileJournalRequest) (*api.GetFileJournalResponse, error) {
	return &api.GetFileJournalResponse{}, nil
}

func (m *mockClient) RevertFile(ctx context.Context, req api.RevertFileRequest) (*api.RevertFileResponse, error) {
	return &api.RevertFileResponse{Success: true}, nil
}

func (m *mockClient) GetCachedFile(ctx context.Context, req api.GetCachedFileRequest) (*api.GetCachedFileResponse, error) {
	return &api.GetCachedFileResponse{Content: "mock cached file content"}, nil
}

func (m *mockClient) GetMcpStatus(ctx context.Context, req api.GetMcpStatusRequest) (*api.GetMcpStatusResponse, error) {
	return &api.GetMcpStatusResponse{}, nil
}

func (m *mockClient) SetPermissionMode(ctx context.Context, req api.SetPermissionModeRequest) (*api.SetPermissionModeResponse, error) {
	return &api.SetPermissionModeResponse{Success: true}, nil
}

func TestWelcomeView(t *testing.T) {
	thm := &theme.Scheme{}
	client := &mockClient{}
	windClient := wind.NewClient()

	render := func(node kitex.Node) kitex.Node {
		return wind.Provider(wind.ProviderProps{Client: windClient},
			tuiapi.Provider(tuiapi.Props{Client: client},
				theme.Provider(theme.Props{Theme: thm}, node),
			),
		)
	}

	t.Run("RenderWelcome", func(t *testing.T) {
		node := render(View(ViewProps{
			OnOpenSetupWizard: func() {},
		}))
		if node == nil {
			t.Fatal("Welcome view returned nil node")
		}
	})
}
