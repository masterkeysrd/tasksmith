package components

import (
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
)

// ModalProps defines properties for the Modal component.
type ModalProps struct {
	// IsOpen controls the visibility of the modal dialog.
	IsOpen bool
	// Title is the kitex.Node containing the header title layout.
	Title kitex.Node
	// OnClose is triggered when the dialog is dismissed.
	OnClose func()
	// Style extends the base modal container paper card style.
	Style style.Style
	// HeaderActions contains custom elements injected next to the close button.
	HeaderActions kitex.Node
	// Children is the list of child elements displayed in the body.
	Children []kitex.Node
}

// Modal renders a standardized full-screen modal card overlay.
// It relies on Dialog's FocusScope for autofocus connection and registers
// keyboard hotkeys to close on Escape or 'q'.
var Modal = kitex.FCC("Modal", func(props ModalProps) kitex.Node {
	if !props.IsOpen {
		return nil
	}

	baseStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(80)).
		Height(style.Percent(80)).
		Padding(1).
		Overflow(style.OverflowHidden)

	baseStyle = baseStyle.Merge(props.Style)

	return kitex.Dialog(kitex.DialogProps{
		ZIndex: 100,
		OnKeyDown: func(e event.Event) {
			ke, ok := e.(*event.KeyEvent)
			if !ok {
				return
			}
			if ke.Code == key.KeyEscape || ke.Text == "q" {
				e.PreventDefault()
				e.StopPropagation()
				if props.OnClose != nil {
					props.OnClose()
				}
			}
		},
	},
		Paper(PaperProps{
			Color:   PaperBase,
			Variant: PaperOutlined,
			Style:   baseStyle,
		},
			// Header Row
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					JustifyContent(style.JustifyBetween).
					AlignItems(style.AlignCenter).
					PaddingBottom(1).
					BorderBottom(true, style.SingleBorder()),
			},
				props.Title,
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						Gap(1),
				},
					props.HeaderActions,
					Button(ButtonProps{
						Variant: ButtonText,
						Color:   ButtonBase,
						OnClick: props.OnClose,
					}, kitex.Text("Close [Esc/q]")),
				),
			),
			// Body Content
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Flex(1, 1, style.Cells(0)).
					MinHeight(style.Cells(0)).
					OverflowY(style.OverflowAuto).
					MarginTop(1),
			},
				props.Children...,
			),
		),
	)
})
