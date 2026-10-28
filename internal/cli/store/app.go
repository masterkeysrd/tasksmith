package store

import (
	"slices"

	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
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
	Version       string
	Workspace     string
	Warnings      *[]string
	Commands      *[]api.Command
	Skills        *[]api.Skill
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

// SetHeaderMeta updates the version and workspace metadata in AppState.
func SetHeaderMeta(version, workspace string) {
	app.Set(func(s AppState) AppState {
		s.Version = version
		s.Workspace = workspace
		return s
	})
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

// SetWorkspaceWarnings updates the workspace warnings in AppState.
func SetWorkspaceWarnings(warnings []string) {
	app.Set(func(s AppState) AppState {
		if len(warnings) == 0 {
			if s.Warnings == nil {
				return s
			}
			s.Warnings = nil
			return s
		}
		if s.Warnings != nil && slices.Equal(*s.Warnings, warnings) {
			return s
		}
		w := make([]string, len(warnings))
		copy(w, warnings)
		s.Warnings = &w
		return s
	})
}

// SetWorkspaceCommands updates the workspace commands in AppState.
func SetWorkspaceCommands(commands []api.Command) {
	app.Set(func(s AppState) AppState {
		if len(commands) == 0 {
			if s.Commands == nil {
				return s
			}
			s.Commands = nil
			return s
		}
		c := slices.Clone(commands)
		s.Commands = &c
		return s
	})
}

// SetWorkspaceSkills updates the workspace skills in AppState.
func SetWorkspaceSkills(skills []api.Skill) {
	app.Set(func(s AppState) AppState {
		if len(skills) == 0 {
			if s.Skills == nil {
				return s
			}
			s.Skills = nil
			return s
		}
		sk := slices.Clone(skills)
		s.Skills = &sk
		return s
	})
}

// ResetApp resets AppState to its default values while preserving version, workspace, and warnings.
func ResetApp() {
	app.Set(func(s AppState) AppState {
		return AppState{
			Hint:      DefaultHint,
			Version:   s.Version,
			Workspace: s.Workspace,
			Warnings:  s.Warnings,
		}
	})
}
