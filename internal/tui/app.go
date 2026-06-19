package tui

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/shell"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/masterkeysrd/tasksmith/internal/tui/views/chat"
	"github.com/masterkeysrd/tasksmith/internal/tui/views/setup"
	"github.com/masterkeysrd/tasksmith/internal/tui/views/welcome"
)

// AppProps defines the top-level application properties.
type AppProps struct {
	Client tuiapi.Client
}

type viewType string

const (
	viewLoading viewType = "loading"
	viewWelcome viewType = "welcome"
	viewSetup   viewType = "setup"
	viewMain    viewType = "main"
)

// App is the root component. It sets up all providers and renders the router.
var App = kitex.FC("App", func(props AppProps) kitex.Node {
	return wind.Provider(wind.ProviderProps{Client: wind.NewClient()},
		tuiapi.Provider(tuiapi.Props{Client: props.Client},
			theme.Provider(theme.Props{},
				components.Paper(components.PaperProps{
					Color: components.PaperBase,
					Style: style.S().Width(style.Percent(100)).Height(style.Percent(100)),
				},
					Router(),
				),
			),
		),
	)
})

// Router switches between views. Setup and Welcome render standalone (no shell).
// The main workspace view renders inside the Shell.
var Router = kitex.SimpleFC("Router", func() kitex.Node {
	wsCfg := queries.UseGetWorkspaceConfig()
	activeView, setActiveView := kitex.UseState(string(viewLoading))
	windClient := wind.UseClient()

	kitex.UseEffect(func() {
		if !wsCfg.IsLoading {
			if wsCfg.Data != nil && wsCfg.Data.IsConfigured {
				setActiveView(string(viewWelcome))
			} else {
				setActiveView(string(viewSetup))
			}
		}
	}, []any{wsCfg.IsLoading})

	switch viewType(activeView()) {
	case viewLoading:
		return components.Paper(components.PaperProps{
			Color: components.PaperBase,
			Style: style.S().
				Width(style.Percent(100)).
				Height(style.Percent(100)).
				Display(style.DisplayFlex).
				AlignItems(style.AlignCenter).
				JustifyContent(style.JustifyCenter),
		}, kitex.Text("Loading workspace..."))

	case viewWelcome:
		return welcome.View(welcome.ViewProps{
			OnOpenSetupWizard: func() { setActiveView(string(viewSetup)) },
			OnNewSession:      func() { setActiveView(string(viewMain)) },
			OnOpenSession:     func(_ string) { setActiveView(string(viewMain)) },
		})

	case viewSetup:
		return setup.View(setup.ViewProps{
			OnComplete: func() {
				windClient.InvalidateQueries(api.GetWorkspaceConfigRequest{})
				setActiveView(string(viewMain))
			},
			OnSkip: func() { setActiveView(string(viewWelcome)) },
		})

	default: // viewMain — shell with active workspace view
		return shell.View(shell.Props{
			OnOpenSetupWizard: func() { setActiveView(string(viewSetup)) },
		},
			chat.View(chat.ViewProps{}),
		)
	}
})
