package components

import (
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
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
