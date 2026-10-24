package components

import (
	"fmt"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
)

func renderTranscriptItem(item store.TranscriptItem, _ int) trail.Node {
	switch item.Kind {
	case store.TranscriptKindHeader:
		if d, ok := item.Data.(store.HeaderData); ok {
			return Header(d)
		}
	case store.TranscriptKindMessage:
		if m, ok := item.Data.(store.Message); ok {
			return Message(MessageProps{Message: m})
		}
	case store.TranscriptKindTimeline:
		if e, ok := item.Data.(api.TimelineEntry); ok {
			return TimelineEvent(e)
		}
	case store.TranscriptKindNotice:
		if d, ok := item.Data.(store.NoticeData); ok {
			return Notice(d)
		}
	case store.TranscriptKindError:
		if d, ok := item.Data.(store.ErrorData); ok {
			return ErrorBanner(d)
		}
	case store.TranscriptKindCommand:
		if d, ok := item.Data.(store.CommandData); ok {
			return CommandOutput(d)
		}
	default:
		return nil
	}
	return nil
}

// Transcript is the host component that permanently prints finalized items into
// terminal scrollback via trail.Static. Remounting (e.g. on new epoch) resets
// the static scrollback region.
var Transcript = trail.SimpleFC("Transcript", func() trail.Node {
	epoch := store.UseTranscript(func(s store.TranscriptState) int {
		return s.Epoch
	})
	itemsPtr := store.UseTranscript(func(s store.TranscriptState) *[]store.TranscriptItem {
		return s.Items
	})

	var items []store.TranscriptItem
	if itemsPtr != nil {
		items = *itemsPtr
	}

	return trail.Box(trail.BoxProps{
		Key: fmt.Sprintf("transcript-%d", epoch),
		Style: style.S().
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)),
	}, trail.Static(items, renderTranscriptItem))
})
