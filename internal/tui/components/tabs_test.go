package components

import (
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
)

func TestTabs(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		node := Tabs(TabsProps{
			DefaultValue: "tab1",
			Children: []kitex.Node{
				Tab(TabProps{Value: "tab1", Label: kitex.Text("Tab 1")}),
				Tab(TabProps{Value: "tab2", Label: kitex.Text("Tab 2")}),
				TabPanel(TabPanelProps{
					Value:    "tab1",
					Children: []kitex.Node{kitex.Text("Panel 1 content")},
				}),
				TabPanel(TabPanelProps{
					Value:    "tab2",
					Children: []kitex.Node{kitex.Text("Panel 2 content")},
				}),
			},
		})
		if node == nil {
			t.Fatal("Tabs returned nil node")
		}
	})

	t.Run("Controlled", func(t *testing.T) {
		value := "tab2"
		node := Tabs(TabsProps{
			Value: value,
			Children: []kitex.Node{
				Tab(TabProps{Value: "tab1", Label: kitex.Text("Tab 1")}),
				Tab(TabProps{Value: "tab2", Label: kitex.Text("Tab 2")}),
				TabPanel(TabPanelProps{
					Value:    "tab1",
					Children: []kitex.Node{kitex.Text("Panel 1 content")},
				}),
				TabPanel(TabPanelProps{
					Value:    "tab2",
					Children: []kitex.Node{kitex.Text("Panel 2 content")},
				}),
			},
		})
		if node == nil {
			t.Fatal("Controlled Tabs returned nil node")
		}
	})

	t.Run("OutOfOrder", func(t *testing.T) {
		node := Tabs(TabsProps{
			DefaultValue: 1,
			Children: []kitex.Node{
				TabPanel(TabPanelProps{
					Value:    1,
					Children: []kitex.Node{kitex.Text("Content 1")},
				}),
				Tab(TabProps{Value: 1, Label: kitex.Text("Tab 1")}),
			},
		})
		if node == nil {
			t.Fatal("Out of order Tabs returned nil node")
		}
	})
}
