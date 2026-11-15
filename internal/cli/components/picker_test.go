package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
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

// TestPickerOnOpenFiresOncePerMount guards the mount effect's dependency list.
// A nil deps slice re-runs the effect after every render, so a picker whose
// options change underneath it would notify its caller that it opened again.
func TestPickerOnOpenFiresOncePerMount(t *testing.T) {
	var opens int
	b := mock.New(80, 24)
	app := trail.FC("OptionsHost", func(opts []uicomponents.Option) kitex.Node {
		return Picker(PickerProps{
			Title:   "Pick",
			Options: opts,
			OnOpen:  func() { opens++ },
		})
	})

	inst, err := trail.New(app([]uicomponents.Option{{Value: "a", Label: "Alpha"}}), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()

	if opens != 1 {
		t.Fatalf("expected OnOpen once on mount, got %d", opens)
	}

	// Re-render with a different option list: the picker updates, but it is the
	// same mounted picker, so it did not open again.
	inst.Rerender(app([]uicomponents.Option{
		{Value: "a", Label: "Alpha"},
		{Value: "b", Label: "Beta"},
	}))
	inst.Frame()

	if opens != 1 {
		t.Fatalf("expected OnOpen to stay at 1 across re-renders, got %d", opens)
	}
}

// TestPickerSubtitleAndGroups renders a picker with a subtitle line and grouped
// options, which is how the MCP server browser presents its listing.
func TestPickerSubtitleAndGroups(t *testing.T) {
	render := renderComponent(t, 80)
	out := render(Picker(PickerProps{
		Title:    "Manage MCP servers",
		Subtitle: "2 servers",
		Options: []uicomponents.Option{
			{Value: "notes", Label: "notes", Cells: []string{"notes", "2 tools"}},
			{Value: "sentry", Label: "sentry", Cells: []string{"sentry", "needs authentication"}},
		},
		Columns: []uicomponents.Column{{Width: 20}, {Width: 0}},
		GroupLabel: func(opt uicomponents.Option) string {
			return "Workspace MCPs (.agents/mcps)"
		},
	}))
	for _, want := range []string{"Manage MCP servers", "2 servers", "Workspace MCPs (.agents/mcps)", "notes", "sentry"} {
		if !strings.Contains(out, want) {
			t.Errorf("picker output missing %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "Workspace MCPs (.agents/mcps)"); n != 1 {
		t.Errorf("group heading rendered %d times, want 1:\n%s", n, out)
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

	out := testutils.SurfaceText(b)
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
	out = testutils.SurfaceText(b)
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
	out = testutils.SurfaceText(b)
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

// TestPickerCtrlNavigationMovesCursor covers the readline-style aliases: Ctrl+P
// and Ctrl+N have to move the cursor exactly like the arrow keys do.
func TestPickerCtrlNavigationMovesCursor(t *testing.T) {
	opts := []uicomponents.Option{
		{Value: "opt1", Label: "Option One"},
		{Value: "opt2", Label: "Option Two"},
		{Value: "opt3", Label: "Option Three"},
	}

	var selected []string
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Picker(PickerProps{
			Title:   "Select an option:",
			Options: opts,
			OnSelect: func(opt uicomponents.Option) {
				selected = append(selected, opt.Value)
			},
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()

	press := func(code rune, text string, mod key.Mod) {
		inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: code, Text: text, Mod: mod}})
		inst.Frame()
	}

	press('n', "n", key.ModCtrl)
	press('n', "n", key.ModCtrl)
	press(key.KeyEnter, "\n", 0)
	press('p', "p", key.ModCtrl)
	press(key.KeyEnter, "\n", 0)

	if strings.Join(selected, ",") != "opt3,opt2" {
		t.Errorf("expected ctrl+n/ctrl+p to move the cursor, got %v", selected)
	}
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

	out := testutils.SurfaceText(b)
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

func TestPickerDisabledReasonNotice(t *testing.T) {
	opts := []uicomponents.Option{
		{Value: "opt1", Label: "Option One"},
		{Value: "opt2", Label: "Option Two"},
	}

	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Picker(PickerProps{
			Title:   "Tasks:",
			Options: opts,
			ConfirmActions: map[string]ConfirmAction{
				"x": {
					DisabledReason: func(opt uicomponents.Option) string {
						if opt.Value == "opt1" {
							return "Task is already completed."
						}
						return ""
					},
					Prompt: func(opt uicomponents.Option) string {
						return "Kill task " + opt.Label + "?"
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

	out := testutils.SurfaceText(b)
	if strings.Contains(out, "Task is already completed.") {
		t.Errorf("notice should not be visible initially")
	}

	// Press 'x' on opt1 (which has disabled reason)
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'x', Text: "x"}})
	inst.Frame()
	out = testutils.SurfaceText(b)
	if !strings.Contains(out, "Task is already completed.") {
		t.Errorf("expected notice in output after pressing x: %s", out)
	}

	// Press 'down' (or any key) to navigate and clear notice
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyDown}})
	inst.Frame()
	out = testutils.SurfaceText(b)
	if strings.Contains(out, "Task is already completed.") {
		t.Errorf("notice should be cleared after pressing down: %s", out)
	}

	// On opt2, DisabledReason returns "", so 'x' enters confirm mode
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'x', Text: "x"}})
	inst.Frame()
	out = testutils.SurfaceText(b)
	if !strings.Contains(out, "Kill task Option Two?") {
		t.Errorf("expected confirm prompt for opt2: %s", out)
	}
}
