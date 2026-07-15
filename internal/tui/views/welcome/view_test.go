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

func TestWelcomeView(t *testing.T) {
	thm := &theme.Scheme{}
	client := &tuiapi.MockClient{
		ListProjectsFunc: func(ctx context.Context, req api.ListProjectsRequest) (*api.ListProjectsResponse, error) {
			return &api.ListProjectsResponse{
				Projects: []api.Project{
					{Name: "test-project", DisplayName: "Test Project", Path: "/path/to/test"},
				},
			}, nil
		},
		ListAgentsFunc: func(ctx context.Context, req api.ListAgentsRequest) (*api.ListAgentsResponse, error) {
			return &api.ListAgentsResponse{
				Agents: []api.Agent{
					{Name: "test-agent", Description: "Test Agent Desc"},
				},
			}, nil
		},
		ListProvidersFunc: func(ctx context.Context, req api.ListProvidersRequest) (*api.ListProvidersResponse, error) {
			return &api.ListProvidersResponse{
				Providers: []api.Provider{
					{Name: "test-provider", DisplayName: "Test Provider"},
				},
			}, nil
		},
		GetWorkspaceConfigFunc: func(ctx context.Context, req api.GetWorkspaceConfigRequest) (*api.GetWorkspaceConfigResponse, error) {
			return &api.GetWorkspaceConfigResponse{
				Name:            "test-workspace",
				DefaultProvider: "test-provider",
				IsConfigured:    true,
			}, nil
		},
		ListSessionsFunc: func(ctx context.Context, req api.ListSessionsRequest) (*api.ListSessionsResponse, error) {
			return &api.ListSessionsResponse{}, nil
		},
	}
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
