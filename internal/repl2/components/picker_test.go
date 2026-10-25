package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
)

func TestFilterOptions(t *testing.T) {
	opts := []uicomponents.Option{
		{Value: "v1", Label: "Alpha", Description: "First Greek letter", Cells: []string{"A", "alpha"}},
		{Value: "v2", Label: "Beta", Description: "Second Greek letter", Cells: []string{"B", "beta"}},
		{Value: "v3", Label: "Gamma", Description: "Third Greek letter", Cells: []string{"G", "gamma"}},
	}

	if res := filterOptions(opts, ""); len(res) != 3 {
		t.Errorf("empty query expected 3 opts, got %d", len(res))
	}
	if res := filterOptions(opts, "alpha"); len(res) != 1 || res[0].Value != "v1" {
		t.Errorf("label match expected v1, got %+v", res)
	}
	if res := filterOptions(opts, "Second"); len(res) != 1 || res[0].Value != "v2" {
		t.Errorf("desc match expected v2, got %+v", res)
	}
	if res := filterOptions(opts, "nonexistent"); len(res) != 0 {
		t.Errorf("nonexistent query expected 0 opts, got %d", len(res))
	}
}

func TestPickerComponentAndKeyboardHint(t *testing.T) {
	opts := []uicomponents.Option{
		{Value: "opt1", Label: "Option One", Description: "First option", Current: true},
		{Value: "opt2", Label: "Option Two", Description: "Second option"},
	}

	var selectedValue string
	var canceled bool
	var opened bool
	var confirmed bool
	var submittedVal string

	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Picker(PickerProps{
			Title:   "Select an option:",
			Options: opts,
			OnOpen: func() {
				opened = true
			},
			OnSelect: func(opt uicomponents.Option) {
				selectedValue = opt.Value
			},
			OnCancel: func() {
				canceled = true
			},
			ConfirmActions: map[string]ConfirmAction{
				"d": {
					Prompt: func(opt uicomponents.Option) string {
						return "Delete " + opt.Label + "?"
					},
					OnConfirm: func(opt uicomponents.Option) {
						confirmed = true
					},
				},
			},
			InputActions: map[string]InputAction{
				"r": {
					Prompt: "Rename:",
					OnSubmit: func(opt uicomponents.Option, value string) {
						submittedVal = value
					},
				},
			},
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()

	if !opened {
		t.Errorf("expected OnOpen to be called on mount")
	}

	out := surfaceText(b)
	if !strings.Contains(out, "Select an option:") {
		t.Errorf("expected title in output: %s", out)
	}
	if !strings.Contains(out, "Option One") || !strings.Contains(out, "Option Two") {
		t.Errorf("expected options in output: %s", out)
	}

	// 1. Verify Browse mode KeyboardHint
	if !strings.Contains(out, "Keyboard:") || !strings.Contains(out, "↑/↓ Navigate") || !strings.Contains(out, "enter Confirm") || !strings.Contains(out, "esc Cancel") {
		t.Errorf("expected default KeyboardHint in browse mode: %s", out)
	}

	// 2. Trigger Confirm mode via 'd'
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'd', Text: "d"}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "Delete Option One?") {
		t.Errorf("expected confirm prompt in output: %s", out)
	}
	if !strings.Contains(out, "y/enter Confirm") || !strings.Contains(out, "n/esc Cancel") {
		t.Errorf("expected confirm mode KeyboardHint: %s", out)
	}
	// Cancel confirm mode via 'n'
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'n', Text: "n"}})
	inst.Frame()

	// 3. Trigger Input mode via 'r'
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'r', Text: "r"}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "Rename:") {
		t.Errorf("expected input prompt in output: %s", out)
	}
	if !strings.Contains(out, "enter Submit") || !strings.Contains(out, "esc Cancel") {
		t.Errorf("expected input mode KeyboardHint: %s", out)
	}
	// Cancel input mode via Esc
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEscape}})
	inst.Frame()

	// 4. Press Enter on opt1
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})
	inst.Frame()
	if selectedValue != "opt1" {
		t.Errorf("expected selectedValue=opt1, got %q", selectedValue)
	}

	// 5. Press Esc
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEscape}})
	inst.Frame()
	if !canceled {
		t.Errorf("expected canceled=true after Escape")
	}
	_ = confirmed
	_ = submittedVal
}

func TestSlotRendersPickerAndRemovesOnAction(t *testing.T) {
	store.ClearSlot()
	defer store.ClearSlot()

	opts := []uicomponents.Option{
		{Value: "a", Label: "Alpha"},
		{Value: "b", Label: "Beta"},
	}

	var selected string
	slotID := "picker-slot-1"
	store.EnqueueSlot(store.SlotItem{
		ID:   slotID,
		Kind: store.SlotKindPicker,
		Data: NewPicker("Choose", opts, func(opt uicomponents.Option) {
			selected = opt.Value
		}),
	})

	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Slot()
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()

	if store.GetSlot().Active == nil || store.GetSlot().Active.ID != slotID {
		t.Fatalf("expected active slot %s", slotID)
	}

	out := surfaceText(b)
	if !strings.Contains(out, "Choose") || !strings.Contains(out, "Alpha") {
		t.Fatalf("expected picker rendered in slot: %s", out)
	}

	// Press Enter to select
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})
	inst.Frame()

	if selected != "a" {
		t.Errorf("expected selected=a, got %q", selected)
	}
	// Slot should be removed!
	if store.GetSlot().Active != nil {
		t.Errorf("expected active slot to be removed after selection, got %+v", store.GetSlot().Active)
	}
}

func TestSlotPickerEscapeRemovesSlotWithAutoGeneratedID(t *testing.T) {
	store.ClearSlot()
	defer store.ClearSlot()

	opts := []uicomponents.Option{
		{Value: "a", Label: "Alpha"},
	}

	var canceled bool
	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: PickerProps{
			Title:   "Choose",
			Options: opts,
			OnCancel: func() {
				canceled = true
			},
		},
	})

	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Slot()
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()

	if store.GetSlot().Active == nil {
		t.Fatal("expected active slot")
	}

	// Press Esc
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEscape}})
	inst.Frame()

	if !canceled {
		t.Error("expected canceled=true after Escape")
	}
	if store.GetSlot().Active != nil {
		t.Errorf("expected active slot to be removed after Escape, got %+v", store.GetSlot().Active)
	}
}
