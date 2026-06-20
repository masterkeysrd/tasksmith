package sidebar

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
)

func metricsPanel(data Data) kitex.Node {
	c := useColors()

	statusColor := components.PaperSuccess
	statusLabel := "READY"
	switch strings.ToLower(data.ActiveSessionStatus) {
	case "running":
		statusColor = components.PaperInfo
		statusLabel = "RUNNING"
	case "waiting", "waiting_approval":
		statusColor = components.PaperTertiary
		statusLabel = "WAITING"
	case "error", "failed":
		statusColor = components.PaperError
		statusLabel = "FAILED"
	case "":
		statusLabel = "IDLE"
	default:
		statusLabel = strings.ToUpper(strings.ReplaceAll(data.ActiveSessionStatus, "_", " "))
	}

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Gap(1).
			Padding(1).
			Background(c.panel),
	},
		sectionHeader("Metrics", "Live shell resources", nil),
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Gap(1),
		},
			metricCard("Status", statusLabel, "Active session state", statusColor),
			metricCard("Projects", fmt.Sprintf("%d", len(data.Projects)), "Workspace project roots", components.PaperInfo),
			metricCard("Agents", fmt.Sprintf("%d", len(data.Agents)), "Registered agent specs", components.PaperSecondary),
			metricCard("Providers", fmt.Sprintf("%d", len(data.Providers)), "Configured model providers", components.PaperContentAlt),
			metricCard("Tools", fmt.Sprintf("%d", countEnabledTools(data.AuthorizedTools)), "Authorized builtin tools", components.PaperContent),
		),
		components.Card(components.CardProps{
			Color:   components.PaperBase,
			Variant: components.CardOutlined,
			Style:   style.S().Background(c.surface),
		},
			components.CardHeader(components.CardHeaderProps{
				Avatar: kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.info)}, icon.Server),
				Title:  kitex.Text("CURRENT PROVIDER"),
			}),
			components.CardContent(components.CardContentProps{
				Style: style.S().Padding(1).Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.text).Bold(true)},
					kitex.Text(strings.ToUpper(data.DefaultProvider))),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.subtle)},
					kitex.Text(data.WorkspaceName)),
			),
		),
	)
}
