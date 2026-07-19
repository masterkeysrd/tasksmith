package components

import (
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
)

// ConfirmDialogProps defines the properties for the ConfirmDialog component.
type ConfirmDialogProps struct {
	// Title is an optional header text for the dialog.
	Title string
	// Message is the confirmation question displayed to the user.
	Message string
	// Content allows passing a rich custom kitex.Node instead of a simple Message string.
	Content kitex.Node
	// ConfirmLabel is the label for the confirm button (default: "Confirm").
	ConfirmLabel string
	// CancelLabel is the label for the cancel button (default: "Cancel").
	CancelLabel string
	// SecondaryLabel is the label for an optional secondary action button.
	SecondaryLabel string
	// ConfirmColor is the color of the confirm button.
	ConfirmColor ButtonColor
	// SecondaryColor is the color of the secondary action button.
	SecondaryColor ButtonColor
	// OnConfirm is called when the user confirms the action.
	OnConfirm func()
	// OnCancel is called when the user cancels.
	OnCancel func()
	// OnSecondary is called when the user triggers the secondary action.
	OnSecondary func()
	// OnKeyDown allows custom key interceptors to override default keys.
	OnKeyDown func(event.Event)
}

// ConfirmDialog renders a centered dialog asking the user to confirm or cancel an action.
var ConfirmDialog = kitex.FC("ConfirmDialog", func(props ConfirmDialogProps) kitex.Node {
	confirmLabel := props.ConfirmLabel
	if confirmLabel == "" {
		confirmLabel = "Confirm"
	}
	cancelLabel := props.CancelLabel
	if cancelLabel == "" {
		cancelLabel = "Cancel"
	}
	confirmColor := props.ConfirmColor
	if confirmColor == "" {
		confirmColor = ButtonError
	}
	secondaryColor := props.SecondaryColor
	if secondaryColor == "" {
		secondaryColor = ButtonBase
	}

	return kitex.Dialog(kitex.DialogProps{
		ZIndex: 100,
		OnKeyDown: func(e event.Event) {
			if props.OnKeyDown != nil {
				props.OnKeyDown(e)
				if e.DefaultPrevented() {
					return
				}
			}
			ke, ok := e.(*event.KeyEvent)
			if !ok {
				return
			}
			if ke.Code == key.KeyEnter || ke.Text == "\r" || ke.Text == "\n" {
				e.PreventDefault()
				e.StopPropagation()
				if props.OnConfirm != nil {
					props.OnConfirm()
				}
			} else if ke.Code == key.KeyEscape {
				e.PreventDefault()
				e.StopPropagation()
				if props.OnCancel != nil {
					props.OnCancel()
				}
			}
		},
	},
		Paper(PaperProps{
			Color:   PaperBase,
			Variant: PaperOutlined,
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Gap(1).
				Padding(1).
				MinWidth(style.Cells(50)).
				MaxWidth(style.Cells(75)),
		},
			kitex.If(props.Title != "", func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Bold(true).
						PaddingHorizontal(1).
						PaddingVertical(0),
				}, kitex.Text(props.Title))
			}),
			kitex.If(props.Content != nil, func() kitex.Node {
				return props.Content
			}),
			kitex.If(props.Content == nil && props.Message != "", func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						PaddingVertical(1).
						PaddingHorizontal(1),
				}, kitex.Text(props.Message))
			}),
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					JustifyContent(style.JustifyEnd).
					Gap(1),
			},
				kitex.If(props.SecondaryLabel != "", func() kitex.Node {
					return Button(ButtonProps{
						Variant: ButtonText,
						Color:   secondaryColor,
						OnClick: props.OnSecondary,
					}, kitex.Text(props.SecondaryLabel))
				}),
				Button(ButtonProps{
					Variant: ButtonText,
					Color:   confirmColor,
					OnClick: props.OnConfirm,
				}, kitex.Text(confirmLabel)),
				Button(ButtonProps{
					Variant: ButtonText,
					Color:   ButtonBase,
					OnClick: props.OnCancel,
				}, kitex.Text(cancelLabel)),
			),
		),
	)
})
