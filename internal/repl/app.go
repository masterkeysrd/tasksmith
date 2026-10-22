package repl

import (
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
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
	app := trail.UseApp()
	ctrl := useREPLController(props, app)

	// Computed declarative UI state (orthogonal to modal / streaming)
	state := ctrl.State()

	var activeNodes []trail.Node

	// Streaming display
	if state == ui.StateStreaming || ctrl.Stream.IsStreaming {
		var content []api.Block
		if r := strings.TrimSpace(ctrl.Stream.StreamReasoning); r != "" {
			content = append(content, api.Block{
				Type: api.BlockReasoning,
				Text: r,
			})
		}
		if t := ctrl.Stream.StreamText; t != "" {
			content = append(content, api.Block{
				Type: api.BlockText,
				Text: t,
			})
		}
		if len(content) > 0 {
			activeNodes = append(activeNodes, ui.Message(ui.MessageProps{
				Message: api.Message{
					Role:    api.RoleAssistant,
					Content: content,
				},
			}))
		}
		activeNodes = append(activeNodes, ui.Spinner(ui.SpinnerProps{Text: ctrl.Stream.SpinnerText}))
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
		activeNodes = append(activeNodes, ui.Composer(ui.ComposerProps{
			Disabled:    false,
			Suggest:     ctrl.SuggestCommands,
			SuggestArgs: ctrl.SuggestArgs,
			OnSubmit:    ctrl.Submit,
			OnCycleMode: ctrl.CycleMode,
			OnCancel:    ctrl.Cancel,
		}))
	}

	// Global status bar (always visible at the bottom)
	activeNodes = append(activeNodes, ui.StatusBar(ui.StatusBarProps{
		State:    state,
		Mode:     ctrl.Mode(),
		Model:    ctrl.ModelLabel(),
		Thinking: ctrl.Thinking(),
		Hint:     ctrl.StatusHint(),
	}))

	return trail.Fragment(
		ui.Transcript(ui.TranscriptProps{Items: ctrl.Items()}),
		trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Width(style.Percent(100)),
		}, activeNodes...),
	)
})
