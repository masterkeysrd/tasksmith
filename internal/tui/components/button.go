package components

import (
	"image/color"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
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

	// Resolve surface colors based on component color
	var base, hover, pressed, focus, disabled color.Color
	switch props.Color {
	case ButtonSecondary:
		base = t.Color.Surface.Secondary
		hover = t.Color.Surface.SecondaryHover
		pressed = t.Color.Surface.SecondaryPressed
		focus = t.Color.Surface.SecondaryFocus
		disabled = t.Color.Surface.SecondaryDisabled
	case ButtonTertiary:
		base = t.Color.Surface.Tertiary
		hover = t.Color.Surface.TertiaryHover
		pressed = t.Color.Surface.TertiaryPressed
		focus = t.Color.Surface.TertiaryFocus
		disabled = t.Color.Surface.TertiaryDisabled
	case ButtonSuccess:
		base = t.Color.Surface.Success
		hover = t.Color.Surface.SuccessHover
		pressed = t.Color.Surface.SuccessPressed
		focus = t.Color.Surface.SuccessFocus
		disabled = t.Color.Surface.SuccessDisabled
	case ButtonInfo:
		base = t.Color.Surface.Info
		hover = t.Color.Surface.InfoHover
		pressed = t.Color.Surface.InfoPressed
		focus = t.Color.Surface.InfoFocus
		disabled = t.Color.Surface.InfoDisabled
	case ButtonError:
		base = t.Color.Surface.Error
		hover = t.Color.Surface.ErrorHover
		pressed = t.Color.Surface.ErrorPressed
		focus = t.Color.Surface.ErrorFocus
		disabled = t.Color.Surface.ErrorDisabled
	case ButtonBase:
		base = t.Color.Surface.BaseFocus
		hover = t.Color.Surface.BaseHover
		pressed = t.Color.Surface.BasePressed
		focus = t.Color.Surface.BaseFocus
		disabled = t.Color.Surface.BaseDisabled
	default:
		base = t.Color.Surface.Primary
		hover = t.Color.Surface.PrimaryHover
		pressed = t.Color.Surface.PrimaryPressed
		focus = t.Color.Surface.PrimaryFocus
		disabled = t.Color.Surface.PrimaryDisabled
	}

	// Determine active color based on state
	var currentColor color.Color
	if props.Disabled {
		currentColor = disabled
	} else if isHovered() {
		currentColor = hover
	} else if props.Active {
		if focus != nil {
			currentColor = focus
		} else {
			currentColor = pressed
		}
	} else {
		currentColor = base
	}

	// Build style based on variant
	s := ButtonBaseStyle
	switch props.Variant {
	case ButtonTonal:
		var bg, fg color.Color
		if props.Disabled {
			bg = disabled
			fg = t.Color.Text.Tertiary
		} else if props.Color == ButtonBase {
			if isHovered() {
				bg = t.Color.Surface.InfoFocus
				fg = t.Color.Surface.Primary
			} else {
				bg = t.Color.Surface.BaseFocus
				fg = t.Color.Text.Secondary
			}
		} else {
			if isHovered() {
				if props.Color == ButtonInfo {
					bg = t.Color.Surface.PrimaryPressed
				} else {
					bg = hover
				}
			} else {
				bg = focus
			}
			if props.Color == ButtonInfo {
				fg = t.Color.Surface.Primary
			} else {
				fg = base
			}
		}
		s = s.Background(bg).Foreground(fg)
	case ButtonSolid:
		var fg color.Color
		if props.Active && focus != nil {
			fg = base
		} else {
			fg = t.Color.Text.InversePrimary
		}
		s = s.Background(currentColor).Foreground(fg)
	case ButtonOutline:
		s = s.Border(style.SingleBorder().Color(currentColor)).Foreground(currentColor)
	case ButtonText:
		s = s.Foreground(currentColor)
	default: // Default to Solid
		s = s.Background(currentColor).Foreground(t.Color.Text.InversePrimary)
	}

	// Merge with explicit style overrides
	s = s.Merge(props.Style)
	if isHovered() {
		s = s.Merge(props.HoverStyle)
	}

	var content []kitex.Node
	if props.StartIcon != nil {
		content = append(content, props.StartIcon)
	}
	content = append(content, props.Children...)
	if props.EndIcon != nil {
		content = append(content, props.EndIcon)
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
	}, content...)
})
