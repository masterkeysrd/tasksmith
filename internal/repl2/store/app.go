package store

import (
	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/kite/extras/trail"
)

const (
	// DefaultHint is the standard shortcut hint shown on the status line.
	DefaultHint = "shift+tab cycle mode • /help commands"
	// CtrlCHint is the exit confirmation hint shown when Ctrl+C is first pressed.
	CtrlCHint = "Press Ctrl+C again to exit"
)

// AppState holds global application UI state for the REPL.
type AppState struct {
	CtrlCPending  bool
	ExitRequested bool
	Hint          string
}

var app = kites.Create(AppState{Hint: DefaultHint})

// UseApp subscribes a component to a comparable slice of AppState.
func UseApp[U comparable](selector func(AppState) U) U {
	return kites.Use(app, selector)
}

// GetApp returns the current AppState snapshot.
func GetApp() AppState {
	return app.Get()
}

// SetCtrlCPending updates the Ctrl+C exit confirmation state.
func SetCtrlCPending(pending bool) {
	app.Set(func(s AppState) AppState {
		s.CtrlCPending = pending
		return s
	})
}

// RequestExit requests the REPL application to terminate.
func RequestExit() {
	app.Set(func(s AppState) AppState {
		s.ExitRequested = true
		return s
	})
}

// SetHint updates the status line hint.
func SetHint(hint string) {
	app.Set(func(s AppState) AppState {
		s.Hint = hint
		return s
	})
}

// UseHint sets the status line hint while the calling component is mounted
// and restores the previous hint when the component unmounts.
func UseHint(hint string) {
	trail.UseLayoutEffectCleanup(func() func() {
		prev := GetApp().Hint
		SetHint(hint)
		return func() {
			if GetApp().Hint == hint {
				SetHint(prev)
			}
		}
	}, []any{hint})
}

// ResetApp resets AppState to its default values.
func ResetApp() {
	app.Set(func(AppState) AppState {
		return AppState{Hint: DefaultHint}
	})
}
