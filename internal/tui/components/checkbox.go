package components

import (
	"image/color"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// CheckboxColor defines color variants for the Checkbox component.
type CheckboxColor string

const (
	CheckboxPrimary   CheckboxColor = "primary"
	CheckboxSecondary CheckboxColor = "secondary"
	CheckboxTertiary  CheckboxColor = "tertiary"
	CheckboxSuccess   CheckboxColor = "success"
	CheckboxInfo      CheckboxColor = "info"
	CheckboxError     CheckboxColor = "error"
)

// CheckboxProps defines the properties for the Checkbox component.
type CheckboxProps struct {
	// Color specifies the color variant of the checkbox when checked.
	Color CheckboxColor
	// Label is the text or node displayed next to the checkbox.
	Label kitex.Node
	// Checked indicates if the checkbox is selected.
	Checked bool
	// Disabled indicates if the checkbox is interactive.
	Disabled bool
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
	checkedState, setCheckedState := kitex.UseState(props.Checked)

	// Keep local state in sync with props.Checked
	kitex.UseEffect(func() {
		setCheckedState(props.Checked)
	}, []any{props.Checked})

	t := theme.UseTheme()

	if t == nil {
		handleChange := func(e event.Event) {
			if !props.Disabled {
				nextVal := !checkedState()
				setCheckedState(nextVal)
				if props.OnChange != nil {
					props.OnChange(nextVal)
				}
			}
		}

		return kitex.Box(kitex.BoxProps{
			Style:   CheckboxBaseStyle.Merge(props.Style),
			OnClick: handleChange,
		},
			kitex.Checkbox(kitex.CheckboxProps{
				Checked:  checkedState(),
				Disabled: props.Disabled,
				OnClick: func(e event.Event) {
					e.StopPropagation()
				},
				OnChange: func(e event.Event) {
					if !props.Disabled {
						if ce, ok := e.(*event.ChangeEvent); ok {
							val := ce.Value == "true"
							if val != checkedState() {
								setCheckedState(val)
								if props.OnChange != nil {
									props.OnChange(val)
								}
							}
						}
					}
				},
			}),
			kitex.If(props.Label != nil, func() kitex.Node {
				return kitex.Box(kitex.BoxProps{}, props.Label)
			}),
		)
	}

	// Resolve colors based on theme and state
	var base, hover color.Color
	switch props.Color {
	case CheckboxSecondary:
		base = t.Color.Surface.Secondary
		hover = t.Color.Surface.SecondaryHover
	case CheckboxTertiary:
		base = t.Color.Surface.Tertiary
		hover = t.Color.Surface.TertiaryHover
	case CheckboxSuccess:
		base = t.Color.Surface.Success
		hover = t.Color.Surface.SuccessHover
	case CheckboxInfo:
		base = t.Color.Surface.Info
		hover = t.Color.Surface.InfoHover
	case CheckboxError:
		base = t.Color.Surface.Error
		hover = t.Color.Surface.ErrorHover
	default:
		base = t.Color.Surface.Primary
		hover = t.Color.Surface.PrimaryHover
	}

	// Determine indicator color
	var indicatorColor color.Color
	if props.Disabled {
		indicatorColor = t.Color.Surface.BaseDisabled
	} else if checkedState() {
		if isHovered() && hover != nil {
			indicatorColor = hover
		} else {
			indicatorColor = base
		}
	} else {
		if isHovered() && hover != nil {
			indicatorColor = hover
		} else {
			indicatorColor = t.Color.Border.Primary
		}
	}

	// Determine label color
	var labelColor color.Color
	if props.Disabled {
		labelColor = t.Color.Text.Tertiary
	} else if isHovered() {
		labelColor = t.Color.Text.Primary
	} else if checkedState() {
		labelColor = t.Color.Text.Primary
	} else {
		labelColor = t.Color.Text.Tertiary
	}

	containerStyle := CheckboxBaseStyle.Merge(props.Style)
	indicatorStyle := style.S().Foreground(indicatorColor)
	labelStyle := style.S().Foreground(labelColor)

	handleChange := func(e event.Event) {
		if !props.Disabled {
			nextVal := !checkedState()
			setCheckedState(nextVal)
			if props.OnChange != nil {
				props.OnChange(nextVal)
			}
		}
	}

	return kitex.Box(kitex.BoxProps{
		Style:   containerStyle,
		OnClick: handleChange,
		OnMouseEnter: func(e event.Event) {
			setIsHovered(true)
		},
		OnMouseLeave: func(e event.Event) {
			setIsHovered(false)
		},
	},
		kitex.Checkbox(kitex.CheckboxProps{
			Checked:  checkedState(),
			Disabled: props.Disabled,
			Style:    indicatorStyle,
			OnClick: func(e event.Event) {
				e.StopPropagation()
			},
			OnChange: func(e event.Event) {
				if !props.Disabled {
					if ce, ok := e.(*event.ChangeEvent); ok {
						val := ce.Value == "true"
						if val != checkedState() {
							setCheckedState(val)
							if props.OnChange != nil {
								props.OnChange(val)
							}
						}
					}
				}
			},
		}),
		kitex.If(props.Label != nil, func() kitex.Node {
			return kitex.Box(kitex.BoxProps{
				Style: labelStyle,
			}, props.Label)
		}),
	)
})
