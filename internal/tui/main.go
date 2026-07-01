package tui

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/masterkeysrd/kite/backend/uv"
	"github.com/masterkeysrd/kite/devtools"
	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/engine"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/kitex/kitexdt"
	kitelog "github.com/masterkeysrd/kite/log"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/keymap"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
)

type RunOptions struct {
	Client       api.Client
	DevToolsAddr string
}

var (
	RootStyle = style.S().
		Display(style.DisplayFlex).
		Width(style.Percent(100)).
		Height(style.Percent(100))
)

func Run(ctx context.Context, opts RunOptions) error {
	b, err := uv.New()
	if err != nil {
		return fmt.Errorf("failed to create backend: %w", err)
	}

	// Wire our slog logger into Kite so its internal warnings (e.g. keyless node
	// shifting) are visible in the tasksmith log output.
	kitelog.SetLogger(slog.Default())

	eng := engine.New(b, engine.Options{})

	if opts.DevToolsAddr != "" {
		kitex.EnableDevMode = true
		if insp, err := devtools.Install(eng, devtools.Options{
			ServerAddr: opts.DevToolsAddr,
		}); err != nil {
			log.Warn("Failed to install devtools", log.Err(err))
		} else {
			kitexdt.Register(insp)
		}
	}

	go func() {
		<-ctx.Done()
		eng.Stop()
	}()

	keymap.SetDocument(eng.Document(), func() mode.Mode {
		return mode.Get()
	})

	main := element.NewBox(eng.Document())
	main.Style(RootStyle)
	eng.Mount(main)

	kitex.Render(App(AppProps{Client: opts.Client}), main)

	return eng.Run(ctx)
}
