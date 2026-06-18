package components

import (
	"testing"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/event"
)

func TestInput(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		node := Input(InputProps{
			Name:  "username",
			Value: "admin",
		})
		if node == nil {
			t.Fatal("Input returned nil node")
		}
	})

	t.Run("Disabled", func(t *testing.T) {
		node := Input(InputProps{
			Name:     "password",
			Disabled: true,
		})
		if node == nil {
			t.Fatal("Disabled Input returned nil node")
		}
	})

	t.Run("OnChange", func(t *testing.T) {
		var newValue string
		node := Input(InputProps{
			OnChange: func(val string) {
				newValue = val
			},
		})
		if node == nil {
			t.Fatal("Input with OnChange returned nil node")
		}
		_ = newValue
	})

	t.Run("Placeholder", func(t *testing.T) {
		node := Input(InputProps{
			Placeholder: "Enter text...",
		})
		if node == nil {
			t.Fatal("Input with placeholder returned nil node")
		}
	})

	t.Run("Colors and Variants", func(t *testing.T) {
		colors := []InputColor{InputPrimary, InputSecondary, InputTertiary, InputSuccess, InputInfo, InputError}
		variants := []InputVariant{InputOutline, InputSolid, InputUnderline}

		for _, col := range colors {
			for _, variant := range variants {
				node := Input(InputProps{
					Color:   col,
					Variant: variant,
				})
				if node == nil {
					t.Fatalf("Input with color %s and variant %s returned nil node", col, variant)
				}
			}
		}
	})
}

func TestInputInteraction(t *testing.T) {
	t.Run("Focus and blur invoke callbacks", func(t *testing.T) {
		doc := dom.NewDocument()
		focused := false
		blurred := false

		node := Input(InputProps{
			OnFocus: func() {
				focused = true
			},
			OnBlur: func() {
				blurred = true
			},
		})

		reals := node.Instantiate(doc)
		if len(reals) == 0 {
			t.Fatal("expected at least one instantiated node")
		}
		realInput := reals[0].(element.Element)

		// Simulate focus
		realInput.DispatchEvent(event.NewFocusEvent(event.EventFocus, realInput))
		if !focused {
			t.Errorf("expected OnFocus callback to be called")
		}

		// Simulate blur
		realInput.DispatchEvent(event.NewFocusEvent(event.EventBlur, realInput))
		if !blurred {
			t.Errorf("expected OnBlur callback to be called")
		}
	})
}
