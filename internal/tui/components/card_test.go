package components

import (
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
)

func TestCard(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		node := Card(CardProps{},
			CardHeader(CardHeaderProps{
				Title: kitex.Text("Card Title"),
			}),
			CardContent(CardContentProps{}, kitex.Text("Card body content")),
			CardActions(CardActionsProps{},
				Button(ButtonProps{}, kitex.Text("OK")),
			),
		)
		if node == nil {
			t.Fatal("Card returned nil node")
		}
	})

	t.Run("OutOfOrder", func(t *testing.T) {
		// Even if children are passed out of order, they should be organized correctly.
		node := Card(CardProps{},
			CardActions(CardActionsProps{},
				Button(ButtonProps{}, kitex.Text("Action")),
			),
			CardHeader(CardHeaderProps{
				Title: kitex.Text("Header"),
			}),
			CardContent(CardContentProps{}, kitex.Text("Content")),
		)
		if node == nil {
			t.Fatal("Out of order Card returned nil node")
		}
	})

	t.Run("HeaderDetails", func(t *testing.T) {
		node := Card(CardProps{},
			CardHeader(CardHeaderProps{
				Avatar:    kitex.Text("(A)"),
				Title:     kitex.Text("Title"),
				Subheader: kitex.Text("Subheader"),
				Action:    kitex.Text("[X]"),
			}),
		)
		if node == nil {
			t.Fatal("Card with full header returned nil node")
		}
	})

	t.Run("Variants", func(t *testing.T) {
		variants := []CardVariant{CardDefault, CardOutlined}
		for _, v := range variants {
			node := Card(CardProps{
				Variant: v,
			},
				CardContent(CardContentProps{}, kitex.Text("Variant test")),
			)
			if node == nil {
				t.Errorf("Card with variant %s returned nil node", v)
			}
		}
	})
}
