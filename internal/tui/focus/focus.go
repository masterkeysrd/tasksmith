package focus

import (
	"strings"

	"github.com/masterkeysrd/kite/dom"
)

var document dom.Document

// TestFocusContext allows tests to mock the focused DOM context.
var TestFocusContext string

// SetDocument installs the active kite document for resolving focus context.
func SetDocument(doc dom.Document) {
	document = doc
}

// WalkContextChain traverses the DOM hierarchy from the focused element upward,
// executing the callback for each context. It stops if the callback returns false.
func WalkContextChain(cb func(ctx string) bool) {
	if TestFocusContext != "" {
		if !cb(TestFocusContext) {
			return
		}
		if strings.HasPrefix(TestFocusContext, "modal:") {
			_ = cb("modal")
		}
		return
	}
	if document == nil {
		return
	}
	el := document.CurrentFocus()
	for el != nil {
		if val, ok := el.Attribute("data-context"); ok {
			if !cb(val) {
				return
			}
			if strings.HasPrefix(val, "modal:") {
				if !cb("modal") {
					return
				}
			}
		}
		el = el.ParentElement()
	}
}
