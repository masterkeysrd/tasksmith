package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
)

func TestFilterOptions(t *testing.T) {
	opts := []Option{
		{Value: "v1", Label: "Alpha", Description: "First Greek letter", Columns: []string{"A", "alpha"}},
		{Value: "v2", Label: "Beta", Description: "Second Greek letter", Columns: []string{"B", "beta"}},
		{Value: "v3", Label: "Gamma", Description: "Third Greek letter", Columns: []string{"G", "gamma"}},
	}

	// 1. Empty query returns all opts
	if res := filterOptions(opts, ""); len(res) != 3 {
		t.Errorf("empty query expected 3 opts, got %d", len(res))
	}
	if res := filterOptions(opts, "   "); len(res) != 3 {
		t.Errorf("whitespace query expected 3 opts, got %d", len(res))
	}

	// 2. Match by Label (case-insensitive)
	res := filterOptions(opts, "alpha")
	if len(res) != 1 || res[0].Value != "v1" {
		t.Errorf("label match expected v1, got %+v", res)
	}

	// 3. Match by Description
	res = filterOptions(opts, "Second")
	if len(res) != 1 || res[0].Value != "v2" {
		t.Errorf("desc match expected v2, got %+v", res)
	}

	// 4. Match by Value
	res = filterOptions(opts, "v3")
	if len(res) != 1 || res[0].Value != "v3" {
		t.Errorf("value match expected v3, got %+v", res)
	}

	// 5. Match by Columns
	res = filterOptions(opts, "gamma")
	if len(res) != 1 || res[0].Value != "v3" {
		t.Errorf("column match expected v3, got %+v", res)
	}

	// 6. Query matching nothing
	res = filterOptions(opts, "nonexistent")
	if len(res) != 0 {
		t.Errorf("nonexistent query expected 0 opts, got %d", len(res))
	}
}

func TestPickerFunctionalComponent(t *testing.T) {
	opts := []Option{
		{Value: "opt1", Label: "Option One", Description: "First option", Current: true},
		{Value: "opt2", Label: "Option Two", Description: "Second option"},
	}

	var selectedValue string
	var canceled bool
	var opened bool

	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Picker(PickerProps{
			Title:   "Select an option:",
			Options: opts,
			OnOpen: func() {
				opened = true
			},
			OnSelect: func(opt Option) any {
				selectedValue = opt.Value
				return opt.Value
			},
			OnAction: func(action any, closePicker bool) {
				if s, ok := action.(string); ok {
					selectedValue = s
				}
			},
			OnCancel: func() {
				canceled = true
			},
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	if !opened {
		t.Errorf("expected OnOpen to be called on mount")
	}

	out := surfaceText(b)
	if !strings.Contains(out, "Select an option:") || !strings.Contains(out, "Option One") {
		t.Errorf("picker output missing title or options:\n%s", out)
	}
	if !strings.Contains(out, DefaultPickerFooter) {
		t.Errorf("picker output missing DefaultPickerFooter:\n%s", out)
	}

	// Press Enter -> selects Option One (opt1)
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEnter},
	})
	inst.Frame()
	if selectedValue != "opt1" {
		t.Errorf("selected value = %q, want 'opt1'", selectedValue)
	}

	// Press Escape -> cancel called
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEscape},
	})
	inst.Frame()
	if !canceled {
		t.Errorf("expected OnCancel to be called on Escape")
	}
}

