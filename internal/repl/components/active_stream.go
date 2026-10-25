package components

import (
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/repl/store"
)

// StreamingHint registers the streaming cancellation hint in the store while mounted.
var StreamingHint = trail.SimpleFC("StreamingHint", func() trail.Node {
	store.UseHint("streaming • esc to cancel")
	return nil
})

// ActiveStream renders the in-flight conversation messages and animated spinner
// for the current active LLM turn.
var ActiveStream = trail.SimpleFC("ActiveStream", func() trail.Node {
	msgsPtr := store.UseStream(func(s store.StreamState) *[]store.Message {
		return s.Messages
	})
	streaming := store.UseStream(func(s store.StreamState) bool {
		return s.Streaming
	})
	waitingPerm := store.UseStream(func(s store.StreamState) bool {
		return s.WaitingPermission
	})

	var msgs []store.Message
	if msgsPtr != nil {
		msgs = *msgsPtr
	}

	if !streaming && len(msgs) == 0 {
		return nil
	}

	var children []trail.Node
	if streaming && !waitingPerm {
		children = append(children, StreamingHint())
	}

	for _, msg := range msgs {
		children = append(children, Message(MessageProps{Message: msg}))
	}

	if streaming && !waitingPerm {
		children = append(children, trail.Box(trail.BoxProps{
			Style: style.S().MarginBottom(1),
		}, Spinner(SpinnerProps{Text: "Thinking..."})))
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)).
			MinWidth(style.Cells(0)),
	}, children...)
})
