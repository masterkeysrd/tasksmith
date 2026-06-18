package tui

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/masterkeysrd/tasksmith/internal/tui/views/setup"
)

type AppProps struct {
	Client api.Client
}

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
		api.Provider(api.Props{Client: props.Client},
			theme.Provider(theme.Props{},
				components.Paper(components.PaperProps{
					Color: components.PaperBase,
					Style: SurfaceStyle,
				},
					setup.View(),
				),
			),
		),
	)
})
