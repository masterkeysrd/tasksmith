package components

import (
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/highlight"
)

// InputProps defines the properties for the Input component.
type InputProps struct {
	// Group is the highlight group for theme-aware styling.
	Group highlight.Group
	// Name is the name of the input field.
	Name string
	// Value is the current value of the input.
	Value string
	// Placeholder is the text displayed when the input is empty.
	Placeholder string
	// Disabled indicates if the input is interactive.
	Disabled bool
	// OnChange is triggered when the value changes.
	OnChange func(string)
	// OnFocus is triggered when the input gains focus.
	OnFocus func()
	// OnBlur is triggered when the input loses focus.
	OnBlur func()
	// Style allows passing additional style overrides.
	Style style.Style
	// PlaceholderStyle allows passing additional style overrides for the placeholder.
	PlaceholderStyle style.Style
}

var (
	// InputBaseStyle is the base style for the input field.
	InputBaseStyle = style.S().
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter).
		Padding(0, 1).
		Width(style.Percent(100)).
		MinWidth(style.Cells(0))
)

// Input is an interactive component for entering text.
// It wraps the primitive kitex.Input and integrates with the highlight system.
var Input = kitex.FC("Input", func(props InputProps) kitex.Node {
	s := highlight.Use(props.Group)

	// Merge base style, highlight style, and explicit overrides.
	s = s.Merge(InputBaseStyle).Merge(props.Style)

	return kitex.Input(kitex.InputProps{
		Name:             props.Name,
		Value:            props.Value,
		Placeholder:      props.Placeholder,
		PlaceholderStyle: props.PlaceholderStyle,
		Disabled:         props.Disabled,
		Style:            s,
		OnChange: func(e event.Event) {
			if props.OnChange != nil {
				if ie, ok := e.(*event.InputEvent); ok {
					props.OnChange(ie.Value)
				} else if ce, ok := e.(*event.ChangeEvent); ok {
					props.OnChange(ce.Value)
				}
			}
		},
		OnFocus: func(e event.Event) {
			if props.OnFocus != nil {
				props.OnFocus()
			}
		},
		OnBlur: func(e event.Event) {
			if props.OnBlur != nil {
				props.OnBlur()
			}
		},
	})
})
