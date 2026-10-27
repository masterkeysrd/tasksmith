package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
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

func TestSlotRendersPermissionPrompt(t *testing.T) {
	store.ClearSlot()
	defer store.ClearSlot()

	req := api.PermissionRequest{
		ID:            "p1",
		ToolName:      "bash",
		Target:        "ls -la",
		Summary:       "List directory",
		Action:        api.ActionExec,
		Safety:        api.SafetyOutside,
		GrantPatterns: []string{"bash(ls *)"},
	}

	store.EnqueueSlot(store.SlotItem{
		ID:   "perm-p1",
		Kind: store.SlotKindPermission,
		Data: req,
	})

	b := mock.New(80, 24)
	app := trail.SimpleFC("TestSlotApp", func() kitex.Node {
		return Slot()
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()

	out := surfaceText(b)
	if !strings.Contains(out, "Permission required") {
		t.Errorf("expected 'Permission required' in rendered Slot output, got:\n%s", out)
	}
	if !strings.Contains(out, "bash · ls -la") {
		t.Errorf("expected 'bash · ls -la' in rendered Slot output, got:\n%s", out)
	}
	if !strings.Contains(out, "Yes, allow once") {
		t.Errorf("expected 'Yes, allow once' in rendered Slot output, got:\n%s", out)
	}
	if !strings.Contains(out, "Keyboard:") {
		t.Errorf("expected KeyboardHint in rendered Slot output, got:\n%s", out)
	}
}
