package command

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func TestWarningsCommand(t *testing.T) {
	store.ClearSlot()
	t.Cleanup(store.ClearSlot)
	registry := NewRegistry()

	if err := registry.Execute("/warnings extra"); err == nil || !strings.Contains(err.Error(), "does not accept arguments") {
		t.Fatalf("unexpected error for /warnings extra: %v", err)
	}
	if active := store.GetSlot().Active; active != nil {
		t.Fatalf("invalid invocation opened a slot: %+v", active)
	}

	for range 2 {
		if err := registry.Execute("/warnings"); err != nil {
			t.Fatal(err)
		}
	}
	if q := store.GetSlot().Queue; q == nil || len(*q) != 1 || (*q)[0].ID != "slot-warnings" || (*q)[0].Kind != store.SlotKindPicker {
		t.Fatalf("expected one warnings picker, got %v", q)
	}
}
