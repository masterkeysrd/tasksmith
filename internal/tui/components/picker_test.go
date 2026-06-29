package components

import (
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

func TestFuzzyScore(t *testing.T) {
	tests := []struct {
		text    string
		pattern string
		matched bool
	}{
		{"Hello World", "hello", true},
		{"Hello World", "world", true},
		{"Hello World", "hllo", true},
		{"Hello World", "xyz", false},
		{"Hello World", "", true},
		{"a", "ab", false},
	}

	for _, tc := range tests {
		_, ok := fuzzyScore(tc.text, tc.pattern)
		if ok != tc.matched {
			t.Errorf("fuzzyScore(%q, %q) = %t; want %t", tc.text, tc.pattern, ok, tc.matched)
		}
	}
}

func TestFilterItems(t *testing.T) {
	items := []PickerItem{
		{ID: "1", Label: "Apple", Sublabel: "Fruit"},
		{ID: "2", Label: "Banana", Sublabel: "Fruit"},
		{ID: "3", Label: "Carrot", Sublabel: "Vegetable"},
		{ID: "4", Label: "Disabled Item", Disabled: true},
	}

	t.Run("Empty Query", func(t *testing.T) {
		res := filterItems(items, "")
		if len(res) != 3 { // Should filter out disabled items
			t.Errorf("Expected 3 items, got %d", len(res))
		}
	})

	t.Run("With Matching Query", func(t *testing.T) {
		res := filterItems(items, "app")
		if len(res) != 1 || res[0].Label != "Apple" {
			t.Errorf("Expected Apple, got %v", res)
		}
	})

	t.Run("No Matching Query", func(t *testing.T) {
		res := filterItems(items, "xyz")
		if len(res) != 0 {
			t.Errorf("Expected 0 items, got %d", len(res))
		}
	})
}

func TestFilterGroups(t *testing.T) {
	groups := []PickerGroup{
		{
			Name: "Fruits",
			Items: []PickerItem{
				{ID: "1", Label: "Apple"},
				{ID: "2", Label: "Banana"},
			},
		},
		{
			Name: "Vegetables",
			Items: []PickerItem{
				{ID: "3", Label: "Carrot"},
			},
		},
	}

	t.Run("Empty Query", func(t *testing.T) {
		res := filterGroups(groups, "")
		if len(res) != 2 {
			t.Errorf("Expected 2 groups, got %d", len(res))
		}
	})

	t.Run("Matching Query in One Group", func(t *testing.T) {
		res := filterGroups(groups, "ban")
		if len(res) != 1 || res[0].Name != "Fruits" || len(res[0].Items) != 1 {
			t.Errorf("Expected 1 group with 1 item, got %v", res)
		}
	})
}

func TestPickerComponent(t *testing.T) {
	thm := &theme.Scheme{}

	render := func(node kitex.Node) kitex.Node {
		return theme.Provider(theme.Props{Theme: thm}, node)
	}

	t.Run("Not Open", func(t *testing.T) {
		node := Picker(PickerProps{
			IsOpen: false,
		})
		if node == nil {
			t.Fatal("Picker returned nil component element node when IsOpen=false")
		}
	})

	t.Run("Open Basic Items", func(t *testing.T) {
		node := render(Picker(PickerProps{
			IsOpen:      true,
			Title:       "Select Option",
			Placeholder: "Filter...",
			Items: []PickerItem{
				{ID: "1", Label: "Item 1"},
				{ID: "2", Label: "Item 2"},
			},
		}))
		if node == nil {
			t.Fatal("Picker returned nil node when open")
		}
	})

	t.Run("Open Groups & Preview", func(t *testing.T) {
		node := render(Picker(PickerProps{
			IsOpen: true,
			Title:  "Select with Preview",
			Groups: []PickerGroup{
				{
					Name: "Group 1",
					Items: []PickerItem{
						{ID: "1", Label: "Item 1"},
					},
				},
			},
			PreviewWidth: 20,
			RenderPreview: func(item PickerItem) kitex.Node {
				return kitex.Text("Preview of " + item.Label)
			},
			Actions: []PickerAction{
				{Label: "Delete", Key: "d"},
			},
		}))
		if node == nil {
			t.Fatal("Picker with preview and actions returned nil node")
		}
	})
}
