package tui

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/shell"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/masterkeysrd/tasksmith/internal/tui/toast"
	"github.com/masterkeysrd/tasksmith/internal/tui/views/analytics"
	"github.com/masterkeysrd/tasksmith/internal/tui/views/chat"
	"github.com/masterkeysrd/tasksmith/internal/tui/views/lspinfo"
	"github.com/masterkeysrd/tasksmith/internal/tui/views/mcpinfo"
	"github.com/masterkeysrd/tasksmith/internal/tui/views/modelpicker"
	permissionsview "github.com/masterkeysrd/tasksmith/internal/tui/views/permissions"
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
					kitex.Fragment(
						Router(),
						toast.ToastContainer(),
					),
				),
			),
		),
	)
})

// Router switches between views. Setup and Welcome render standalone (no shell).
// The main workspace view renders inside the Shell.
var Router = kitex.SimpleFC("Router", func() kitex.Node {
	wsCfg := queries.UseGetWorkspaceConfig()
	providers := queries.UseListProviders()
	activeView, setActiveView := kitex.UseState(string(viewLoading))
	activeSessionID := active.UseSessionID()
	activeScreen := active.UseScreen()
	windClient := wind.UseClient()

	kitex.UseEffect(func() {
		active.InvalidateSessionState = func(sessionID string) {
			windClient.InvalidateQueries(api.GetSessionStateRequest{SessionID: sessionID})
		}
	}, []any{windClient})

	kitex.UseEffect(func() {
		if !wsCfg.IsLoading && !providers.IsLoading {
			hasConfiguredProvider := providers.Data != nil && len(providers.Data.Providers) > 0
			hasSelectedProvider := wsCfg.Data != nil && wsCfg.Data.DefaultProvider != ""
			isConfigured := wsCfg.Data != nil && wsCfg.Data.IsConfigured

			if isConfigured && hasSelectedProvider && hasConfiguredProvider {
				setActiveView(string(viewWelcome))
			} else {
				setActiveView(string(viewSetup))
			}
		}
	}, []any{wsCfg.IsLoading, providers.IsLoading, wsCfg.Data, providers.Data})

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
		if activeScreen == "analytics" {
			return analytics.View(analytics.Props{
				OnClose: func() { active.SetScreen("chat") },
			})
		}
		return welcome.View(welcome.ViewProps{
			OnOpenSetupWizard: func() { setActiveView(string(viewSetup)) },
			OnNewSession: func(id string) {
				active.SetSessionID(id)
				setActiveView(string(viewMain))
			},
			OnOpenSession: func(id string) {
				active.SetSessionID(id)
				setActiveView(string(viewMain))
			},
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
		if activeScreen == "analytics" {
			return shell.View(shell.Props{},
				analytics.View(analytics.Props{
					OnClose: func() { active.SetScreen("chat") },
				}),
			)
		}
		return shell.View(shell.Props{},
			kitex.Fragment(
				chat.View(chat.ViewProps{SessionID: activeSessionID}),
				lspinfo.View(lspinfo.ViewProps{}),
				mcpinfo.View(mcpinfo.ViewProps{}),
				modelpicker.View(modelpicker.ViewProps{}),
				permissionsview.View(permissionsview.ViewProps{}),
			),
		)
	}
})
