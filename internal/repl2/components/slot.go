package components

import (
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
)

// Slot is the host component that renders the currently active slot item.
var Slot = trail.SimpleFC("Slot", func() trail.Node {
	active := store.UseActiveSlot()
	if active == nil {
		return nil
	}

	var child trail.Node
	switch active.Kind {
	case store.SlotKindPermission:
		// Placeholder until Permission component is added.
		child = nil
	case store.SlotKindQuestion:
		// Placeholder until Question component is added.
		child = nil
	case store.SlotKindPicker:
		// Placeholder until Picker component is added.
		child = nil
	case store.SlotKindAutocomplete:
		// Placeholder until Autocomplete component is added.
		child = nil
	default:
		return nil
	}

	if child == nil {
		return nil
	}

	return trail.Box(trail.BoxProps{
		Key: active.ID,
		Style: style.S().
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)),
	}, child)
})
