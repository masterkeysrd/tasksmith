package components

import (
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/tasksmith/internal/tui/highlight"
)

func TestButton(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		node := Button(ButtonProps{
			Group: highlight.Set("Button"),
		}, kitex.Text("Click me"))
		if node == nil {
			t.Fatal("Button returned nil node")
		}
	})

	t.Run("WithIcons", func(t *testing.T) {
		node := Button(ButtonProps{
			Group:     highlight.Set("Button"),
			StartIcon: kitex.Text("["),
			EndIcon:   kitex.Text("]"),
		}, kitex.Text("Icons"))
		if node == nil {
			t.Fatal("Button with icons returned nil node")
		}
	})

	t.Run("Variants", func(t *testing.T) {
		variants := []ButtonVariant{ButtonOutline}
		for _, v := range variants {
			node := Button(ButtonProps{
				Group:   highlight.Set("Button"),
				Variant: v,
			})
			if node == nil {
				t.Errorf("Button with variant %s returned nil node", v)
			}
		}
	})

	t.Run("Disabled", func(t *testing.T) {
		node := Button(ButtonProps{
			Group:    highlight.Set("Button"),
			Disabled: true,
		})
		if node == nil {
			t.Fatal("Disabled button returned nil node")
		}
	})
}
