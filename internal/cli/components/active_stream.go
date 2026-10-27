package components

import (
	"image/color"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
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
	status := store.UseStream(func(s store.StreamState) api.SessionStatus {
		return s.Status
	})
	pendingCmd := store.UseStream(func(s store.StreamState) string {
		return s.PendingCommand
	})

	var msgs []store.Message
	if msgsPtr != nil {
		msgs = *msgsPtr
	}

	if !streaming && len(msgs) == 0 && pendingCmd == "" {
		return nil
	}

	var children []trail.Node
	if streaming && !waitingPerm {
		children = append(children, StreamingHint())
	}

	for _, msg := range msgs {
		children = append(children, Message(MessageProps{Message: msg}))
	}

	if pendingCmd != "" {
		scheme := theme.UseTheme()
		var magentaColor, primaryColor color.Color
		if scheme != nil {
			magentaColor = scheme.Color.Text.Magenta
			if magentaColor == nil {
				magentaColor = scheme.Color.Text.Primary
			}
			primaryColor = scheme.Color.Text.Primary
		}
		children = append(children, trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				Width(style.Percent(100)).
				MarginBottom(1),
		},
			trail.Span(trail.SpanProps{
				Style: style.S().Bold(true).Foreground(magentaColor).WhiteSpace(style.WhiteSpaceNoWrap).Flex(0, 0),
			}, trail.Text("! ")),
			trail.Span(trail.SpanProps{
				Style: style.S().Foreground(primaryColor),
			}, trail.Text(pendingCmd)),
		))
	}

	if streaming && !waitingPerm {
		children = append(children, trail.Box(trail.BoxProps{
			Style: style.S().MarginBottom(1),
		}, Spinner(SpinnerProps{Status: status})))
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)).
			MinWidth(style.Cells(0)),
	}, children...)
})
