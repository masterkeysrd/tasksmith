package components

import (
	"image/color"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// AlertSeverity defines the urgency levels for the Alert component.
type AlertSeverity string

const (
	AlertSuccess AlertSeverity = "success"
	AlertInfo    AlertSeverity = "info"
	AlertWarning AlertSeverity = "warning"
	AlertError   AlertSeverity = "error"
)

// AlertVariant defines visual styles for the Alert component.
type AlertVariant string

const (
	// AlertStandard is a standard alert with a background color.
	AlertStandard AlertVariant = "standard"
	// AlertOutlined is an alert with a border.
	AlertOutlined AlertVariant = "outlined"
)

// AlertProps defines the properties for the Alert component.
type AlertProps struct {
	// Severity specifies the urgency level, which determines the default styling and icon.
	Severity AlertSeverity
	// Variant specifies the visual variant of the alert.
	Variant AlertVariant
	// ShowIcon indicates if the default severity icon should be displayed.
	ShowIcon bool
	// Icon allows providing a custom icon node.
	Icon kitex.Node
	// Action is an optional node (e.g., a Button) displayed at the end of the alert.
	Action kitex.Node
	// Style allows passing additional style overrides.
	Style style.Style
	// Children is the content of the alert (usually text).
	Children []kitex.Node
}

var (
	// AlertBaseStyle is the base style for the alert container.
	AlertBaseStyle = style.S().
			Display(style.DisplayFlex).
			AlignItems(style.AlignCenter).
			Padding(0, 1).
			Gap(1).
			Width(style.Percent(100))

	// AlertContentStyle is the style for the content wrapper.
	AlertContentStyle = style.S().
				Flex(1)
)

// Alert is a component for displaying important messages to the user.
// It uses Paper as its base and supports multiple severity levels and optional actions.
var Alert = kitex.FCC("Alert", func(props AlertProps) kitex.Node {
	t := theme.UseTheme()

	var bgColor color.Color
	var fgColor color.Color

	if t != nil {
		switch props.Severity {
		case AlertSuccess:
			bgColor = t.Color.Surface.SuccessFocus
			fgColor = t.Color.Surface.Success
		case AlertInfo:
			bgColor = t.Color.Surface.InfoFocus
			fgColor = t.Color.Surface.Info
		case AlertWarning:
			bgColor = t.Color.Surface.TertiaryFocus
			fgColor = t.Color.Surface.Tertiary
		case AlertError:
			bgColor = t.Color.Surface.ErrorFocus
			fgColor = t.Color.Surface.Error
		default:
			bgColor = t.Color.Surface.Base
			fgColor = t.Color.Text.Primary
		}
	}

	alertStyle := AlertBaseStyle
	if t != nil {
		if props.Variant == AlertOutlined {
			alertStyle = alertStyle.
				Background(t.Color.Surface.Base).
				Foreground(fgColor).
				Border(style.SingleBorder().Color(fgColor))
		} else {
			alertStyle = alertStyle.
				Background(bgColor).
				Foreground(fgColor)
		}
	}

	// Determine the icon to display.
	var iconNode kitex.Node
	if props.Icon != nil {
		iconNode = props.Icon
	} else if props.ShowIcon {
		// Use the correct styled text (which inherits the fgColor automatically!)
		switch props.Severity {
		case AlertSuccess:
			iconNode = icon.Check
		case AlertInfo:
			iconNode = icon.Info
		case AlertWarning:
			iconNode = icon.Warning
		case AlertError:
			iconNode = icon.Error
		}
	}

	return Paper(PaperProps{
		Color:   PaperBase,
		Variant: PaperDefault, // Let Alert handle the border styling via alertStyle
		Style:   alertStyle.Merge(props.Style),
	},
		kitex.If(iconNode != nil, func() kitex.Node {
			return iconNode
		}),
		kitex.Box(kitex.BoxProps{
			Style: AlertContentStyle,
		},
			props.Children...,
		),
		kitex.If(props.Action != nil, func() kitex.Node {
			return props.Action
		}),
	)
})
