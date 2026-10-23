package repl

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
	uiclient "github.com/masterkeysrd/tasksmith/internal/ui/client"
	uihooks "github.com/masterkeysrd/tasksmith/internal/ui/hooks"
	uitheme "github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// AppProps configures the root REPL trail functional component.
type AppProps struct {
	Client            *client.Client
	Workspace         string
	Version           string
	SessionID         string
	Agent             string
	Model             string
	Title             string
	InitialSess       *api.Session
	InitialMode       api.PermissionMode
	InitialModelLabel string
	InitialThinking   string
	Registry          *command.Registry
}

// App is the root trail functional component for the TaskSmith REPL. It only
// composes providers and lays out the two regions of the screen; every
// component below reads what it needs through hooks.
var App = trail.FC("App", func(props AppProps) trail.Node {
	queryClient := kitex.UseMemo(wind.NewClient, []any{})
	return uiclient.Provider(uiclient.Props{Client: props.Client},
		wind.Provider(wind.ProviderProps{Client: queryClient},
			uitheme.Provider(uitheme.Props{Transparent: true},
				replProviders(props, trail.Fragment(
					TranscriptOutput(),
					ActiveArea(),
				)),
			),
		),
	)
})

// replProviders nests the REPL's providers around children. The order matters:
// each provider may use the ones above it.
func replProviders(props AppProps, children ...trail.Node) trail.Node {
	return uihooks.SessionProvider(uihooks.SessionProviderProps{
		Initial:         props.InitialSess,
		Agent:           props.Agent,
		Model:           props.Model,
		Mode:            props.InitialMode,
		InitialModel:    props.InitialModelLabel,
		InitialThinking: props.InitialThinking,
	}, uihooks.ConversationProvider(uihooks.ConversationProviderProps{},
		TranscriptProvider(TranscriptProviderProps{Version: props.Version, Workspace: props.Workspace},
			OverlayProvider(OverlayProviderProps{},
				SessionActionsProvider(SessionActionsProviderProps{},
					PermissionsProvider(PermissionsProviderProps{},
						SendProvider(SendProviderProps{},
							CommandsProvider(CommandsProviderProps{Registry: props.Registry, Agent: props.Agent, Model: props.Model},
								StatusScope(StatusScopeProps{}, children...),
							),
						),
					),
				),
			),
		),
	))
}
