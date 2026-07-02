package autocomplete

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/kite/key"
)

// State represents the reactive data of an autocomplete session.
type State struct {
	Query         string
	IsOpen        bool
	SelectedIndex int
	FilteredItems []Item
}

// Config defines the configurable trigger rules for the autocomplete controller.
type Config struct {
	Triggers map[string][]string // e.g. "@" -> ["file", "lsp", "skill"]
	Prefixes map[string]string   // e.g. "@file:" -> "file"
}

// Controller is a pure, framework-agnostic autocomplete state machine.
type Controller struct {
	store    *kites.Store[State]
	triggers map[string][]string
	prefixes map[string]string
}

// New instantiates a new Autocomplete controller.
func New(cfg Config) *Controller {
	return &Controller{
		store: kites.Create(State{
			FilteredItems: []Item{},
		}),
		triggers: cfg.Triggers,
		prefixes: cfg.Prefixes,
	}
}

// Use registers the active functional component to re-render when the state changes.
func (c *Controller) Use() State {
	_ = kites.Use(c.store, func(s State) string {
		// Re-render only when query, open status, selection index, or item count changes
		return fmt.Sprintf("%s-%t-%d-%d", s.Query, s.IsOpen, s.SelectedIndex, len(s.FilteredItems))
	})
	return c.store.Get()
}

// SetQuery updates the search query.
func (c *Controller) SetQuery(q string) {
	c.store.Set(func(s State) State {
		s.Query = q
		s.SelectedIndex = 0
		return s
	})
}

// SetIsOpen toggles the dropdown visibility.
func (c *Controller) SetIsOpen(open bool) {
	c.store.Set(func(s State) State {
		s.IsOpen = open
		if !open {
			s.FilteredItems = []Item{}
		}
		return s
	})
}

// SetItems updates the active completions list.
func (c *Controller) SetItems(items []Item) {
	c.store.Set(func(s State) State {
		if items == nil {
			s.FilteredItems = []Item{}
		} else {
			s.FilteredItems = items
		}
		if s.SelectedIndex >= len(s.FilteredItems) {
			s.SelectedIndex = 0
		}
		return s
	})
}

// SetSelectedIndex updates the active selection row.
func (c *Controller) SetSelectedIndex(idx int) {
	c.store.Set(func(s State) State {
		s.SelectedIndex = idx
		return s
	})
}

// Parse inspects the input query text to determine trigger match, prefix stripping, and target sources.
func (c *Controller) Parse(q string) (strippedQuery string, sources []string, matched bool) {
	if q == "" {
		return "", nil, false
	}

	// 1. Check for complete, specific prefixes (e.g. "@file:main")
	for pref, src := range c.prefixes {
		if strings.HasPrefix(q, pref) {
			return strings.TrimPrefix(q, pref), []string{src}, true
		}
	}

	// 2. Check for general triggers (e.g. "@" or "/")
	for trig, srcs := range c.triggers {
		if strings.HasPrefix(q, trig) {
			return strings.TrimPrefix(q, trig), srcs, true
		}
	}

	return "", nil, false
}

// CheckTrigger scans the text cursor position for a valid trigger character.
func (c *Controller) CheckTrigger(text string, cursorOffset int) (string, bool) {
	if cursorOffset <= 0 || cursorOffset > len(text) {
		return "", false
	}

	start := FindTriggerStart(text, cursorOffset)
	if start == -1 {
		return "", false
	}

	word := text[start:cursorOffset]

	// Match trigger if it starts with any registered trigger trigger prefix
	for trig := range c.triggers {
		if strings.HasPrefix(word, trig) {
			return word, true
		}
	}

	return "", false
}

// ApplySelection splices the selected Item's value into the text, replacing the trigger query.
func (c *Controller) ApplySelection(text string, cursorOffset int, item Item) (string, int) {
	start := FindTriggerStart(text, cursorOffset)
	if start == -1 {
		return text, cursorOffset
	}

	before := text[:start]
	after := text[cursorOffset:]
	insert := item.InsertValue

	newText := before + insert + after
	newCursor := start + len(insert)
	return newText, newCursor
}

// InterceptKey handles keyboard navigation (up/down/esc) when the dropdown menu is open.
func (c *Controller) InterceptKey(ke *event.KeyEvent) bool {
	state := c.store.Get()
	if !state.IsOpen {
		return false
	}

	items := state.FilteredItems
	if len(items) == 0 {
		if ke.Code == key.KeyEscape {
			c.SetIsOpen(false)
			return true
		}
		return false
	}

	idx := state.SelectedIndex

	switch {
	case ke.Code == key.KeyEscape:
		c.SetIsOpen(false)
		return true

	case ke.Code == key.KeyDown || (ke.Text == "n" && (ke.Mod&key.ModCtrl) != 0):
		c.SetSelectedIndex((idx + 1) % len(items))
		return true

	case ke.Code == key.KeyUp || (ke.Text == "p" && (ke.Mod&key.ModCtrl) != 0):
		next := idx - 1
		if next < 0 {
			next = len(items) - 1
		}
		c.SetSelectedIndex(next)
		return true
	}

	return false
}

// FindTriggerStart scans backward from the cursor offset to find the start of the current word.
func FindTriggerStart(text string, cursorOffset int) int {
	if cursorOffset <= 0 || cursorOffset > len(text) {
		return 0
	}
	for i := cursorOffset - 1; i >= 0; i-- {
		r := text[i]
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return i + 1
		}
	}
	return 0
}
