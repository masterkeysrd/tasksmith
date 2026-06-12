package components

import (
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
)

func TestTabs(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		node := Tabs(TabsProps{
			DefaultValue: "tab1",
		},
			Tab(TabProps{Value: "tab1"}, kitex.Text("Tab 1")),
			Tab(TabProps{Value: "tab2"}, kitex.Text("Tab 2")),
			TabPanel(TabPanelProps{
				Value: "tab1",
			}, kitex.Text("Panel 1 content")),
			TabPanel(TabPanelProps{
				Value: "tab2",
			}, kitex.Text("Panel 2 content")),
		)
		if node == nil {
			t.Fatal("Tabs returned nil node")
		}
	})

	t.Run("Controlled", func(t *testing.T) {
		value := "tab2"
		node := Tabs(TabsProps{
			Value: value,
		},
			Tab(TabProps{Value: "tab1"}, kitex.Text("Tab 1")),
			Tab(TabProps{Value: "tab2"}, kitex.Text("Tab 2")),
			TabPanel(TabPanelProps{
				Value: "tab1",
			}, kitex.Text("Panel 1 content")),
			TabPanel(TabPanelProps{
				Value: "tab2",
			}, kitex.Text("Panel 2 content")),
		)
		if node == nil {
			t.Fatal("Controlled Tabs returned nil node")
		}
	})

	t.Run("OutOfOrder", func(t *testing.T) {
		node := Tabs(TabsProps{
			DefaultValue: 1,
		},
			TabPanel(TabPanelProps{
				Value: 1,
			}, kitex.Text("Content 1")),
			Tab(TabProps{Value: 1}, kitex.Text("Tab 1")),
		)
		if node == nil {
			t.Fatal("Out of order Tabs returned nil node")
		}
	})
}
