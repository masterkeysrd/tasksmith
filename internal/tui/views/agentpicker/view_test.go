package agentpicker

import (
	"context"
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/tasksmith/internal/agent/model"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

func TestAgentPickerView(t *testing.T) {
	thm := &theme.Scheme{}
	client := &tuiapi.MockClient{
		ListAgentsFunc: func(ctx context.Context, req api.ListAgentsRequest) (*api.ListAgentsResponse, error) {
			return &api.ListAgentsResponse{
				Agents: []api.Agent{
					{Name: "explorer", Description: "Codebase explorer subagent"},
					{Name: "coder", Description: "Software engineer assistant"},
				},
			}, nil
		},
		ListSessionsFunc: func(ctx context.Context, req api.ListSessionsRequest) (*api.ListSessionsResponse, error) {
			return &api.ListSessionsResponse{
				Sessions: []api.Session{
					{
						ID:    "session_1",
						Title: "Session 1",
						Settings: model.SessionSettings{
							ProviderName: "ollama",
							ModelName:    "qwen",
							AgentName:    "explorer",
						},
					},
				},
			}, nil
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

	t.Run("Not Open", func(t *testing.T) {
		active.SetModal("")
		node := render(View(ViewProps{}))
		if node == nil {
			t.Fatal("Render returned nil node when closed (provider wrapper itself is non-nil)")
		}
	})

	t.Run("Open", func(t *testing.T) {
		active.SetModal("agentpicker")
		node := render(View(ViewProps{}))
		if node == nil {
			t.Fatal("Render returned nil node when open")
		}
	})
}
