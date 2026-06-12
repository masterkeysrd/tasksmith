package components

import (
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/highlight"
)

// ButtonVariant defines visual styles for the Button component.
type ButtonVariant string

const (
	// ButtonOutline is a button with a border.
	ButtonOutline ButtonVariant = "outline"
)

var (
	// ButtonOutlineStyle is the base style for the outline variant.
	ButtonOutlineStyle = style.S().
				Border(true, style.BorderSingle)

	// ButtonBaseStyle is the base style for all buttons.
	ButtonBaseStyle = style.S().
			Display(style.DisplayFlex).
			AlignItems(style.AlignCenter).
			Gap(1)
)

// ButtonGroups defines the highlight groups for different button states.
type ButtonGroups struct {
	Normal   highlight.Group
	Hover    highlight.Group
	Active   highlight.Group
	Disabled highlight.Group
}

// ButtonProps defines the properties for the Button component.
type ButtonProps struct {
	// Group is the highlight group to use for theme-aware styling.
	Group highlight.Group
	// Groups optionally provides specific highlight groups for different states.
	Groups ButtonGroups
	// Variant specifies the visual variant of the button.
	Variant ButtonVariant
	// Disabled indicates if the button is interactive.
	Disabled bool
	// Active indicates if the button is in an active or selected state.
	Active bool
	// OnClick is the callback triggered when the button is clicked.
	OnClick func()
	// Style allows passing additional style overrides.
	Style style.Style
	// StartIcon is an optional icon to display before the text.
	StartIcon kitex.Node
	// EndIcon is an optional icon to display after the text.
	EndIcon kitex.Node
	// Children is the list of child nodes (usually just text).
	Children []kitex.Node
}

// Button is an interactive component that triggers an action.
// It integrates with the highlight system to react to theme changes and
// supports variants like outlined for common UI patterns.
var Button = kitex.FCC("Button", func(props ButtonProps) kitex.Node {
	isHovered, setIsHovered := kitex.UseState(false)

	// Consume highlight groups reactively.
	// We call these unconditionally to satisfy hook order requirements.
	normalStyle := highlight.Use(props.Group)
	if !props.Groups.Normal.Empty() {
		normalStyle = highlight.Use(props.Groups.Normal)
	}
	hoverStyle := highlight.Use(props.Groups.Hover)
	activeStyle := highlight.Use(props.Groups.Active)
	disabledStyle := highlight.Use(props.Groups.Disabled)

	// Determine the base style based on current state.
	s := normalStyle
	if props.Disabled && !props.Groups.Disabled.Empty() {
		s = disabledStyle
	} else if props.Active && !props.Groups.Active.Empty() {
		s = activeStyle
	} else if isHovered() && !props.Groups.Hover.Empty() {
		s = hoverStyle
	}

	// Apply base button layout
	s = s.Merge(ButtonBaseStyle)

	// Apply variant-specific styles
	switch props.Variant {
	case ButtonOutline:
		s = s.Merge(ButtonOutlineStyle)
	}

	// Merge with explicit style overrides (layout, colors, etc.)
	s = s.Merge(props.Style)

	// Compose children with icons.
	// We wrap the children in a Span to ensure the label is treated as a single unit
	// for layout (e.g., when applying gaps) and to allow for label-specific styling.
	var content []kitex.Node
	if props.StartIcon != nil {
		content = append(content, props.StartIcon)
	}
	if len(props.Children) > 0 {
		content = append(content, kitex.Span(kitex.BoxProps{}, props.Children...))
	}
	if props.EndIcon != nil {
		content = append(content, props.EndIcon)
	}

	return kitex.Button(kitex.ButtonProps{
		Style:    s,
		Disabled: props.Disabled,
		Active:   props.Active,
		OnClick: func(e event.Event) {
			if props.OnClick != nil && !props.Disabled {
				props.OnClick()
			}
		},
		OnMouseEnter: func(e event.Event) {
			setIsHovered(true)
		},
		OnMouseLeave: func(e event.Event) {
			setIsHovered(false)
		},
	}, content...)
})
