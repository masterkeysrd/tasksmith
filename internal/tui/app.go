package tui

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/colorscheme"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/highlight"
)

type AppProps struct {
	Client api.Client
}

var (
	HLSurface = highlight.Set("TasksmithSurface", highlight.Link("Normal"))
)

var (
	SurfaceStyle = style.S().
		Width(style.Percent(100)).
		Height(style.Percent(100))
)

var App = kitex.FC("App", func(props AppProps) kitex.Node {
	cs, err := colorscheme.Find(colorscheme.Default)
	if err != nil {
		cs = &colorscheme.Colorscheme{Name: "empty", Groups: make(map[string]colorscheme.Highlight)}
	}

	return api.Provider(api.Props{Client: props.Client},
		highlight.Provider(highlight.Props{Theme: cs},
			components.Paper(components.PaperProps{
				Group: HLSurface,
				Style: SurfaceStyle,
			}, kitex.Text("Hello, Tasksmith!")),
		),
	)
})
