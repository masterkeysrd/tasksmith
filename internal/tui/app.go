package tui

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/colorscheme"
	"github.com/masterkeysrd/tasksmith/internal/tui/highlight"
)

type AppProps struct {
	Client api.Client
}

var App = kitex.FC("App", func(props AppProps) kitex.Node {
	cs, err := colorscheme.Find(colorscheme.Default)
	if err != nil {
		cs = &colorscheme.Colorscheme{Name: "empty", Groups: make(map[string]colorscheme.Highlight)}
	}

	return api.Provider(api.Props{Client: props.Client},
		highlight.Provider(highlight.Props{Theme: cs},
			kitex.Text("Hello, Tasksmith!"),
		),
	)
})
