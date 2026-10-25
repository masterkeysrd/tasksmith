package components

import (
	"fmt"

	"github.com/masterkeysrd/kite/extras/kitex"
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
// terminal scrollback via trail.Static. When Epoch changes (e.g. /clear or session reset),
// it coordinates app.Clear() before mounting the new epoch's items to guarantee
// the new items are printed after the terminal scrollback is cleared.
var Transcript = trail.SimpleFC("Transcript", func() trail.Node {
	app := trail.UseApp()
	epoch := store.UseTranscript(func(s store.TranscriptState) int {
		return s.Epoch
	})
	itemsPtr := store.UseTranscript(func(s store.TranscriptState) *[]store.TranscriptItem {
		return s.Items
	})

	clearedEpoch, setClearedEpoch := kitex.UseState(epoch)

	kitex.UseLayoutEffect(func() {
		if epoch != clearedEpoch() {
			if app != nil {
				app.Clear()
				if eng := app.Engine(); eng != nil {
					eng.Post(func() {
						setClearedEpoch(epoch)
					})
				} else {
					setClearedEpoch(epoch)
				}
			} else {
				setClearedEpoch(epoch)
			}
		}
	}, []any{epoch, clearedEpoch()})

	var items []store.TranscriptItem
	if epoch == clearedEpoch() && itemsPtr != nil {
		items = *itemsPtr
	}

	return trail.Box(trail.BoxProps{
		Key: fmt.Sprintf("transcript-%d", clearedEpoch()),
		Style: style.S().
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)),
	}, trail.Static(items, renderTranscriptItem))
})
