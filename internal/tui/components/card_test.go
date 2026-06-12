package components

import (
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
)

func TestCard(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		node := Card(CardProps{
			Children: []kitex.Node{
				CardHeader(CardHeaderProps{
					Title: kitex.Text("Card Title"),
				}),
				CardContent(CardContentProps{
					Children: []kitex.Node{kitex.Text("Card body content")},
				}),
				CardActions(CardActionsProps{
					Children: []kitex.Node{
						Button(ButtonProps{Children: []kitex.Node{kitex.Text("OK")}}),
					},
				}),
			},
		})
		if node == nil {
			t.Fatal("Card returned nil node")
		}
	})

	t.Run("OutOfOrder", func(t *testing.T) {
		// Even if children are passed out of order, they should be organized correctly.
		node := Card(CardProps{
			Children: []kitex.Node{
				CardActions(CardActionsProps{
					Children: []kitex.Node{
						Button(ButtonProps{Children: []kitex.Node{kitex.Text("Action")}}),
					},
				}),
				CardHeader(CardHeaderProps{
					Title: kitex.Text("Header"),
				}),
				CardContent(CardContentProps{
					Children: []kitex.Node{kitex.Text("Content")},
				}),
			},
		})
		if node == nil {
			t.Fatal("Out of order Card returned nil node")
		}
	})

	t.Run("HeaderDetails", func(t *testing.T) {
		node := Card(CardProps{
			Children: []kitex.Node{
				CardHeader(CardHeaderProps{
					Avatar:    kitex.Text("(A)"),
					Title:     kitex.Text("Title"),
					Subheader: kitex.Text("Subheader"),
					Action:    kitex.Text("[X]"),
				}),
			},
		})
		if node == nil {
			t.Fatal("Card with full header returned nil node")
		}
	})

	t.Run("Variants", func(t *testing.T) {
		variants := []CardVariant{CardDefault, CardOutlined}
		for _, v := range variants {
			node := Card(CardProps{
				Variant: v,
				Children: []kitex.Node{
					CardContent(CardContentProps{
						Children: []kitex.Node{kitex.Text("Variant test")},
					}),
				},
			})
			if node == nil {
				t.Errorf("Card with variant %s returned nil node", v)
			}
		}
	})
}
