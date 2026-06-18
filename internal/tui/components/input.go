package components

import (
	"image/color"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// InputVariant defines visual styles for the Input component.
type InputVariant string

const (
	// InputOutline is an input with a full border.
	InputOutline InputVariant = "outline"
	// InputSolid is an input with a background color and no border (or a subtle one on focus).
	InputSolid InputVariant = "solid"
	// InputUnderline is an input with only a bottom border.
	InputUnderline InputVariant = "underline"
)

// InputColor defines color variants for the Input component.
type InputColor string

const (
	InputPrimary   InputColor = "primary"
	InputSecondary InputColor = "secondary"
	InputTertiary  InputColor = "tertiary"
	InputSuccess   InputColor = "success"
	InputInfo      InputColor = "info"
	InputError     InputColor = "error"
)

// InputProps defines the properties for the Input component.
type InputProps struct {
	// Name is the name of the input field.
	Name string
	// Value is the current value of the input.
	Value string
	// Placeholder is the text displayed when the input is empty.
	Placeholder string
	// Disabled indicates if the input is interactive.
	Disabled bool
	// Variant specifies the visual style variant.
	Variant InputVariant
	// Color specifies the color theme.
	Color InputColor
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
		AlignItems(style.AlignCenter).
		PaddingHorizontal(1).
		Width(style.Percent(100)).
		MinWidth(style.Cells(0))
)

// Input is an interactive component for entering text.
// It wraps the primitive kitex.Input and integrates with the theme system.
var Input = kitex.FC("Input", func(props InputProps) kitex.Node {
	isFocused, setIsFocused := kitex.UseState(false)
	isHovered, setIsHovered := kitex.UseState(false)

	t := theme.UseTheme()

	var s style.Style
	ps := props.PlaceholderStyle

	if t != nil {
		var baseColor, focusColor, hoverColor, disabledColor color.Color
		switch props.Color {
		case InputSecondary:
			baseColor = t.Color.Surface.Secondary
			focusColor = t.Color.Surface.SecondaryFocus
			hoverColor = t.Color.Surface.SecondaryHover
			disabledColor = t.Color.Surface.SecondaryDisabled
		case InputTertiary:
			baseColor = t.Color.Surface.Tertiary
			focusColor = t.Color.Surface.TertiaryFocus
			hoverColor = t.Color.Surface.TertiaryHover
			disabledColor = t.Color.Surface.TertiaryDisabled
		case InputSuccess:
			baseColor = t.Color.Surface.Success
			focusColor = t.Color.Surface.SuccessFocus
			hoverColor = t.Color.Surface.SuccessHover
			disabledColor = t.Color.Surface.SuccessDisabled
		case InputInfo:
			baseColor = t.Color.Surface.Info
			focusColor = t.Color.Surface.InfoFocus
			hoverColor = t.Color.Surface.InfoHover
			disabledColor = t.Color.Surface.InfoDisabled
		case InputError:
			baseColor = t.Color.Surface.Error
			focusColor = t.Color.Surface.ErrorFocus
			hoverColor = t.Color.Surface.ErrorHover
			disabledColor = t.Color.Surface.ErrorDisabled
		default: // InputPrimary or empty
			baseColor = t.Color.Surface.Primary
			focusColor = t.Color.Surface.PrimaryFocus
			hoverColor = t.Color.Surface.PrimaryHover
			disabledColor = t.Color.Surface.PrimaryDisabled
		}

		// Foreground text color
		var textCol color.Color
		if props.Disabled {
			textCol = t.Color.Text.Tertiary
		} else if props.Color != "" {
			textCol = baseColor
		} else {
			textCol = t.Color.Text.Primary
		}

		// Background color
		var bgCol color.Color
		if props.Disabled {
			bgCol = t.Color.Surface.BaseDisabled
		} else if props.Variant == InputSolid {
			if isFocused() {
				bgCol = t.Color.Surface.BaseHover
			} else if isHovered() {
				bgCol = t.Color.Surface.BaseHover
			} else {
				bgCol = t.Color.Surface.BaseDisabled
			}
		} else {
			bgCol = t.Color.Surface.Base
		}

		// Border style
		var border style.Border
		switch props.Variant {
		case InputSolid:
			border = style.Border{}
		case InputUnderline:
			var borderCol color.Color
			if props.Disabled {
				borderCol = disabledColor
			} else if isFocused() {
				borderCol = focusColor
			} else if isHovered() {
				borderCol = hoverColor
			} else {
				borderCol = t.Color.Border.Primary
			}
			border = style.SingleBorder().Top(false).Left(false).Right(false).Color(borderCol)
		default: // InputOutline or empty
			var borderCol color.Color
			if props.Disabled {
				borderCol = disabledColor
			} else if isFocused() {
				borderCol = focusColor
			} else if isHovered() {
				borderCol = hoverColor
			} else {
				borderCol = t.Color.Border.Primary
			}
			border = style.SingleBorder().Color(borderCol)
		}

		s = InputBaseStyle.
			Background(bgCol).
			Foreground(textCol)

		if props.Variant == InputSolid {
			s = s.Border(false)
		} else if border != (style.Border{}) {
			s = s.Border(border)
		}

		if !ps.ForegroundOpt().IsSet() {
			ps = ps.Foreground(t.Color.Text.Secondary)
		}
	} else {
		s = InputBaseStyle
	}

	s = s.Merge(props.Style)

	return kitex.Input(kitex.InputProps{
		Name:             props.Name,
		Value:            props.Value,
		Placeholder:      props.Placeholder,
		PlaceholderStyle: ps,
		Disabled:         props.Disabled,
		Style:            s,
		OnMouseEnter: func(e event.Event) {
			setIsHovered(true)
		},
		OnMouseLeave: func(e event.Event) {
			setIsHovered(false)
		},
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
			setIsFocused(true)
			if props.OnFocus != nil {
				props.OnFocus()
			}
		},
		OnBlur: func(e event.Event) {
			setIsFocused(false)
			if props.OnBlur != nil {
				props.OnBlur()
			}
		},
	})
})