func TestPickerAlignmentAndNoLayoutShift(t *testing.T) {
	opts := []Option{
		{Value: "default", Label: "default", Description: "Allow safe reads", Current: true},
		{Value: "accept_edits", Label: "accept_edits", Description: "Allow workspace edits"},
	}

	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Picker(PickerProps{
			Title:   "Select mode:",
			Options: opts,
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	frame := b.LastFrame().Surface
	if frame == nil {
		t.Fatalf("expected rendered frame")
	}

	var row0Y, row1Y int
	for y := 0; y < frame.Height; y++ {
		rowStr := ""
		for x := 0; x < frame.Width; x++ {
			rowStr += frame.CellAt(x, y).Content
		}
		if strings.Contains(rowStr, "default") {
			row0Y = y
		}
		if strings.Contains(rowStr, "accept_edits") {
			row1Y = y
		}
	}

	// Both labels must start at X=2
	if frame.CellAt(0, row0Y).Content != "❯" || frame.CellAt(1, row0Y).Content != " " || frame.CellAt(2, row0Y).Content != "d" {
		t.Errorf("row0 initial alignment error: [%q, %q, %q]",
			frame.CellAt(0, row0Y).Content, frame.CellAt(1, row0Y).Content, frame.CellAt(2, row0Y).Content)
	}
	if frame.CellAt(0, row1Y).Content != " " || frame.CellAt(1, row1Y).Content != " " || frame.CellAt(2, row1Y).Content != "a" {
		t.Errorf("row1 initial alignment error: [%q, %q, %q]",
			frame.CellAt(0, row1Y).Content, frame.CellAt(1, row1Y).Content, frame.CellAt(2, row1Y).Content)
	}

	// Find description start X coordinate for row0 and row1 (table column alignment)
	var desc0X, desc1X int
	for x := 0; x < frame.Width-10; x++ {
		if frame.CellAt(x, row0Y).Content == "A" && frame.CellAt(x+1, row0Y).Content == "l" && frame.CellAt(x+2, row0Y).Content == "l" {
			desc0X = x
			break
		}
	}
	for x := 0; x < frame.Width-10; x++ {
		if frame.CellAt(x, row1Y).Content == "A" && frame.CellAt(x+1, row1Y).Content == "l" && frame.CellAt(x+2, row1Y).Content == "l" {
			desc1X = x
			break
		}
	}
	if desc0X == 0 || desc1X == 0 || desc0X != desc1X {
		t.Errorf("descriptions not column-aligned in table: desc0X=%d, desc1X=%d", desc0X, desc1X)
	}

	// Navigate Down and verify zero layout shift
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyDown},
	})
	inst.Frame()

	frameDown := b.LastFrame().Surface
	if frameDown.CellAt(0, row0Y).Content != " " || frameDown.CellAt(1, row0Y).Content != " " || frameDown.CellAt(2, row0Y).Content != "d" {
		t.Errorf("after Down, row0 alignment shifted: [%q, %q, %q]",
			frameDown.CellAt(0, row0Y).Content, frameDown.CellAt(1, row0Y).Content, frameDown.CellAt(2, row0Y).Content)
	}
	if frameDown.CellAt(0, row1Y).Content != "❯" || frameDown.CellAt(1, row1Y).Content != " " || frameDown.CellAt(2, row1Y).Content != "a" {
		t.Errorf("after Down, row1 alignment shifted: [%q, %q, %q]",
			frameDown.CellAt(0, row1Y).Content, frameDown.CellAt(1, row1Y).Content, frameDown.CellAt(2, row1Y).Content)
	}
}

