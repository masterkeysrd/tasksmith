package components

import (
	"testing"
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
}
