package components

import (
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
)

func TestButton(t *testing.T) {
	render := func(node kitex.Node) kitex.Node {
		return node
	}

	t.Run("Basic", func(t *testing.T) {
		node := render(Button(ButtonProps{
			Children: []kitex.Node{kitex.Text("Click me")},
		}))
		if node == nil {
			t.Fatal("Button returned nil node")
		}
	})

	t.Run("WithIcons", func(t *testing.T) {
		node := render(Button(ButtonProps{
			StartIcon: kitex.Text("["),
			EndIcon:   kitex.Text("]"),
			Children:  []kitex.Node{kitex.Text("Icons")},
		}))
		if node == nil {
			t.Fatal("Button with icons returned nil node")
		}
	})

	t.Run("Variants", func(t *testing.T) {
		variants := []ButtonVariant{ButtonOutline, ButtonSolid, ButtonText}
		for _, v := range variants {
			node := render(Button(ButtonProps{
				Variant: v,
			}))
			if node == nil {
				t.Errorf("Button with variant %s returned nil node", v)
			}
		}
	})

	t.Run("Disabled", func(t *testing.T) {
		node := render(Button(ButtonProps{
			Disabled: true,
		}))
		if node == nil {
			t.Fatal("Disabled button returned nil node")
		}
	})
}
