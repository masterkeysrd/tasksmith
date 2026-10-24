package repl2

import (
	"time"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/repl2/components"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
)

// CtrlCTimeout is the duration before the Ctrl+C confirmation resets.
const CtrlCTimeout = 2 * time.Second

func isCtrlC(k event.KeyEvent) bool {
	return k.Code == key.CtrlC || k.MatchString("ctrl+c")
}

// App is the root REPL UI component.
var App = trail.SimpleFC("App", func() trail.Node {
	app := trail.UseApp()
	ctrlCPending := store.UseApp(func(s store.AppState) bool { return s.CtrlCPending })

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
		components.Slot(),
		components.DefaultStatusLine(),
	)
})
