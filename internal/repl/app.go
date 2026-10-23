package repl

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
	"github.com/masterkeysrd/tasksmith/internal/repl/components"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
	uiclient "github.com/masterkeysrd/tasksmith/internal/ui/client"
	uitheme "github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// AppProps configures the root REPL trail functional component.
type AppProps struct {
	Client            *client.Client
	Workspace         string
	Version           string
	SessionID         string
	Agent             string
	Model             string
	Title             string
	InitialSess       *api.Session
	InitialMode       api.PermissionMode
	InitialModelLabel string
	InitialThinking   string
	Registry          *command.Registry
}

// App is the root trail functional component for the TaskSmith REPL.
var App = trail.FC("App", func(props AppProps) trail.Node {
	queryClient := kitex.UseMemo(wind.NewClient, []any{})
	return uiclient.Provider(uiclient.Props{Client: props.Client},
		wind.Provider(wind.ProviderProps{Client: queryClient}, replView(props)),
	)
})

var replView = trail.FC("REPL", func(props AppProps) trail.Node {
	props.Registry = kitex.UseMemo(func() *command.Registry {
		if props.Registry != nil {
			return props.Registry
		}
		return command.NewRegistry()
	}, []any{props.Registry})
	type completionSetup struct {
		engine *autocomplete.Engine
		err    error
	}
	completions := kitex.UseMemo(func() completionSetup {
		engine := autocomplete.New()
		for _, source := range props.Registry.Sources() {
			if err := engine.Register(source); err != nil {
				return completionSetup{err: err}
			}
		}
		return completionSetup{engine: engine}
	}, []any{props.Registry})
	app := trail.UseApp()
	ctrl := useREPLController(props, app)

	// Computed declarative UI state (orthogonal to modal / streaming)
	state := ctrl.State()

	var activeNodes []trail.Node

	for _, message := range ctrl.Conversation.Stream.Messages {
		activeNodes = append(activeNodes, trail.Box(trail.BoxProps{
			Key: message.ID,
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Width(style.Percent(100)).
				MinWidth(style.Cells(0)),
		}, components.Message(components.MessageProps{Message: message})))
	}
	if ctrl.Conversation.Error != nil {
		activeNodes = append(activeNodes, ui.ErrorMessage(ui.ErrorMessageProps{Text: ctrl.Conversation.Error.Error()}))
	}
	if ctrl.IsStreaming {
		activeNodes = append(activeNodes, ui.Spinner(ui.SpinnerProps{Text: "Thinking..."}))
	}

	// Active modal overlays
	switch state {
	case ui.StatePicker:
		activeNodes = append(activeNodes, ui.Picker(ctrl.ActivePicker()))
	case ui.StateApproval:
		if req, ok := ctrl.Perms.Active(); ok {
			activeNodes = append(activeNodes, ui.Approval(ui.ApprovalProps{
				Request:    req,
				OnDecision: ctrl.Perms.Decide,
			}))
		}
	}

	// Composer (visible in idle and streaming so user can draft next prompt)
	if state != ui.StatePicker && state != ui.StateApproval {
		if completions.err != nil {
			activeNodes = append(activeNodes, ui.ErrorMessage(ui.ErrorMessageProps{Text: completions.err.Error()}))
		}
		activeNodes = append(activeNodes, components.Composer(components.ComposerProps{
			Autocomplete:    completions.engine,
			CompletionViews: command.CompletionViews(ctrl.Mode()),
			OnSubmit:        ctrl.Submit, OnCycleMode: ctrl.CycleMode, OnCancel: ctrl.Cancel,
		}))
	}

	// Global status line (always visible at the bottom)
	activeNodes = append(activeNodes, components.StatusLine(components.StatusLineProps{
		State:    state,
		Mode:     ctrl.Mode(),
		Model:    ctrl.ModelLabel(),
		Thinking: ctrl.Thinking(),
		Hint:     ctrl.StatusHint(),
	}))

	return uitheme.Provider(uitheme.Props{Transparent: true},
		trail.Fragment(
			trail.Box(trail.BoxProps{Key: ctrl.Transcript.Key},
				components.Transcript(components.TranscriptProps{
					Entries: ctrl.Transcript.Entries,
				}),
			),
			trail.Box(trail.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					Width(style.Percent(100)),
			}, activeNodes...),
		),
	)
})
