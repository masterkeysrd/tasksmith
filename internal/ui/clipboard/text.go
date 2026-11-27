package clipboard

import (
	"strings"

	"github.com/masterkeysrd/kite/dom"
)

// WriteText publishes text to doc's system clipboard and reports whether the
// write was dispatched.
func WriteText(doc dom.Document, text string) bool {
	if doc == nil || strings.TrimSpace(text) == "" {
		return false
	}
	cb := doc.Clipboard()
	if cb == nil {
		return false
	}
	cb.WriteText(text)
	return true
}
