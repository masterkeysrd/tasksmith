package tui

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/masterkeysrd/tasksmith/internal/tui/views/setup"
	"github.com/masterkeysrd/tasksmith/internal/tui/views/welcome"
)

type AppProps struct {
	Client tuiapi.Client
}

type ViewType string

const (
	ViewLoading ViewType = "loading"
	ViewWelcome ViewType = "welcome"
	ViewSetup   ViewType = "setup"
)

var (
	SurfaceStyle = style.S().
		Width(style.Percent(100)).
		Height(style.Percent(100))
)

var App = kitex.FC("App", func(props AppProps) kitex.Node {
	client := wind.NewClient()

	return wind.Provider(wind.ProviderProps{
		Client: client,
	},
		tuiapi.Provider(tuiapi.Props{Client: props.Client},
			theme.Provider(theme.Props{},
				components.Paper(components.PaperProps{
					Color: components.PaperBase,
					Style: SurfaceStyle,
				},
					MainContent(),
				),
			),
		),
	)
})

var MainContent = kitex.SimpleFC("MainContent", func() kitex.Node {
	wsCfg := queries.UseGetWorkspaceConfig()
	activeView, setActiveView := kitex.UseState(string(ViewLoading))
	windClient := wind.UseClient()

	kitex.UseEffect(func() {
		if !wsCfg.IsLoading {
			if wsCfg.Data != nil && wsCfg.Data.IsConfigured {
				setActiveView(string(ViewWelcome))
			} else {
				setActiveView(string(ViewSetup))
			}
		}
	}, []any{wsCfg.IsLoading})

	if activeView() == string(ViewLoading) {
		return components.Paper(components.PaperProps{
			Color: components.PaperBase,
			Style: SurfaceStyle.Merge(style.S().Display(style.DisplayFlex).AlignItems(style.AlignCenter).JustifyContent(style.JustifyCenter)),
		},
			kitex.Text("Loading workspace configurations..."),
		)
	}

	if activeView() == string(ViewWelcome) {
		return welcome.View(welcome.ViewProps{
			OnOpenSetupWizard: func() {
				setActiveView(string(ViewSetup))
			},
		})
	}

	return setup.View(setup.ViewProps{
		OnComplete: func() {
			windClient.InvalidateQueries(api.GetWorkspaceConfigRequest{})
			setActiveView(string(ViewWelcome))
		},
		OnSkip: func() {
			setActiveView(string(ViewWelcome))
		},
	})
})
