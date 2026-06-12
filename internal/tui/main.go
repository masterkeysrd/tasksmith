package tui

import (
	"context"
	"fmt"

	"github.com/masterkeysrd/kite/backend/uv"
	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/engine"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/keymap"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
)

func Run(ctx context.Context, client api.Client) error {
	b, err := uv.New()
	if err != nil {
		return fmt.Errorf("failed to create backend: %w", err)
	}

	eng := engine.New(b, engine.Options{})

	go func() {
		<-ctx.Done()
		eng.Stop()
	}()

	keymap.SetDocument(eng.Document(), func() mode.Mode {
		return mode.Get()
	})

	main := element.NewBox(eng.Document())
	main.Style(style.S().
		Display(style.DisplayFlex).
		Width(style.Percent(100)).
		Height(style.Percent(100)))
	eng.Mount(main)

	kitex.Render(App(AppProps{Client: client}), main)

	return eng.Run(ctx)
}
