package components

import (
	"image/color"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// ButtonVariant defines visual styles for the Button component.
type ButtonVariant string

const (
	// ButtonText is a button with no background or border.
	ButtonText ButtonVariant = "text"
	// ButtonSolid is a button with a solid background.
	ButtonSolid ButtonVariant = "solid"
	// ButtonOutline is a button with a border.
	ButtonOutline ButtonVariant = "outline"
	// ButtonTonal is a button with a soft/tinted background (e.g. selected pills, theme button, enable all).
	ButtonTonal ButtonVariant = "tonal"
)

// ButtonColor defines color variants for the Button component.
type ButtonColor string

const (
	ButtonPrimary   ButtonColor = "primary"
	ButtonSecondary ButtonColor = "secondary"
	ButtonTertiary  ButtonColor = "tertiary"
	ButtonSuccess   ButtonColor = "success"
	ButtonInfo      ButtonColor = "info"
	ButtonError     ButtonColor = "error"
	ButtonBase      ButtonColor = "base"
)

var (
	// ButtonBaseStyle is the base style for all buttons.
	ButtonBaseStyle = style.S().
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter).
		Gap(1).
		Border(false).
		Padding(0, 1)
)

// ButtonProps defines the properties for the Button component.
type ButtonProps struct {
	// Key is an optional unique identifier for the component.
	Key string
	// Variant specifies the visual variant of the button.
	Variant ButtonVariant
	// Color specifies the color variant of the button.
	Color ButtonColor
	// Disabled indicates if the button is interactive.
	Disabled bool
	// Active indicates if the button is in an active or selected state.
	Active bool
	// OnClick is the callback triggered when the button is clicked.
	OnClick func()
	// Style allows passing additional style overrides.
	Style style.Style
	// HoverStyle allows passing additional style overrides for the hovered state.
	HoverStyle style.Style
	// ActiveStyle allows passing additional style overrides when the button is active.
	ActiveStyle style.Style
	// StartIcon is an optional icon to display before the text.
	StartIcon kitex.Node
	// EndIcon is an optional icon to display after the text.
	EndIcon kitex.Node
	// Children is the content of the button.
	Children []kitex.Node
}

// Button is an interactive component that triggers an action.
// It integrates with the theme system to react to theme changes.
var Button = kitex.FCC("Button", func(props ButtonProps) kitex.Node {
	isHovered, setIsHovered := kitex.UseState(false)
	isFocused, setIsFocused := kitex.UseState(false)
	t := theme.UseTheme()

	if t == nil {
		return kitex.Button(kitex.ButtonProps{
			Key:      props.Key,
			Disabled: props.Disabled,
			Active:   props.Active,
			OnClick: func(e event.Event) {
				if props.OnClick != nil && !props.Disabled {
					props.OnClick()
				}
			},
		}, props.Children...)
	}

	var textCol, bgCol, borderCol color.Color
	switch props.Color {
	case ButtonSecondary:
		textCol = t.Color.Text.Secondary
		bgCol = t.Color.Surface.Selected
		borderCol = t.Color.Border.Default
	case ButtonTertiary:
		textCol = t.Color.Text.Warning
		bgCol = t.Color.Tint.Warning
		borderCol = t.Color.Border.Warning
	case ButtonSuccess:
		textCol = t.Color.Text.Success
		bgCol = t.Color.Tint.Success
		borderCol = t.Color.Border.Success
	case ButtonInfo:
		textCol = t.Color.Text.Info
		bgCol = t.Color.Tint.Info
		borderCol = t.Color.Border.Info
	case ButtonError:
		textCol = t.Color.Text.Error
		bgCol = t.Color.Tint.Error
		borderCol = t.Color.Border.Error
	case ButtonBase:
		textCol = t.Color.Text.Primary
		bgCol = t.Color.Surface.Panel
		borderCol = t.Color.Border.Default
	default: // ButtonPrimary
		textCol = t.Color.Text.Accent
		bgCol = t.Color.Tint.Accent
		borderCol = t.Color.Border.Accent
	}

	if props.Disabled {
		textCol = t.Color.Text.Disabled
		bgCol = t.Color.Surface.Disabled
		borderCol = t.Color.Border.Subtle
	} else if isHovered() || isFocused() {
		borderCol = t.Color.Border.Focus
	}

	// Build style based on variant
	s := ButtonBaseStyle
	switch props.Variant {
	case ButtonTonal:
		s = s.Background(bgCol).Foreground(textCol)
	case ButtonSolid:
		s = s.Background(bgCol).Foreground(textCol)
	case ButtonOutline:
		s = s.Border(style.SingleBorder().Color(borderCol)).Foreground(textCol)
		if isHovered() || isFocused() {
			s = s.Background(t.Color.Surface.Hover)
		}
	case ButtonText:
		s = s.Foreground(textCol)
		if isHovered() || isFocused() {
			s = s.Background(t.Color.Surface.Hover)
		}
	default: // Default to Solid
		s = s.Background(bgCol).Foreground(textCol)
	}

	// Merge with explicit style overrides
	s = s.Merge(props.Style)
	if isHovered() || isFocused() {
		s = s.Merge(props.HoverStyle)
	}
	if props.Active {
		s = s.Merge(props.ActiveStyle)
	}

	var content []kitex.Node
	if props.StartIcon != nil {
		content = append(content, kitex.Box(kitex.BoxProps{}, props.StartIcon))
	}
	content = append(content, props.Children...)
	if props.EndIcon != nil {
		content = append(content, kitex.Box(kitex.BoxProps{}, props.EndIcon))
	}

	return kitex.Button(kitex.ButtonProps{
		Key:      props.Key,
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
		OnFocus: func(e event.Event) {
			setIsFocused(true)
		},
		OnBlur: func(e event.Event) {
			setIsFocused(false)
		},
	}, content...)
})
