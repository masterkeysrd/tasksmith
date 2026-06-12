package components

import (
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/highlight"
)

// CheckboxGroups defines the highlight groups for different checkbox states.
type CheckboxGroups struct {
	Normal   highlight.Group
	Hover    highlight.Group
	Active   highlight.Group
	Disabled highlight.Group
}

// CheckboxProps defines the properties for the Checkbox component.
type CheckboxProps struct {
	// Group is the highlight group to use for the container.
	Group highlight.Group
	// Groups optionally provides specific highlight groups for different states.
	Groups CheckboxGroups
	// LabelGroup is the highlight group for the label.
	LabelGroup highlight.Group
	// Label is the text or node displayed next to the checkbox.
	Label kitex.Node
	// Checked indicates if the checkbox is selected.
	Checked bool
	// Disabled indicates if the checkbox is interactive.
	Disabled bool
	// Active indicates if the checkbox is in an active or selected state.
	Active bool
	// OnChange is triggered when the checked state changes.
	OnChange func(bool)
	// Style allows passing additional style overrides to the container.
	Style style.Style
}

var (
	// CheckboxBaseStyle is the base style for the checkbox container.
	CheckboxBaseStyle = style.S().
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter).
		Gap(1)
)

// Checkbox is an interactive component that allows toggling a boolean value.
// It combines a kitex.Checkbox with a label and handles click events on both.
var Checkbox = kitex.FC("Checkbox", func(props CheckboxProps) kitex.Node {
	isHovered, setIsHovered := kitex.UseState(false)

	// Consume highlight groups reactively.
	normalStyle := highlight.Use(props.Group)
	if !props.Groups.Normal.Empty() {
		normalStyle = highlight.Use(props.Groups.Normal)
	}
	hoverStyle := highlight.Use(props.Groups.Hover)
	activeStyle := highlight.Use(props.Groups.Active)
	disabledStyle := highlight.Use(props.Groups.Disabled)

	// Determine the container style based on current state.
	s := normalStyle
	if props.Disabled && !props.Groups.Disabled.Empty() {
		s = disabledStyle
	} else if props.Active && !props.Groups.Active.Empty() {
		s = activeStyle
	} else if isHovered() && !props.Groups.Hover.Empty() {
		s = hoverStyle
	}

	s = s.Merge(CheckboxBaseStyle).Merge(props.Style)

	// Resolve label style
	lStyle := highlight.Use(props.LabelGroup)

	handleChange := func(e event.Event) {
		if !props.Disabled && props.OnChange != nil {
			props.OnChange(!props.Checked)
		}
	}

	return kitex.Box(kitex.BoxProps{
		Style:   s,
		OnClick: handleChange,
		OnMouseEnter: func(e event.Event) {
			setIsHovered(true)
		},
		OnMouseLeave: func(e event.Event) {
			setIsHovered(false)
		},
	},
		kitex.Checkbox(kitex.CheckboxProps{
			Checked:  props.Checked,
			Disabled: props.Disabled,
			OnClick: func(e event.Event) {
				e.StopPropagation()
			},
			OnChange: func(e event.Event) {
				if props.OnChange != nil {
					props.OnChange(!props.Checked)
				}
			},
		}),
		kitex.If(props.Label != nil, func() kitex.Node {
			return kitex.Span(kitex.BoxProps{
				Style: lStyle,
			}, props.Label)
		}),
	)
})
