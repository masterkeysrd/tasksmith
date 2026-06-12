// Package mode provides the mode enum and a reactive store for managing TUI input modes.
package mode

import (
	"github.com/masterkeysrd/kite/extras/kites"
)

// Mode represents a TUI input mode.
type Mode int

const (
	// Normal is the default mode for navigation and command entry.
	Normal Mode = iota
	// Insert is the mode for text insertion.
	Insert
	// Command is the mode for entering commands via a command bar.
	Command
)

// String returns a string representation of the mode.
func (m Mode) String() string {
	switch m {
	case Normal:
		return "NORMAL"
	case Insert:
		return "INSERT"
	case Command:
		return "COMMAND"
	default:
		return "UNKNOWN"
	}
}

type state struct {
	current Mode
}

// store is the global state store for the TUI mode.
var store = kites.Create(state{
	current: Normal,
})

// Set updates the current TUI mode.
func Set(m Mode) {
	store.Set(func(s state) state {
		s.current = m
		return s
	})
}

// Use returns the current mode.
// It is a Kite hook and must be called within a component's render function.
func Use() Mode {
	return kites.Use(store, func(s state) Mode {
		return s.current
	})
}
