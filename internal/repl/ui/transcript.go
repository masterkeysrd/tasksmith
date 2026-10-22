package ui

import (
	"github.com/masterkeysrd/kite/extras/trail"
)

// TranscriptProps defines the properties for the transcript component.
type TranscriptProps struct {
	Items []trail.Node
}

// Transcript renders a slice of nodes permanently into terminal scrollback using trail.Static.
var Transcript = trail.FC("Transcript", func(props TranscriptProps) trail.Node {
	return trail.Static(props.Items, func(item trail.Node, _ int) trail.Node {
		return item
	})
})
