// Package active provides reactive stores for managing the active workspace view state (e.g. session ID).
package active

import (
	"github.com/masterkeysrd/kite/extras/kites"
)

type state struct {
	sessionID string
	screen    string // "chat" or "analytics"
}

var store = kites.Create(state{
	sessionID: "",
	screen:    "chat",
})

// SetSessionID updates the active session ID and switches the screen back to chat.
func SetSessionID(id string) {
	store.Set(func(s state) state {
		s.sessionID = id
		s.screen = "chat"
		return s
	})
}

// SetScreen updates the active screen.
func SetScreen(scr string) {
	store.Set(func(s state) state {
		s.screen = scr
		return s
	})
}

// GetScreen returns the active screen.
func GetScreen() string {
	return store.Get().screen
}

// UseScreen returns the active screen reactively.
func UseScreen() string {
	return kites.Use(store, func(s state) string {
		return s.screen
	})
}

// GetSessionID returns the active session ID.
func GetSessionID() string {
	return store.Get().sessionID
}

// UseSessionID returns the active session ID reactively.
// It is a Kite hook and must be called within a component's render function.
func UseSessionID() string {
	return kites.Use(store, func(s state) string {
		return s.sessionID
	})
}
