// Package active provides reactive stores for managing the active workspace view state (e.g. session ID).
package active

import (
	"time"

	"github.com/masterkeysrd/kite/extras/kites"
)

type state struct {
	sessionID     string
	screen        string // "chat" or "analytics"
	modal         string // active modal overlay identifier, "" if none
	isSidebarOpen bool
	statusMessage string
}

var store = kites.Create(state{
	sessionID:     "",
	screen:        "chat",
	modal:         "",
	isSidebarOpen: true,
	statusMessage: "",
})

// SetSessionID updates the active session ID and switches the screen back to chat, closing any modal.
func SetSessionID(id string) {
	store.Set(func(s state) state {
		s.sessionID = id
		s.screen = "chat"
		s.modal = ""
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

// SetModal updates the active modal overlay. Set to "" to close modals.
func SetModal(modal string) {
	store.Set(func(s state) state {
		s.modal = modal
		return s
	})
}

// GetModal returns the active modal.
func GetModal() string {
	return store.Get().modal
}

// UseModal returns the active modal reactively.
func UseModal() string {
	return kites.Use(store, func(s state) string {
		return s.modal
	})
}

// GetSidebarOpen returns whether the sidebar is open.
func GetSidebarOpen() bool {
	return store.Get().isSidebarOpen
}

// SetSidebarOpen updates whether the sidebar is open.
func SetSidebarOpen(open bool) {
	store.Set(func(s state) state {
		s.isSidebarOpen = open
		return s
	})
}

// UseSidebarOpen returns whether the sidebar is open reactively.
func UseSidebarOpen() bool {
	return kites.Use(store, func(s state) bool {
		return s.isSidebarOpen
	})
}

// InvalidateSessionState is a callback registered by the App to invalidate session state queries.
var InvalidateSessionState func(sessionID string)

// InvalidateFileChanges is a callback registered by the App to invalidate file changes queries.
var InvalidateFileChanges func(sessionID string)

// SetStatusMessage sets a transient feedback message in the status bar/command bar.
func SetStatusMessage(msg string) {
	store.Set(func(s state) state {
		s.statusMessage = msg
		return s
	})
	// Auto-clear after 4 seconds
	go func() {
		time.Sleep(4 * time.Second)
		store.Set(func(s state) state {
			if s.statusMessage == msg {
				s.statusMessage = ""
			}
			return s
		})
	}()
}

// UseStatusMessage returns the active transient status message reactively.
func UseStatusMessage() string {
	return kites.Use(store, func(s state) string {
		return s.statusMessage
	})
}
