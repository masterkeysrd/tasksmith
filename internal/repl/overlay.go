package repl

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
	"github.com/masterkeysrd/tasksmith/internal/repl/components"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	uihooks "github.com/masterkeysrd/tasksmith/internal/ui/hooks"
)

type overlayKind int

const (
	overlayNone overlayKind = iota
	// overlayPicker shows a picker supplied by a command.
	overlayPicker
	// overlaySessions shows the workspace session browser.
	overlaySessions
)

// OverlayControl reads and changes the modal shown above the prompt.
type OverlayControl struct {
	Kind overlayKind
	// Picker is the picker to show when Kind is overlayPicker.
	Picker ui.PickerProps
	// Hint overrides the status line hint while it is not empty.
	Hint string

	IsOpen       bool
	OpenPicker   func(ui.PickerProps)
	OpenSessions func()
	Close        func()
	SetHint      func(string)
}

type overlayState struct {
	Kind   overlayKind
	Picker ui.PickerProps
	Hint   string
}

var overlayContext = kitex.CreateContext[*OverlayControl](nil)

// UseOverlay returns the active modal overlay.
func UseOverlay() OverlayControl {
	control := kitex.UseContext(overlayContext)
	if control == nil {
		panic("repl: UseOverlay must be used within an OverlayProvider")
	}
	return *control
}

// OverlayProviderProps configures [OverlayProvider].
type OverlayProviderProps struct {
	Children []trail.Node // injected by trail.FCC
}

// OverlayProvider tracks which modal, if any, is open.
var OverlayProvider = trail.FCC("OverlayProvider", func(props OverlayProviderProps) trail.Node {
	state, setState := kitex.UseState(overlayState{})
	control := &OverlayControl{
		Kind:   state().Kind,
		Picker: state().Picker,
		Hint:   state().Hint,
		IsOpen: state().Kind != overlayNone,
		OpenPicker: func(picker ui.PickerProps) {
			setState(overlayState{Kind: overlayPicker, Picker: picker, Hint: state().Hint})
		},
		OpenSessions: func() {
			setState(overlayState{Kind: overlaySessions, Hint: state().Hint})
		},
		Close: func() {
			setState(overlayState{Hint: state().Hint})
		},
		SetHint: func(hint string) {
			current := state()
			current.Hint = hint
			setState(current)
		},
	}
	return overlayContext.Provider(control, props.Children...)
})

// bindPicker closes the overlay when the picker is selected or cancelled,
// before running the picker's own handlers.
func bindPicker(overlay OverlayControl, picker ui.PickerProps) ui.PickerProps {
	picker.OnOpen = func() { overlay.SetHint("") }
	onSelect := picker.OnSelect
	picker.OnSelect = func(option uicomponents.Option) {
		overlay.Close()
		if onSelect != nil {
			onSelect(option)
		}
	}
	onCancel := picker.OnCancel
	picker.OnCancel = func() {
		overlay.Close()
		if onCancel != nil {
			onCancel()
		}
	}
	return picker
}

// Overlay renders the open modal: a command picker, the session browser, or
// the permission prompt when the agent is waiting for a decision.
var Overlay = trail.SimpleFC("Overlay", func() trail.Node {
	overlay := UseOverlay()
	permissions := UsePermissionQueue()

	switch overlay.Kind {
	case overlayPicker:
		return ui.Picker(bindPicker(overlay, overlay.Picker))
	case overlaySessions:
		return SessionBrowser()
	}
	if request, ok := permissions.Active(); ok {
		return ui.Approval(ui.ApprovalProps{Request: request, OnDecision: permissions.Decide})
	}
	return nil
})

type sessionPickerState struct {
	Props    ui.PickerProps
	Sessions wind.Result[[]api.Session]
}

// useSessionPicker builds the session browser's picker from the workspace's
// sessions and the session actions.
func useSessionPicker() sessionPickerState {
	overlay := UseOverlay()
	session := uihooks.UseSession()
	actions := UseSessionActions()
	transcript := UseTranscript()
	sessions := uihooks.UseSessions(wind.Options{Enabled: true})

	activeID := ""
	if !session.Draft {
		activeID = session.ID
	}
	picker := bindPicker(overlay, command.NewSessionPicker(command.Env{
		AppendOutput: func(output ui.CommandOutputProps) {
			transcript.Append(components.CommandEntry(output))
		},
		SwitchSession: actions.Switch,
		RenameSession: actions.Rename,
		DeleteSession: actions.Delete,
	}, sessions.Data, activeID))

	switch {
	case sessions.IsLoading || sessions.IsFetching:
		picker.Footer = "Loading sessions..."
	case actions.IsRenaming:
		picker.Footer = "Renaming session..."
	case actions.IsDeleting:
		picker.Footer = "Deleting session..."
	}
	return sessionPickerState{Props: picker, Sessions: sessions}
}

// SessionBrowser lists the workspace's sessions. It closes itself, reporting
// why, when there is nothing to browse.
var SessionBrowser = trail.SimpleFC("SessionBrowser", func() trail.Node {
	overlay := UseOverlay()
	transcript := UseTranscript()
	browser := useSessionPicker()
	sessions := browser.Sessions

	kitex.UseEffect(func() {
		if sessions.IsLoading || sessions.IsFetching {
			return
		}
		if sessions.IsError {
			transcript.Append(components.ErrorEntry(ui.ErrorMessageProps{Text: sessions.Error.Error()}))
		} else if len(sessions.Data) == 0 {
			transcript.Append(components.CommandEntry(ui.CommandOutputProps{
				Command: "/sessions",
				Output:  "No sessions found.",
			}))
		} else {
			return
		}
		overlay.Close()
	}, []any{sessions.IsLoading, sessions.IsFetching, sessions.IsError, sessions.Error, sessions.Data})

	return ui.Picker(browser.Props)
})
