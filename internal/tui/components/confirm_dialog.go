package components

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
)

// ConfirmDialogProps defines the properties for the ConfirmDialog component.
type ConfirmDialogProps struct {
	// Message is the confirmation question displayed to the user.
	Message string
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
}

// ConfirmDialog renders a centered full-screen dialog asking the user to confirm or cancel an action.
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

	return kitex.Dialog(kitex.DialogProps{ZIndex: 100},
		Paper(PaperProps{
			Color:   PaperBase,
			Variant: PaperOutlined,
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Gap(1).
				Padding(1).
				MinWidth(style.Cells(40)),
		},
			kitex.If(props.Message != "", func() kitex.Node {
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
