package autocomplete

import (
	"context"
	"strings"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/key"
)

// Controller manages the reactive state of an autocomplete session.
type Controller struct {
	GetQuery         func() string
	SetQuery         func(string)
	GetIsOpen        func() bool
	SetIsOpen        func(bool)
	GetSelectedIndex func() int
	SetSelectedIndex func(int)
	GetFiltered      func() []Item
	SetFiltered      func([]Item)

	Provider Provider
}

// NewController instantiates an Autocomplete state manager hook inside a Kitex functional component.
func NewController(provider Provider) *Controller {
	query, setQuery := kitex.UseState("")
	isOpen, setIsOpen := kitex.UseState(false)
	selectedIndex, setSelectedIndex := kitex.UseState(0)
	filtered, setFiltered := kitex.UseState([]Item{})

	// Reactive effect: Filter items when the query string changes
	kitex.UseEffect(func() {
		q := query()

		// Strip trigger characters and namespace prefixes before querying the provider
		strippedQuery := q
		if strings.HasPrefix(q, "@file:") {
			strippedQuery = strings.TrimPrefix(q, "@file:")
		} else if strings.HasPrefix(q, "@sym:") {
			strippedQuery = strings.TrimPrefix(q, "@sym:")
		} else if strings.HasPrefix(q, "@skill:") {
			strippedQuery = strings.TrimPrefix(q, "@skill:")
		} else if strings.HasPrefix(q, "@") {
			strippedQuery = strings.TrimPrefix(q, "@")
		} else if strings.HasPrefix(q, "/") {
			strippedQuery = strings.TrimPrefix(q, "/")
		}

		items, err := provider.Query(context.Background(), strippedQuery)
		if err == nil {
			setFiltered(items)
		} else {
			setFiltered([]Item{})
		}

		// Reset selection index to top on query change
		setSelectedIndex(0)
	}, []any{query()})

	return &Controller{
		GetQuery:         query,
		SetQuery:         setQuery,
		GetIsOpen:        isOpen,
		SetIsOpen:        setIsOpen,
		GetSelectedIndex: selectedIndex,
		SetSelectedIndex: setSelectedIndex,
		GetFiltered:      filtered,
		SetFiltered:      setFiltered,
		Provider:         provider,
	}
}

// CheckTrigger scans the text cursor position for a valid autocomplete trigger.
// Returns the active trigger word and true if matched.
func (c *Controller) CheckTrigger(text string, cursorOffset int) (string, bool) {
	if cursorOffset <= 0 || cursorOffset > len(text) {
		return "", false
	}

	start := FindTriggerStart(text, cursorOffset)
	if start == -1 {
		return "", false
	}

	word := text[start:cursorOffset]

	// Trigger autocomplete on '@' (context reference) or '/' (slash command)
	if len(word) > 0 && (strings.HasPrefix(word, "@") || strings.HasPrefix(word, "/")) {
		return word, true
	}

	return "", false
}

// ApplySelection splices the selected Item's value into the text, replacing the trigger query.
// Returns the modified text string and the new cursor index offset.
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
// Returns true if the key event was intercepted and processed.
func (c *Controller) InterceptKey(ke *event.KeyEvent) bool {
	if !c.GetIsOpen() {
		return false
	}

	items := c.GetFiltered()
	if len(items) == 0 {
		if ke.Code == key.KeyEscape {
			c.SetIsOpen(false)
			return true
		}
		return false
	}

	idx := c.GetSelectedIndex()

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
