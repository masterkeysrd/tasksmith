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
	// ConfirmColor is the color of the confirm button.
	ConfirmColor ButtonColor
	// OnConfirm is called when the user confirms the action.
	OnConfirm func()
	// OnCancel is called when the user cancels.
	OnCancel func()
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

	return kitex.Dialog(kitex.DialogProps{ZIndex: 100},
		Paper(PaperProps{
			Color:   PaperBase,
			Variant: PaperOutlined,
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Gap(1).
				Padding(2).
				MinWidth(style.Cells(36)),
		},
			kitex.If(props.Message != "", func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						PaddingBottom(1),
				}, kitex.Text(props.Message))
			}),
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					AlignItems(style.AlignCenter).
					Gap(1),
			},
				Button(ButtonProps{
					Variant: ButtonSolid,
					Color:   confirmColor,
					Style: style.S().
						Flex(1).
						JustifyContent(style.JustifyCenter).
						TextAlign(style.TextAlignCenter).
						AlignItems(style.AlignCenter).
						PaddingVertical(1),
					OnClick: props.OnConfirm,
				}, kitex.Text(confirmLabel)),
				Button(ButtonProps{
					Variant: ButtonText,
					Color:   ButtonBase,
					Style: style.S().
						Flex(1).
						JustifyContent(style.JustifyCenter).
						AlignItems(style.AlignCenter).
						TextAlign(style.TextAlignCenter).
						PaddingVertical(1),
					OnClick: props.OnCancel,
				}, kitex.Text(cancelLabel)),
			),
		),
	)
})
