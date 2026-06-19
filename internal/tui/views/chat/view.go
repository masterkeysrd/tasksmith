package chat

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
)

// ViewProps defines the properties for the Chat view.
type ViewProps struct{}

// View is the main Chat view component.
var View = kitex.FC("ChatView", func(props ViewProps) kitex.Node {
	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Width(style.Percent(100)).
			Height(style.Percent(100)),
	})
})
