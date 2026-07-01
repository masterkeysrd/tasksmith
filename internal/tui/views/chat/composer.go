package chat

import (
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// ComposerProps defines the properties for the Composer component.
type ComposerProps struct {
	Value     string
	Disabled  bool
	IsInsert  bool
	Ref       kitex.Ref[dom.Element]
	OnChange  func(string)
	OnKeyDown func(event.Event)
	OnSubmit  func()
}

// Composer is a multiline composer component styled like a terminal UI box,
// matching the design mockup in mockup.tsx.
var Composer = kitex.FC("Composer", func(props ComposerProps) kitex.Node {
	isFocused, setIsFocused := kitex.UseState(false)
	t := theme.UseTheme()

	if t == nil {
		return kitex.Box(kitex.BoxProps{}, kitex.Text("No Theme"))
	}

	// Resolve the focus blue color from the palette/theme
	blueColor := t.Color.Surface.Info

	// Border color switches to blue when focused, otherwise comment color
	borderColor := t.Color.Text.Tertiary // comment: #565f89
	if isFocused() {
		borderColor = blueColor // blue: #7aa2f7
	}

	// Wrapper style with a full single border
	wrapperStyle := style.S().
		Width(style.Percent(100)).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignEnd).
		Padding(0, 1). // 1 cell padding inside
		Border(style.SingleBorder().Color(borderColor))

	// Text area style
	var textCol color.Color
	if props.Disabled {
		textCol = t.Color.Text.Tertiary
	} else {
		textCol = t.Color.Text.Secondary // fg_dark: #a9b1d6
	}

	textareaStyle := style.S().
		Flex(1, 1, style.Cells(0)).
		MinHeight(style.Cells(1)).
		MaxHeight(style.Cells(8)).
		Background(color.Transparent).
		Foreground(textCol).
		Border(false)

	// Placeholder style
	ps := style.S().Foreground(t.Color.Text.Tertiary)

	textareaDisabled := props.Disabled || !props.IsInsert

	textareaProps := kitex.TextAreaProps{
		Name:              "composer-textarea",
		Value:             props.Value,
		Placeholder:       "Message TaskSmith...",
		PlaceholderStyle:  ps,
		Disabled:          textareaDisabled,
		Style:             textareaStyle,
		Ref:               props.Ref,
		OnChange: func(e event.Event) {
			val := ""
			if ie, ok := e.(*event.ChangeEvent); ok {
				val = ie.Value
			} else if ie, ok := e.(*event.InputEvent); ok {
				val = ie.Value
			}
			if props.OnChange != nil {
				props.OnChange(val)
			}
		},
		OnFocus: func(e event.Event) {
			setIsFocused(true)
		},
		OnBlur: func(e event.Event) {
			setIsFocused(false)
			mode.Set(mode.Normal)
		},
		OnKeyDown: func(e event.Event) {
			ke, ok := e.(*event.KeyEvent)
			if !ok {
				return
			}

			if ke.Code == key.KeyEscape {
				e.PreventDefault()
				e.StopPropagation()
				if props.Ref != nil && props.Ref.Current != nil {
					props.Ref.Current.Blur()
				}
				mode.Set(mode.Normal)
				return
			}

			// Enter without modifiers submits
			if ke.Code == key.KeyEnter && (ke.Mod&key.ModShift) == 0 {
				e.PreventDefault()
				e.StopPropagation()
				if props.OnSubmit != nil {
					props.OnSubmit()
				}
				return
			}

			if props.OnKeyDown != nil {
				props.OnKeyDown(e)
			}
		},
	}

	// Send button style
	btnStyle := style.S().
		Padding(0, 1).
		Background(color.Transparent).
		Height(style.Cells(1))

	isSendDisabled := props.Disabled || strings.TrimSpace(props.Value) == ""

	if isSendDisabled {
		btnStyle = btnStyle.Foreground(t.Color.Text.Tertiary)
	} else {
		btnStyle = btnStyle.Foreground(t.Color.Text.Tertiary)
	}

	btnHoverStyle := style.S()
	if !isSendDisabled {
		btnHoverStyle = btnHoverStyle.
			Background(t.Color.Surface.BaseFocus).
			Foreground(blueColor)
	}

	wrapperProps := kitex.BoxProps{Style: wrapperStyle}
	if !props.Disabled && !props.IsInsert {
		wrapperProps.OnClick = func(e event.Event) {
			mode.Set(mode.Insert)
		}
	}

	return kitex.Box(wrapperProps,
		kitex.TextArea(textareaProps),
		components.Button(components.ButtonProps{
			Variant:    components.ButtonText,
			Disabled:   isSendDisabled,
			OnClick:    props.OnSubmit,
			Style:      btnStyle,
			HoverStyle: btnHoverStyle,
		}, icon.MoveUp),
	)
})
