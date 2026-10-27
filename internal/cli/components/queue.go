package components

import (
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// Queue renders the list of queued user messages and commands pending execution.
var Queue = trail.SimpleFC("Queue", func() trail.Node {
	queuePtr := store.UseStream(func(s store.StreamState) *[]api.QueueItem {
		return s.Queue
	})
	if queuePtr == nil || len(*queuePtr) == 0 {
		return nil
	}
	items := *queuePtr

	scheme := theme.UseTheme()
	queuedColor := scheme.Color.Transcript.QueuedText

	rows := make([]trail.Node, 0, len(items))
	for _, item := range items {
		prefix := "> "
		if item.Kind == api.QueueKindCommand {
			prefix = "! "
		}
		txt := queueItemText(item)

		rowStyle := style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Width(style.Percent(100)).
			Foreground(queuedColor)

		prefixStyle := style.S().
			Bold(true).
			WhiteSpace(style.WhiteSpaceNoWrap).
			Flex(0, 0).
			Foreground(queuedColor)

		textStyle := style.S().
			Foreground(queuedColor)

		rows = append(rows, trail.Box(trail.BoxProps{
			Style: rowStyle,
			Key:   item.ID,
		},
			trail.Span(trail.SpanProps{Style: prefixStyle}, trail.Text(prefix)),
			trail.Span(trail.SpanProps{Style: textStyle}, trail.Text(txt)),
		))
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)).
			MarginBottom(1),
	}, rows...)
})

func queueItemText(item api.QueueItem) string {
	switch p := item.Payload.(type) {
	case api.QueueMessagePayload:
		var sb strings.Builder
		for _, b := range p.Message.Content {
			if b.Text != "" {
				sb.WriteString(b.Text)
			}
		}
		return sb.String()
	case *api.QueueMessagePayload:
		if p == nil {
			return ""
		}
		var sb strings.Builder
		for _, b := range p.Message.Content {
			if b.Text != "" {
				sb.WriteString(b.Text)
			}
		}
		return sb.String()
	case api.QueueCommandPayload:
		return p.Command
	case *api.QueueCommandPayload:
		if p == nil {
			return ""
		}
		return p.Command
	default:
		return ""
	}
}
