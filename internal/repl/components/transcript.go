package components

import "github.com/masterkeysrd/kite/extras/trail"

// TranscriptProps defines the properties for the Transcript component.
type TranscriptProps struct {
	// Items are finalized nodes in print order. Items may only be appended;
	// each is written to terminal scrollback exactly once.
	Items []trail.Node
}

// Transcript permanently prints finalized items, such as completed conversation
// messages, notices and command output, into terminal scrollback.
var Transcript = trail.FC("Transcript", func(props TranscriptProps) trail.Node {
	return trail.Static(props.Items, func(item trail.Node, _ int) trail.Node {
		return item
	})
})
