package modelpicker

import (
	"context"
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

func TestModelPickerView(t *testing.T) {
	thm := &theme.Scheme{}
	client := &tuiapi.MockClient{
		ListProvidersFunc: func(ctx context.Context, req api.ListProvidersRequest) (*api.ListProvidersResponse, error) {
			return &api.ListProvidersResponse{
				Providers: []api.Provider{
					{
						Name:        "genai",
						DisplayName: "Google GenAI",
						Models: []api.Model{
							{ID: "gemini-3.5-flash", Name: "Gemini 3.5 Flash", ContextWindow: 1048576},
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
		active.SetModal("modelpicker")
		node := render(View(ViewProps{}))
		if node == nil {
			t.Fatal("Render returned nil node when open")
		}
	})
}
