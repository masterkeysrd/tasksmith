package store

import (
	"testing"
)

func TestSlotKindHidesComposer(t *testing.T) {
	tests := []struct {
		kind SlotKind
		want bool
	}{
		{kind: SlotKindPermission, want: true},
		{kind: SlotKindQuestion, want: true},
		{kind: SlotKindPicker, want: true},
		{kind: SlotKindAutocomplete, want: false},
		{kind: SlotKind("unknown"), want: false},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			if got := tt.kind.HidesComposer(); got != tt.want {
				t.Fatalf("SlotKind(%q).HidesComposer() = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}

func TestSlotEnqueueAndDequeueFIFO(t *testing.T) {
	ClearSlot()
	defer ClearSlot()

	if s := GetSlot(); s.Active != nil || s.Queue != nil {
		t.Fatalf("expected initial slot state to be empty, got %+v", s)
	}

	item1 := SlotItem{ID: "item-1", Kind: SlotKindQuestion, Data: "question-1"}
	item2 := SlotItem{ID: "item-2", Kind: SlotKindPermission, Data: "permission-2"}
	item3 := SlotItem{ID: "item-3", Kind: SlotKindPicker, Data: "picker-3"}

	EnqueueSlot(item1)
	s := GetSlot()
	if s.Active == nil || s.Active.ID != "item-1" {
		t.Fatalf("expected Active to be item-1, got %+v", s.Active)
	}
	if s.Queue == nil || len(*s.Queue) != 1 {
		t.Fatalf("expected queue length 1, got %+v", s.Queue)
	}

	EnqueueSlot(item2)
	EnqueueSlot(item3)
	s = GetSlot()
	if s.Active == nil || s.Active.ID != "item-1" {
		t.Fatalf("expected Active to still be item-1, got %+v", s.Active)
	}
	if s.Queue == nil || len(*s.Queue) != 3 {
		t.Fatalf("expected queue length 3, got %+v", s.Queue)
	}
	if (*s.Queue)[0].ID != "item-1" || (*s.Queue)[1].ID != "item-2" || (*s.Queue)[2].ID != "item-3" {
		t.Fatalf("queue items not in FIFO order: %+v", *s.Queue)
	}

	// Dequeue item-1 -> item-2 becomes active
	DequeueSlot()
	s = GetSlot()
	if s.Active == nil || s.Active.ID != "item-2" {
		t.Fatalf("expected Active to advance to item-2, got %+v", s.Active)
	}
	if s.Queue == nil || len(*s.Queue) != 2 {
		t.Fatalf("expected queue length 2, got %+v", s.Queue)
	}

	// Dequeue item-2 -> item-3 becomes active
	DequeueSlot()
	s = GetSlot()
	if s.Active == nil || s.Active.ID != "item-3" {
		t.Fatalf("expected Active to advance to item-3, got %+v", s.Active)
	}
	if s.Queue == nil || len(*s.Queue) != 1 {
		t.Fatalf("expected queue length 1, got %+v", s.Queue)
	}

	// Dequeue item-3 -> queue empty, Active becomes nil
	DequeueSlot()
	s = GetSlot()
	if s.Active != nil {
		t.Fatalf("expected Active to be nil when queue is empty, got %+v", s.Active)
	}
	if s.Queue != nil {
		t.Fatalf("expected Queue to be nil when queue is empty, got %+v", s.Queue)
	}

	// Dequeueing empty queue is a no-op
	DequeueSlot()
	s = GetSlot()
	if s.Active != nil || s.Queue != nil {
		t.Fatalf("expected Active and Queue to remain nil on DequeueSlot() on empty queue")
	}
}

func TestSlotEnqueueGeneratesIDWhenEmpty(t *testing.T) {
	ClearSlot()
	defer ClearSlot()

	EnqueueSlot(SlotItem{Kind: SlotKindPicker, Data: "picker-1"})
	s := GetSlot()
	if s.Active == nil {
		t.Fatal("expected Active to be non-nil")
	}
	if s.Active.ID == "" {
		t.Fatal("expected auto-generated non-empty ID")
	}

	RemoveSlot(s.Active.ID)
	if GetSlot().Active != nil {
		t.Fatal("expected Active to be removed after RemoveSlot")
	}
}

func TestSlotEnqueueDeduplication(t *testing.T) {
	ClearSlot()
	defer ClearSlot()

	item1 := SlotItem{ID: "dup-1", Kind: SlotKindQuestion, Data: "initial-data"}
	EnqueueSlot(item1)

	s := GetSlot()
	if s.Active == nil || s.Active.Data != "initial-data" {
		t.Fatalf("expected active item data initial-data, got %+v", s.Active)
	}

	// Re-enqueue same ID with updated data
	item1Updated := SlotItem{ID: "dup-1", Kind: SlotKindQuestion, Data: "updated-data"}
	EnqueueSlot(item1Updated)

	s = GetSlot()
	if s.Queue == nil || len(*s.Queue) != 1 {
		t.Fatalf("expected queue length 1 after re-enqueueing duplicate ID, got %d", len(*s.Queue))
	}
	if s.Active == nil || s.Active.Data != "updated-data" {
		t.Fatalf("expected active item data to be updated to updated-data, got %+v", s.Active)
	}

	// Add second item, then re-enqueue second item to verify in-place update in non-head position
	item2 := SlotItem{ID: "dup-2", Kind: SlotKindPermission, Data: "perm-initial"}
	EnqueueSlot(item2)

	item2Updated := SlotItem{ID: "dup-2", Kind: SlotKindPermission, Data: "perm-updated"}
	EnqueueSlot(item2Updated)

	s = GetSlot()
	if len(*s.Queue) != 2 {
		t.Fatalf("expected queue length 2, got %d", len(*s.Queue))
	}
	if (*s.Queue)[1].Data != "perm-updated" {
		t.Fatalf("expected queue[1] data to be perm-updated, got %+v", (*s.Queue)[1])
	}

	// Empty ID should not deduplicate
	EnqueueSlot(SlotItem{ID: "", Kind: SlotKindAutocomplete, Data: "auto-1"})
	EnqueueSlot(SlotItem{ID: "", Kind: SlotKindAutocomplete, Data: "auto-2"})
	s = GetSlot()
	if len(*s.Queue) != 4 {
		t.Fatalf("expected queue length 4 when enqueuing empty IDs, got %d", len(*s.Queue))
	}
}

func TestSlotRemoveSlot(t *testing.T) {
	ClearSlot()
	defer ClearSlot()

	// Removing from empty queue does nothing
	RemoveSlot("non-existent")
	if s := GetSlot(); s.Active != nil || s.Queue != nil {
		t.Fatalf("expected empty state after RemoveSlot on empty queue")
	}

	// Removing with empty string does nothing
	RemoveSlot("")
	if s := GetSlot(); s.Active != nil || s.Queue != nil {
		t.Fatalf("expected empty state after RemoveSlot('')")
	}

	itemA := SlotItem{ID: "A", Kind: SlotKindPermission}
	itemB := SlotItem{ID: "B", Kind: SlotKindQuestion}
	itemC := SlotItem{ID: "C", Kind: SlotKindPicker}

	EnqueueSlot(itemA)
	EnqueueSlot(itemB)
	EnqueueSlot(itemC)

	// Remove middle item ("B")
	RemoveSlot("B")
	s := GetSlot()
	if len(*s.Queue) != 2 {
		t.Fatalf("expected queue length 2 after removing middle item, got %d", len(*s.Queue))
	}
	if s.Active == nil || s.Active.ID != "A" {
		t.Fatalf("expected Active to remain A, got %+v", s.Active)
	}
	if (*s.Queue)[0].ID != "A" || (*s.Queue)[1].ID != "C" {
		t.Fatalf("expected queue [A, C], got %+v", *s.Queue)
	}

	// Remove head item ("A") -> Active should advance to "C"
	RemoveSlot("A")
	s = GetSlot()
	if len(*s.Queue) != 1 {
		t.Fatalf("expected queue length 1 after removing head item, got %d", len(*s.Queue))
	}
	if s.Active == nil || s.Active.ID != "C" {
		t.Fatalf("expected Active to advance to C, got %+v", s.Active)
	}

	// Remove remaining item ("C") -> queue becomes empty, Active becomes nil
	RemoveSlot("C")
	s = GetSlot()
	if s.Active != nil || s.Queue != nil {
		t.Fatalf("expected Active and Queue to be nil after removing last item, got %+v", s)
	}

	// Remove non-existent ID does not change state
	EnqueueSlot(itemA)
	RemoveSlot("does-not-exist")
	s = GetSlot()
	if len(*s.Queue) != 1 || s.Active.ID != "A" {
		t.Fatalf("expected queue to remain unchanged after removing non-existent ID")
	}
}

func TestSlotClearSlot(t *testing.T) {
	ClearSlot()
	defer ClearSlot()

	EnqueueSlot(SlotItem{ID: "1", Kind: SlotKindPermission})
	EnqueueSlot(SlotItem{ID: "2", Kind: SlotKindQuestion})

	if s := GetSlot(); s.Active == nil || s.Queue == nil {
		t.Fatalf("expected slot to be populated before clear")
	}

	ClearSlot()
	s := GetSlot()
	if s.Active != nil {
		t.Fatalf("expected Active to be nil after ClearSlot, got %+v", s.Active)
	}
	if s.Queue != nil {
		t.Fatalf("expected Queue to be nil after ClearSlot, got %+v", s.Queue)
	}
}

func TestSlotUseHideComposerAndActiveSlot(t *testing.T) {
	ClearSlot()
	defer ClearSlot()

	// Initial: no active slot -> UseHideComposer returns false
	if GetSlot().Active != nil {
		t.Fatal("expected no active slot")
	}

	// Autocomplete does not hide composer
	EnqueueSlot(SlotItem{ID: "auto", Kind: SlotKindAutocomplete})
	s := GetSlot()
	if s.Active == nil || s.Active.Kind.HidesComposer() {
		t.Fatalf("expected autocomplete to not hide composer")
	}

	// Clear and enqueue permission which hides composer
	ClearSlot()
	EnqueueSlot(SlotItem{ID: "perm", Kind: SlotKindPermission})
	s = GetSlot()
	if s.Active == nil || !s.Active.Kind.HidesComposer() {
		t.Fatalf("expected permission to hide composer")
	}
}