func TestPickerFilterable(t *testing.T) {
	opts := []Option{
		{Value: "apple", Label: "Apple", Description: "Red fruit"},
		{Value: "banana", Label: "Banana", Description: "Yellow fruit"},
		{Value: "cherry", Label: "Cherry", Description: "Dark red fruit"},
	}

	var selected string
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Picker(PickerProps{
			Title:      "Select fruit:",
			Options:    opts,
			Filterable: true,
			OnSelect: func(opt Option) any {
				selected = opt.Value
				return opt.Value
			},
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	out := surfaceText(b)
	if !strings.Contains(out, "Filter:") || !strings.Contains(out, "Apple") {
		t.Fatalf("expected Filter: prompt and fruits, got:\n%s", out)
	}

	// 1. Type "ban" into filter input
	for _, ch := range "ban" {
		inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
			Key: key.Key{Code: ch, Text: string(ch)},
		})
	}
	inst.Frame()

	outFiltered := surfaceText(b)
	if !strings.Contains(outFiltered, "Banana") {
		t.Errorf("expected Banana in filtered output, got:\n%s", outFiltered)
	}
	if strings.Contains(outFiltered, "Apple") || strings.Contains(outFiltered, "Cherry") {
		t.Errorf("Apple and Cherry should be filtered out:\n%s", outFiltered)
	}

	// 2. Type "xyz" to match nothing -> renders "No matching options."
	for _, ch := range "xyz" {
		inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
			Key: key.Key{Code: ch, Text: string(ch)},
		})
	}
	inst.Frame()

	outEmpty := surfaceText(b)
	if !strings.Contains(outEmpty, "No matching options.") {
		t.Errorf("expected 'No matching options.' for unmatched filter, got:\n%s", outEmpty)
	}

	// 3. Backspace 6 times to clear "banxyz"
	for range 6 {
		inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
			Key: key.Key{Code: key.KeyBackspace},
		})
	}
	inst.Frame()

	outRestored := surfaceText(b)
	if !strings.Contains(outRestored, "Apple") || !strings.Contains(outRestored, "Cherry") {
		t.Errorf("expected restored options after backspaces, got:\n%s", outRestored)
	}

	// 4. Press Enter selects Apple (index 0)
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEnter},
	})
	inst.Frame()
	if selected != "apple" {
		t.Errorf("selected = %q, want 'apple'", selected)
	}
}

func TestPickerColumnsMultiColumn(t *testing.T) {
	opts := []Option{
		{
			Value:   "s1",
			Columns: []string{"sess-001", "main-workspace", "2m ago"},
			Current: true,
		},
		{
			Value:   "s2",
			Columns: []string{"sess-002", "branch-workspace", "1h ago"},
		},
	}

	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Picker(PickerProps{
			Title:   "Sessions",
			Options: opts,
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	out := surfaceText(b)
	if !strings.Contains(out, "sess-001") || !strings.Contains(out, "main-workspace") {
		t.Fatalf("multi-column columns missing from output:\n%s", out)
	}
	// Row 0 has Current: true, so last column should have " • current"
	if !strings.Contains(out, "2m ago • current") {
		t.Errorf("expected '2m ago • current' for current session, got:\n%s", out)
	}
	// Row 1 does not have Current: true
	if strings.Contains(out, "1h ago • current") {
		t.Errorf("row 1 should not have '• current':\n%s", out)
	}
}

func TestPickerConfirmActions(t *testing.T) {
	opts := []Option{
		{Value: "s1", Label: "Session One"},
		{Value: "s2", Label: "Session Two"},
	}

	var confirmedValue string
	var actionResult any
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Picker(PickerProps{
			Title:   "Sessions",
			Options: opts,
			ConfirmActions: map[string]ConfirmAction{
				"f4": {
					Prompt: func(opt Option) string {
						return fmt.Sprintf("Delete %q?", opt.Label)
					},
					OnConfirm: func(opt Option) any {
						confirmedValue = opt.Value
						return "deleted-" + opt.Value
					},
				},
			},
			OnAction: func(action any, closePicker bool) {
				actionResult = action
			},
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	// 1. Press f4 -> enters confirm mode
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyF4},
	})
	inst.Frame()

	outConfirm := surfaceText(b)
	if !strings.Contains(outConfirm, `? Delete "Session One"? (y/N)`) {
		t.Fatalf("expected confirm prompt, got:\n%s", outConfirm)
	}
	if !strings.Contains(outConfirm, "y/enter to confirm • n/esc to cancel") {
		t.Errorf("expected confirm footer hint, got:\n%s", outConfirm)
	}

	// 2. Press 'n' -> cancels back to browse mode without confirming
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: 'n', Text: "n"},
	})
	inst.Frame()
	if confirmedValue != "" {
		t.Errorf("expected no confirm on 'n', got %q", confirmedValue)
	}
	outCanceled := surfaceText(b)
	if strings.Contains(outCanceled, "? Delete") {
		t.Errorf("expected confirm prompt to disappear after 'n':\n%s", outCanceled)
	}

	// 3. Press f4 again and press 'y' -> confirms deletion
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyF4},
	})
	inst.Frame()
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: 'y', Text: "y"},
	})
	inst.Frame()
	if confirmedValue != "s1" {
		t.Errorf("confirmedValue = %q, want 's1'", confirmedValue)
	}
	if actionResult != "deleted-s1" {
		t.Errorf("actionResult = %v, want 'deleted-s1'", actionResult)
	}
}

