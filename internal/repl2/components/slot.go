package components

import (
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
)

// SlotComponent is a functional slot node generator parameterized by slotID.
type SlotComponent func(slotID string) trail.Node

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
		switch v := active.Data.(type) {
		case PickerProps:
			origSelect := v.OnSelect
			v.OnSelect = func(opt uicomponents.Option) {
				store.RemoveSlot(active.ID)
				if origSelect != nil {
					origSelect(opt)
				}
			}
			origCancel := v.OnCancel
			v.OnCancel = func() {
				store.RemoveSlot(active.ID)
				if origCancel != nil {
					origCancel()
				}
			}
			child = Picker(v)
		case SlotComponent:
			child = v(active.ID)
		case func(slotID string) trail.Node:
			child = v(active.ID)
		case trail.Node:
			child = v
		}
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
