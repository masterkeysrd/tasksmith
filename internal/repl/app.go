package repl

import (
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
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
	app := trail.UseApp()
	ctrl := useREPLController(props, app)

	// Computed declarative UI state (orthogonal to modal / streaming)
	state := ctrl.State()

	var activeNodes []trail.Node

	// Streaming display
	if state == ui.StateStreaming || ctrl.Stream.IsStreaming {
		if r := strings.TrimSpace(ctrl.Stream.StreamReasoning); r != "" {
			activeNodes = append(activeNodes, ui.Reasoning(ui.ReasoningProps{
				Text:     r,
				Duration: ctrl.Stream.StreamReasoningDuration,
				Running:  !ctrl.Stream.StreamReasoningDone,
			}))
		}
		var allActiveTools []api.ToolCall
		if len(ctrl.Stream.CompletedTools) > 0 {
			allActiveTools = append(allActiveTools, ctrl.Stream.CompletedTools...)
		}
		if len(ctrl.Stream.PendingTools) > 0 {
			allActiveTools = append(allActiveTools, ctrl.Stream.PendingTools...)
		}
		if len(allActiveTools) > 0 {
			groups := ui.GroupToolCalls(allActiveTools)
			for _, grp := range groups {
				grpRunning := false
				for _, c := range grp {
					for _, pt := range ctrl.Stream.PendingTools {
						if pt.ID == c.ID {
							grpRunning = true
							break
						}
					}
					if grpRunning {
						break
					}
				}
				activeNodes = append(activeNodes, ui.ToolGroup(ui.ToolGroupProps{
					Calls:   grp,
					Running: grpRunning,
				}))
			}
		}
		if t := strings.TrimSpace(ctrl.Stream.StreamText); t != "" {
			activeNodes = append(activeNodes, ui.Message(ui.MessageProps{
				Message: api.Message{
					Role: api.RoleAssistant,
					Content: []api.Block{
						{Type: api.BlockText, Text: t},
					},
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

	return uitheme.Provider(uitheme.Props{Transparent: true},
		trail.Fragment(
			ui.Transcript(ui.TranscriptProps{Items: ctrl.Items()}),
			trail.Box(trail.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					Width(style.Percent(100)),
			}, activeNodes...),
		),
	)
})
