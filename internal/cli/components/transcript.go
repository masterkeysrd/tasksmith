package components

import (
	"fmt"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
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
	case store.TranscriptKindContextUsage:
		if d, ok := item.Data.(store.ContextUsageData); ok {
			return ContextUsage(d)
		}
	default:
		return nil
	}
	return nil
}

// Transcript is the host component that prints the header and finalized items.
// In draft mode (!Frozen), Header and draft command outputs remain live children.
// When Frozen, the snapshot Header and items are frozen into trail.Static and flushed into scrollback.
var Transcript = trail.SimpleFC("Transcript", func() trail.Node {
	app := trail.UseApp()
	liveHeader := store.UseHeaderData()
	epoch := store.UseTranscript(func(s store.TranscriptState) int {
		return s.Epoch
	})
	frozen := store.UseTranscript(func(s store.TranscriptState) bool {
		return s.Frozen
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

	var children []trail.Node
	if epoch == clearedEpoch() {
		var items []store.TranscriptItem
		if itemsPtr != nil {
			items = *itemsPtr
		}

		if !frozen {
			children = make([]trail.Node, 0, len(items)+1)
			children = append(children, Header(liveHeader))
			for i, item := range items {
				if n := renderTranscriptItem(item, i); n != nil {
					children = append(children, n)
				}
			}
		} else {
			if len(items) > 0 {
				children = []trail.Node{trail.Static(items, renderTranscriptItem)}
			}
		}
	}

	return trail.Box(trail.BoxProps{
		Key: fmt.Sprintf("transcript-%d", clearedEpoch()),
		Style: style.S().
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)),
	}, children...)
})
