package autocomplete

import (
	"testing"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/key"
)

func TestControllerParse(t *testing.T) {
	c := New(Config{
		Triggers: map[string][]string{
			"@": {"file", "symbol", "skill"},
			"/": {"command"},
		},
		Prefixes: map[string]string{
			"@file:":  "file",
			"@sym:":   "symbol",
			"@skill:": "skill",
		},
	})

	tests := []struct {
		query        string
		wantStripped string
		wantSources  []string
		wantMatched  bool
	}{
		{"@", "", []string{"file", "symbol", "skill"}, true},
		{"@file:", "", []string{"file"}, true},
		{"@file:main", "main", []string{"file"}, true},
		{"@sym:", "", []string{"symbol"}, true},
		{"@sym:Main", "Main", []string{"symbol"}, true},
		{"@skill:", "", []string{"skill"}, true},
		{"@skill:agent", "agent", []string{"skill"}, true},
		{"/co", "co", []string{"command"}, true},
		{"hello", "", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			stripped, sources, matched := c.Parse(tt.query)
			if matched != tt.wantMatched {
				t.Fatalf("Parse(%q) matched = %t, want %t", tt.query, matched, tt.wantMatched)
			}
			if !matched {
				return
			}
			if stripped != tt.wantStripped {
				t.Errorf("Parse(%q) strippedQuery = %q, want %q", tt.query, stripped, tt.wantStripped)
			}
			if len(sources) != len(tt.wantSources) {
				t.Errorf("Parse(%q) sources count = %d, want %d", tt.query, len(sources), len(tt.wantSources))
				return
			}
			for i, s := range sources {
				if s != tt.wantSources[i] {
					t.Errorf("Parse(%q) sources[%d] = %q, want %q", tt.query, i, s, tt.wantSources[i])
				}
			}
		})
	}
}

func TestControllerCycleInline(t *testing.T) {
	c := New(Config{
		Triggers: map[string][]string{
			"": {"command"},
		},
		CycleInline: true,
	})

	var updatedValue string
	onChange := func(v string) {
		updatedValue = v
	}

	eventDown := &event.KeyEvent{}
	eventDown.Code = key.KeyDown

	// If menu is closed, pressing Down Arrow should not be handled
	if handled := c.HandleOnKeyDown(eventDown, "th", onChange); handled {
		t.Fatalf("Expected event to not be handled when menu is closed")
	}

	// Pressing Tab when closed should open the menu
	eventTab := &event.KeyEvent{}
	eventTab.Code = key.KeyTab
	if handled := c.HandleOnKeyDown(eventTab, "th", onChange); !handled {
		t.Fatalf("Expected Tab to be handled and open the menu when closed")
	}

	state := c.store.Get()
	if !state.IsOpen {
		t.Fatalf("Expected menu to be open after Tab on closed menu")
	}

	c.SetItems([]Item{
		{Label: "theme", InsertValue: "theme"},
		{Label: "thinking", InsertValue: "thinking"},
	})

	handled := c.HandleOnKeyDown(eventDown, "th", onChange)
	if !handled {
		t.Fatalf("Expected event to be handled")
	}

	state = c.store.Get()
	if state.SelectedIndex != 1 {
		t.Errorf("Expected SelectedIndex to be 1, got %d", state.SelectedIndex)
	}
	if updatedValue != "thinking" {
		t.Errorf("Expected updated value to be 'thinking', got %q", updatedValue)
	}

	handled = c.HandleOnKeyDown(eventDown, "thinking", onChange)
	if !handled {
		t.Fatalf("Expected event to be handled")
	}

	state = c.store.Get()
	if state.SelectedIndex != 0 {
		t.Errorf("Expected SelectedIndex to be 0, got %d", state.SelectedIndex)
	}
	if updatedValue != "theme" {
		t.Errorf("Expected updated value to be 'theme', got %q", updatedValue)
	}
}

func TestControllerParseEmptyTrigger(t *testing.T) {
	c := New(Config{
		Triggers: map[string][]string{
			"": {"command"},
		},
	})

	stripped, sources, matched := c.Parse("")
	if !matched {
		t.Fatalf("Expected empty query to match when empty trigger is registered")
	}
	if len(sources) != 1 || sources[0] != "command" {
		t.Errorf("Expected sources to be ['command'], got %v", sources)
	}
	if stripped != "" {
		t.Errorf("Expected stripped query to be empty, got %q", stripped)
	}
}
