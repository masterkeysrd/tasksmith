package components

import (
	"image/color"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// SelectProps defines the properties for the Select component.
type SelectProps struct {
	Value         string
	OnValueChange func(string)
	Style         style.Style
	Disabled      bool
	Children      []kitex.Node
}

var SelectBaseStyle = style.S().
	Display(style.DisplayFlex).
	AlignItems(style.AlignCenter).
	PaddingHorizontal(1)

// Select is a custom VDOM wrapper around the native kitex.Select element,
// providing consistent theme styling.
var Select = kitex.FCC("Select", func(props SelectProps) kitex.Node {
	t := theme.UseTheme()

	isFocused, setIsFocused := kitex.UseState(false)
	isHovered, setIsHovered := kitex.UseState(false)

	var bgCol color.Color
	var textCol color.Color

	if t != nil {
		textCol = t.Color.Text.Primary
		if props.Disabled {
			bgCol = t.Color.Surface.BaseDisabled
			textCol = t.Color.Text.Tertiary
		} else {
			if isFocused() {
				bgCol = t.Color.Surface.BaseHover
			} else if isHovered() {
				bgCol = t.Color.Surface.BaseHover
			} else {
				bgCol = t.Color.Surface.BaseDisabled
			}
		}
	} else {
		bgCol = color.Transparent
		textCol = color.White
	}

	s := SelectBaseStyle.
		Background(bgCol).
		Foreground(textCol).
		Border(false).
		Merge(props.Style)

	selectProps := kitex.SelectProps{
		Value:    props.Value,
		Disabled: props.Disabled,
		Style:    s,
		OnValueChange: func(val string) {
			if props.OnValueChange != nil {
				props.OnValueChange(val)
			}
		},
		OnFocus: func(e event.Event) {
			setIsFocused(true)
		},
		OnBlur: func(e event.Event) {
			setIsFocused(false)
		},
		OnMouseEnter: func(e event.Event) {
			setIsHovered(true)
		},
		OnMouseLeave: func(e event.Event) {
			setIsHovered(false)
		},
	}

	return kitex.Select(selectProps, props.Children...)
})

// OptionProps defines the properties for the Option component.
type OptionProps struct {
	Text     string
	Value    string
	Disabled bool
}

// Option is a wrapper around kitex.Option.
func Option(props OptionProps) kitex.Node {
	return kitex.Option(kitex.OptionProps{
		Text:     props.Text,
		Value:    props.Value,
		Disabled: props.Disabled,
	})
}
