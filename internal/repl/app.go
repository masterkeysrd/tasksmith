package repl

import (
	"strings"
	"time"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
	"github.com/masterkeysrd/tasksmith/internal/repl/components"
	"github.com/masterkeysrd/tasksmith/internal/repl/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
)

// CtrlCTimeout is the duration before the Ctrl+C confirmation resets.
const CtrlCTimeout = 2 * time.Second

func isCtrlC(k event.KeyEvent) bool {
	return k.Code == key.CtrlC || k.MatchString("ctrl+c")
}

var appContent = trail.SimpleFC("AppContent", func() trail.Node {
	app := trail.UseApp()
	ctrlCPending := store.UseApp(func(s store.AppState) bool { return s.CtrlCPending })
	exitRequested := store.UseApp(func(s store.AppState) bool { return s.ExitRequested })

	kitex.UseLayoutEffect(func() {
		if exitRequested && app != nil {
			app.Exit(nil)
		}
	}, []any{exitRequested})

	mode := store.UseSession(func(s store.SessionState) api.PermissionMode {
		return s.Config.Mode
	})

	registry := kitex.UseMemo(func() *command.Registry {
		return command.NewRegistry()
	}, nil)

	engine := kitex.UseMemo(func() *autocomplete.Engine {
		e := autocomplete.New()
		for _, s := range registry.Sources() {
			_ = e.Register(s)
		}
		return e
	}, []any{registry})

	views := kitex.UseMemo(func() map[string]uicomponents.AutocompleteView {
		return command.CompletionViews(mode)
	}, []any{mode})

	trail.UseInput(func(_ string, k event.KeyEvent) {
		if isCtrlC(k) {
			if store.GetApp().CtrlCPending {
				app.Exit(nil)
			} else {
				store.SetCtrlCPending(true)
			}
		} else if store.GetApp().CtrlCPending {
			store.SetCtrlCPending(false)
		}
	})

	trail.UseTimeout(func() {
		if store.GetApp().CtrlCPending {
			store.SetCtrlCPending(false)
		}
	}, CtrlCTimeout, []any{ctrlCPending})

	return trail.Box(trail.BoxProps{
		Style: style.S().
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)),
	},
		components.Transcript(),
		components.ActiveStream(),
		components.Composer(components.ComposerProps{
			Autocomplete:    engine,
			CompletionViews: views,
			OnSubmit: func(text string) {
				if strings.HasPrefix(text, "/") {
					if err := registry.Execute(text); err != nil {
						store.AppendError("Error", err.Error())
					}
				} else if store.GetStream().Streaming {
					store.AppendNotice("A response is already running.")
				} else {
					store.Send(text)
				}
			},
			OnCancel:    store.Cancel,
			OnCycleMode: func() { _ = store.CycleMode() },
		}),
		components.Slot(),
		components.Tasks(),
		components.DefaultStatusLine(),
	)
})

// App is the root REPL UI component, wrapped in a wind.Provider.
var App = trail.SimpleFC("App", func() trail.Node {
	queryClient := kitex.UseMemo(func() *wind.Client {
		return wind.NewClient()
	}, nil)

	return wind.Provider(wind.ProviderProps{Client: queryClient}, appContent())
})
