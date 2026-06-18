package components

import (
	"testing"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/geom"
)

func TestCheckbox(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		node := Checkbox(CheckboxProps{
			Label:   kitex.Text("Option 1"),
			Checked: true,
		})
		if node == nil {
			t.Fatal("Checkbox returned nil node")
		}
	})

	t.Run("Disabled", func(t *testing.T) {
		node := Checkbox(CheckboxProps{
			Label:    kitex.Text("Disabled Option"),
			Disabled: true,
		})
		if node == nil {
			t.Fatal("Disabled Checkbox returned nil node")
		}
	})

	t.Run("OnChange", func(t *testing.T) {
		changed := false
		node := Checkbox(CheckboxProps{
			Label:   kitex.Text("Click me"),
			Checked: false,
			OnChange: func(val bool) {
				changed = val
			},
		})
		if node == nil {
			t.Fatal("Checkbox with OnChange returned nil node")
		}
		_ = changed // avoid unused variable error
	})
}

func TestCheckboxInteraction(t *testing.T) {
	t.Run("Container click toggles state", func(t *testing.T) {
		doc := dom.NewDocument()
		var lastVal bool
		changeCount := 0

		cbProps := CheckboxProps{
			Checked: false,
			Label:   kitex.Text("Toggle Me"),
			OnChange: func(val bool) {
				lastVal = val
				changeCount++
			},
		}

		cbNode := Checkbox(cbProps)
		reals := cbNode.Instantiate(doc)
		if len(reals) == 0 {
			t.Fatal("expected at least one instantiated node")
		}
		realContainer := reals[0]

		elEl, ok := realContainer.(element.Element)
		if !ok {
			t.Fatalf("expected container to be element.Element")
		}

		// Simulate click on the container (which handles handleChange)
		elEl.DispatchEvent(event.NewMouseEvent(event.EventClick, geom.Point{}, event.ButtonLeft, 0))

		if changeCount != 1 {
			t.Errorf("expected OnChange to be called once, got %d", changeCount)
		}
		if !lastVal {
			t.Errorf("expected checkbox value to toggle to true")
		}
	})

	t.Run("Inner checkbox click propagates change event and stops click propagation", func(t *testing.T) {
		doc := dom.NewDocument()
		var lastVal bool
		changeCount := 0

		cbProps := CheckboxProps{
			Checked: false,
			Label:   kitex.Text("Toggle Me"),
			OnChange: func(val bool) {
				lastVal = val
				changeCount++
			},
		}

		cbNode := Checkbox(cbProps)
		reals := cbNode.Instantiate(doc)
		if len(reals) == 0 {
			t.Fatal("expected at least one instantiated node")
		}
		realContainer := reals[0]

		// Find the inner checkbox element
		el, ok := realContainer.(dom.Element)
		if !ok {
			t.Fatalf("expected container to be dom.Element")
		}
		innerCheckbox := el.QuerySelector("checkbox")
		if innerCheckbox == nil {
			t.Fatalf("could not find nested checkbox element")
		}

		elEl, ok := innerCheckbox.(element.Element)
		if !ok {
			t.Fatalf("expected inner checkbox to be element.Element")
		}

		// Dispatch click event directly on inner checkbox
		clickEv := event.NewMouseEvent(event.EventClick, geom.Point{}, event.ButtonLeft, 0)
		elEl.DispatchEvent(clickEv)

		if !clickEv.PropagationStopped() {
			t.Errorf("expected click event propagation to be stopped")
		}

		// Click event is stopped, but because clicking the inner checkbox causes a native state toggle and change event,
		// let's verify if OnChange is triggered
		if changeCount != 1 {
			t.Errorf("expected OnChange to be called once, got %d", changeCount)
		}
		if !lastVal {
			t.Errorf("expected checkbox value to toggle to true")
		}
	})
}
