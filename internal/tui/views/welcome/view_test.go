package welcome

import (
	"context"
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/tasksmith/internal/api"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type mockClient struct{}

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

func (m *mockClient) DeleteSession(ctx context.Context, req api.DeleteSessionRequest) (*api.DeleteSessionResponse, error) {
	return &api.DeleteSessionResponse{Success: true}, nil
}

func (m *mockClient) SendMessage(ctx context.Context, req api.SendMessageRequest) (*api.SendMessageResponse, error) {
	return &api.SendMessageResponse{Success: true}, nil
}

func (m *mockClient) GetSessionMessages(ctx context.Context, req api.GetSessionMessagesRequest) (*api.GetSessionMessagesResponse, error) {
	return &api.GetSessionMessagesResponse{}, nil
}

func (m *mockClient) GetSessionState(ctx context.Context, req api.GetSessionStateRequest) (*api.GetSessionStateResponse, error) {
	return &api.GetSessionStateResponse{Status: "idle"}, nil
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
