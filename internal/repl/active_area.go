package repl

import (
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
	"github.com/masterkeysrd/tasksmith/internal/repl/components"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
	uihooks "github.com/masterkeysrd/tasksmith/internal/ui/hooks"
)

// UseReplState derives the interactive phase of the REPL from what is open and
// running.
func UseReplState() ui.State {
	overlay := UseOverlay()
	permissions := UsePermissionQueue()
	streaming := uihooks.UseIsStreaming()
	switch {
	case overlay.IsOpen:
		return ui.StatePicker
	case permissions.Count() > 0:
		return ui.StateApproval
	case streaming:
		return ui.StateStreaming
	default:
		return ui.StateIdle
	}
}

// StatusScopeProps configures [StatusScope].
type StatusScopeProps struct {
	Children []trail.Node // injected by trail.FCC
}

// StatusScope shares the REPL phase and hint override with the status line
// segments. The session's mode and model are read by the segments themselves.
var StatusScope = trail.FCC("StatusScope", func(props StatusScopeProps) trail.Node {
	state := UseReplState()
	overlay := UseOverlay()
	return components.StatusProvider(components.StatusContext{State: state, Hint: overlay.Hint}, props.Children...)
})

// ActiveArea is the live part of the screen below the transcript: the message
// being streamed, the modal or prompt, and the status line.
var ActiveArea = trail.SimpleFC("ActiveArea", func() trail.Node {
	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)),
	},
		StreamMessages(),
		ConversationError(),
		Thinking(),
		Overlay(),
		Prompt(),
		components.DefaultStatusLine(),
	)
})

// StreamMessages renders the messages that are still being produced.
var StreamMessages = trail.SimpleFC("StreamMessages", func() trail.Node {
	conversation := uihooks.UseCurrentConversation()
	nodes := make([]trail.Node, 0, len(conversation.Stream.Messages))
	for _, message := range conversation.Stream.Messages {
		nodes = append(nodes, trail.Box(trail.BoxProps{
			Key: message.ID,
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Width(style.Percent(100)).
				MinWidth(style.Cells(0)),
		}, components.Message(components.MessageProps{Message: message})))
	}
	return trail.Fragment(nodes...)
})

// ConversationError shows why the conversation failed, if it did.
var ConversationError = trail.SimpleFC("ConversationError", func() trail.Node {
	conversation := uihooks.UseCurrentConversation()
	if conversation.Error == nil {
		return nil
	}
	return ui.ErrorMessage(ui.ErrorMessageProps{Text: conversation.Error.Error()})
})

// Thinking shows a spinner while a response is being produced.
var Thinking = trail.SimpleFC("Thinking", func() trail.Node {
	if !uihooks.UseIsStreaming() {
		return nil
	}
	return ui.Spinner(ui.SpinnerProps{Text: "Thinking..."})
})

// Prompt is the composer, hidden while a modal needs the user's attention.
var Prompt = trail.SimpleFC("Prompt", func() trail.Node {
	state := UseReplState()
	commands := UseCommands()
	submit := UseSubmit()
	mode := uihooks.UsePermissionMode()
	send := UseSend()

	if state == ui.StatePicker || state == ui.StateApproval {
		return nil
	}
	var nodes []trail.Node
	if commands.Err != nil {
		nodes = append(nodes, ui.ErrorMessage(ui.ErrorMessageProps{Text: commands.Err.Error()}))
	}
	nodes = append(nodes, components.Composer(components.ComposerProps{
		Autocomplete:    commands.Engine,
		CompletionViews: command.CompletionViews(mode.Mode),
		OnSubmit:        submit,
		OnCycleMode:     mode.Cycle,
		OnCancel:        send.Cancel,
	}))
	return trail.Fragment(nodes...)
})