func TestPickerInputActions(t *testing.T) {
	opts := []Option{
		{Value: "s1", Label: "Initial Session Name"},
	}

	var submittedValue string
	var actionResult any
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Picker(PickerProps{
			Title:   "Sessions",
			Options: opts,
			InputActions: map[string]InputAction{
				"f2": {
					Prompt: "Rename session:",
					InitialValue: func(opt Option) string {
						return opt.Label
					},
					OnSubmit: func(opt Option, val string) any {
						submittedValue = val
						return "renamed-" + val
					},
				},
			},
			OnAction: func(action any, closePicker bool) {
				actionResult = action
			},
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	// 1. Press f2 -> enters input mode
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyF2},
	})
	inst.Frame()

	outInput := surfaceText(b)
	if !strings.Contains(outInput, "Rename session:") {
		t.Fatalf("expected Rename session: prompt, got:\n%s", outInput)
	}
	if !strings.Contains(outInput, "enter to submit • esc to cancel") {
		t.Errorf("expected input footer hint, got:\n%s", outInput)
	}

	// 2. Press Escape -> cancels back to browse
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEscape},
	})
	inst.Frame()
	outEsc := surfaceText(b)
	if strings.Contains(outEsc, "Rename session:") {
		t.Errorf("expected input prompt to close on Escape:\n%s", outEsc)
	}

	// 3. Press f2 again, press Enter -> submits initial value
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyF2},
	})
	inst.Frame()
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEnter},
	})
	inst.Frame()
	if submittedValue != "Initial Session Name" {
		t.Errorf("submittedValue = %q, want 'Initial Session Name'", submittedValue)
	}
	if actionResult != "renamed-Initial Session Name" {
		t.Errorf("actionResult = %v, want 'renamed-Initial Session Name'", actionResult)
	}
}

func TestPickerCustomKeys(t *testing.T) {
	opts := []Option{
		{Value: "opt1", Label: "Option One"},
		{Value: "opt2", Label: "Option Two"},
	}

	var customInvoked string
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Picker(PickerProps{
			Title:   "Select:",
			Options: opts,
			Keys: map[string]func(opt Option) any{
				"d": func(opt Option) any {
					customInvoked = opt.Value
					return opt.Value
				},
			},
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	// Press 'd' -> triggers custom key action
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: 'd', Text: "d"},
	})
	inst.Frame()
	if customInvoked != "opt1" {
		t.Errorf("customInvoked = %q, want 'opt1'", customInvoked)
	}
}

func TestPickerCursorReconciliation(t *testing.T) {
	var currentOpts = []Option{
		{Value: "o1", Label: "Option 1"},
		{Value: "o2", Label: "Option 2"},
		{Value: "o3", Label: "Option 3"},
	}

	b := mock.New(80, 24)
	var rerender func(opts []Option)

	parent := trail.SimpleFC("ParentApp", func() kitex.Node {
		optsState, setOpts := kitex.UseState(currentOpts)
		rerender = func(next []Option) {
			setOpts(next)
		}
		return Picker(PickerProps{
			Title:   "Reconciliation Test",
			Options: optsState(),
		})
	})

	inst, err := trail.New(parent(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	// Move Down twice to select index 2 (Option 3)
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyDown}})
	inst.Frame()
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyDown}})
	inst.Frame()

	out2 := surfaceText(b)
	if !strings.Contains(out2, "❯ Option 3") {
		t.Fatalf("expected Option 3 selected, got:\n%s", out2)
	}

	// Shrink options list to 1 option
	rerender([]Option{{Value: "o1", Label: "Option 1"}})
	inst.Frame()

	outShrunk := surfaceText(b)
	if !strings.Contains(outShrunk, "❯ Option 1") {
		t.Errorf("expected cursor clamped to Option 1 after options shrunk, got:\n%s", outShrunk)
	}
}
