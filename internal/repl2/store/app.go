package store

import "github.com/masterkeysrd/kite/extras/kites"

// AppState holds global application UI state for the REPL.
type AppState struct {
	CtrlCPending bool
}

var app = kites.Create(AppState{})

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

// ResetApp resets AppState to its zero value.
func ResetApp() {
	app.Set(func(AppState) AppState {
		return AppState{}
	})
}
