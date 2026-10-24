package components

import (
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
)

func TestSlotRender(t *testing.T) {
	store.ClearSlot()
	defer store.ClearSlot()

	// When no active slot item, Slot node is instantiated
	node := Slot()
	if node == nil {
		t.Fatal("expected Slot() component node to not be nil")
	}

	kinds := []store.SlotKind{
		store.SlotKindPermission,
		store.SlotKindQuestion,
		store.SlotKindPicker,
		store.SlotKindAutocomplete,
		store.SlotKind("unknown"),
	}

	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			store.ClearSlot()
			store.EnqueueSlot(store.SlotItem{
				ID:   "test-item",
				Kind: kind,
				Data: "data",
			})
			n := Slot()
			if n == nil {
				t.Fatalf("expected Slot() component node to not be nil for kind %s", kind)
			}
		})
	}
}
