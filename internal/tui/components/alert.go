package components

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/highlight"
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

var (
	// HLAlertSuccess is linked to the Hint theme group.
	HLAlertSuccess = highlight.Set("AlertSuccess", highlight.Link("Hint"))
	// HLAlertInfo is linked to the Info theme group.
	HLAlertInfo = highlight.Set("AlertInfo", highlight.Link("Info"))
	// HLAlertWarning is linked to the Warn theme group.
	HLAlertWarning = highlight.Set("AlertWarning", highlight.Link("Warn"))
	// HLAlertError is linked to the Error theme group.
	HLAlertError = highlight.Set("AlertError", highlight.Link("Error"))
)

// AlertProps defines the properties for the Alert component.
type AlertProps struct {
	// Group is the highlight group to use for theme-aware styling.
	// If not provided, it defaults to the group corresponding to the Severity.
	Group highlight.Group
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
		Gap(1)

	// AlertContentStyle is the style for the content wrapper.
	AlertContentStyle = style.S().
		Flex(1)
)

// Alert is a component for displaying important messages to the user.
// It uses Paper as its base and supports multiple severity levels and optional actions.
var Alert = kitex.FCC("Alert", func(props AlertProps) kitex.Node {
	// Resolve the highlight group based on severity if not explicitly provided.
	severityGroup := props.Group
	if severityGroup.Empty() {
		switch props.Severity {
		case AlertSuccess:
			severityGroup = HLAlertSuccess
		case AlertInfo:
			severityGroup = HLAlertInfo
		case AlertWarning:
			severityGroup = HLAlertWarning
		case AlertError:
			severityGroup = HLAlertError
		default:
			severityGroup = highlight.Set("Alert", highlight.Link("Normal"))
		}
	}

	// Map AlertVariant to PaperVariant
	paperVariant := PaperDefault
	if props.Variant == AlertOutlined {
		paperVariant = PaperOutlined
	}

	// Determine the icon to display.
	var iconNode kitex.Node
	if props.Icon != nil {
		iconNode = props.Icon
	} else if props.ShowIcon {
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
		Group:   severityGroup,
		Variant: paperVariant,
		Style:   AlertBaseStyle.Merge(props.Style),
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
