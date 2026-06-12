package components

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/highlight"
)

// PaperVariant defines visual styles for the Paper component.
type PaperVariant string

const (
	// PaperDefault is a standard container with no extra decoration.
	PaperDefault PaperVariant = "default"
	// PaperOutlined is a container with a single-line border.
	PaperOutlined PaperVariant = "outlined"
)

var (
	// PaperOutlinedStyle is the base style for the outlined variant
	// of the Paper component.
	PaperOutlinedStyle = style.S().
		Border(true, style.BorderSingle)
)

// PaperProps defines the properties for the Paper component.
type PaperProps struct {
	// Group is the highlight group to use for theme-aware styling.
	Group highlight.Group
	// Variant specifies the visual variant of the paper.
	Variant PaperVariant
	// Style allows passing additional style overrides (e.g., padding, margin).
	Style style.Style
	// Children is the list of child nodes to render inside the paper.
	Children []kitex.Node
}

// Paper is a base layout component that provides a styled container.
// It integrates with the highlight system to react to theme changes and
// supports variants like outlined for common UI patterns.
var Paper = kitex.FCC("Paper", func(props PaperProps) kitex.Node {
	s := highlight.Use(props.Group)

	// Apply variant-specific styles
	switch props.Variant {
	case PaperOutlined:
		s = s.Merge(PaperOutlinedStyle)
	}

	// Merge with explicit style overrides (layout, colors, etc.)
	s = s.Merge(props.Style)

	return kitex.Box(kitex.BoxProps{
		Style: s,
	}, props.Children...)
})
