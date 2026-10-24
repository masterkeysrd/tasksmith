package components

import (
	"fmt"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
)

func renderTranscriptItem(item store.TranscriptItem, _ int) trail.Node {
	switch item.Kind {
	case store.TranscriptKindHeader:
		if d, ok := item.Data.(store.HeaderData); ok {
			return Header(d)
		}
	case store.TranscriptKindTimeline,
		store.TranscriptKindNotice,
		store.TranscriptKindError,
		store.TranscriptKindCommand:
		// Placeholder until respective components are added.
		return nil
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
