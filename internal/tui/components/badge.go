package components

import (
	"image/color"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// ToolBadgeProps defines properties for the ToolBadge component.
type ToolBadgeProps struct {
	// Icon is the visual node (checkmark, error, dots, info, etc.) displayed in the badge.
	Icon kitex.Node
	// Label is the status text displayed.
	Label string
	// Color is the foreground color of the badge.
	Color color.Color
	// OnClick is triggered when the badge is clicked.
	// If nil, the badge is rendered as a static box.
	OnClick func()
}

// ToolBadge renders a standardized compact badge representing tool execution state.
var ToolBadge = kitex.FCC("ToolBadge", func(props ToolBadgeProps) kitex.Node {
	t := theme.UseTheme()

	boxStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		AlignSelf(style.AlignStart).
		Padding(0, 1).
		Gap(1).
		Height(style.Cells(1)).
		MarginVertical(1)

	if t != nil {
		boxStyle = boxStyle.
			Background(t.Color.Surface.BaseHover).
			Foreground(props.Color)
	}

	labelNode := kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(props.Label))

	if props.OnClick != nil {
		return Button(ButtonProps{
			Variant: ButtonText,
			Color:   ButtonBase,
			Style:   boxStyle,
			OnClick: props.OnClick,
		},
			props.Icon,
			labelNode,
		)
	}

	return kitex.Box(kitex.BoxProps{Style: boxStyle},
		props.Icon,
		labelNode,
	)
})
