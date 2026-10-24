package store

import "github.com/masterkeysrd/kite/extras/kites"

// SlotKind identifies the kind of UI component rendered in the Slot.
type SlotKind string

const (
	SlotKindPermission   SlotKind = "permission"
	SlotKindQuestion     SlotKind = "question"
	SlotKindPicker       SlotKind = "picker"
	SlotKindAutocomplete SlotKind = "autocomplete"
)

// HidesComposer reports whether this slot kind should hide the Composer while active.
func (k SlotKind) HidesComposer() bool {
	switch k {
	case SlotKindPermission, SlotKindQuestion, SlotKindPicker:
		return true
	default:
		return false
	}
}

// SlotItem is a pure data descriptor enqueued into the Slot.
type SlotItem struct {
	ID   string
	Kind SlotKind
	Data any
}

// SlotState holds the FIFO queue of SlotItems and the currently active front item.
type SlotState struct {
	Active *SlotItem
	Queue  *[]SlotItem
}

var slot = kites.Create(SlotState{})

func normalizeSlotState(items []SlotItem) SlotState {
	if len(items) == 0 {
		return SlotState{
			Active: nil,
			Queue:  nil,
		}
	}
	q := make([]SlotItem, len(items))
	copy(q, items)
	return SlotState{
		Active: &q[0],
		Queue:  &q,
	}
}

// UseSlot subscribes a component to a comparable slice of SlotState.
func UseSlot[U comparable](selector func(SlotState) U) U {
	return kites.Use(slot, selector)
}

// GetSlot returns the current SlotState snapshot.
func GetSlot() SlotState {
	return slot.Get()
}

// UseActiveSlot returns the currently active SlotItem, or nil if none.
func UseActiveSlot() *SlotItem {
	return UseSlot(func(s SlotState) *SlotItem {
		return s.Active
	})
}

// UseHideComposer reports whether the currently active slot component hides the composer.
func UseHideComposer() bool {
	return UseSlot(func(s SlotState) bool {
		return s.Active != nil && s.Active.Kind.HidesComposer()
	})
}

// EnqueueSlot enqueues an item into the slot queue. If item.ID is non-empty and
// an item with the same ID already exists, it is updated in place to prevent duplicates.
func EnqueueSlot(item SlotItem) {
	slot.Set(func(s SlotState) SlotState {
		var current []SlotItem
		if s.Queue != nil {
			current = append(current, *s.Queue...)
		}
		if item.ID != "" {
			for i, existing := range current {
				if existing.ID == item.ID {
					current[i] = item
					return normalizeSlotState(current)
				}
			}
		}
		current = append(current, item)
		return normalizeSlotState(current)
	})
}

// RemoveSlot removes the item with matching ID from the slot queue.
func RemoveSlot(id string) {
	if id == "" {
		return
	}
	slot.Set(func(s SlotState) SlotState {
		if s.Queue == nil || len(*s.Queue) == 0 {
			return s
		}
		var current []SlotItem
		found := false
		for _, item := range *s.Queue {
			if !found && item.ID == id {
				found = true
				continue
			}
			current = append(current, item)
		}
		if !found {
			return s
		}
		return normalizeSlotState(current)
	})
}

// DequeueSlot removes the front item from the slot queue if non-empty.
func DequeueSlot() {
	slot.Set(func(s SlotState) SlotState {
		if s.Queue == nil || len(*s.Queue) == 0 {
			return s
		}
		return normalizeSlotState((*s.Queue)[1:])
	})
}

// ClearSlot resets SlotState to empty.
func ClearSlot() {
	slot.Set(func(SlotState) SlotState {
		return SlotState{}
	})
}
