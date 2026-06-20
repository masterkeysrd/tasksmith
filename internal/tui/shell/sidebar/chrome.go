package sidebar

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
)

type tabDef struct {
	Value Tab
	Label string
}

var sidebarTabs = []tabDef{
	{Value: TabExplorer, Label: "EXP"},
	{Value: TabOrchestrator, Label: "AGT"},
	{Value: TabSessions, Label: "SES"},
	{Value: TabMetrics, Label: "MET"},
}

var Content = kitex.FC("ShellSidebarContent", func(props ContentProps) kitex.Node {
	c := useColors()

	frameStyle := style.S().
		Width(style.Cells(34)).
		MinWidth(style.Cells(34)).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Background(c.background).
		BorderRight(true, style.SingleBorder(), c.border)

	bodyStyle := style.S().
		Padding(1).
		Background(c.panel)

	tabListStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		Background(c.background).
		Gap(0).
		Padding(0)

	tabStyle := style.S().
		Flex(1).
		AlignItems(style.AlignCenter).
		PaddingHorizontal(0).
		JustifyContent(style.JustifyCenter).
		Background(c.background).
		Foreground(c.subtle).
		TextAlign(style.TextAlignCenter)

	tabHoverStyle := style.S().
		Background(c.surface).
		Foreground(c.muted)

	tabActiveStyle := style.S().
		Background(c.success).
		Foreground(c.inverse).
		Bold(true)

	return kitex.Box(kitex.BoxProps{Style: frameStyle},
		components.Tabs(components.TabsProps{
			Value:        props.CurrentTab,
			Color:        components.PaperBase,
			Style:        style.S().Flex(1, 1, style.Cells(0)).MinHeight(style.Cells(0)),
			TabListStyle: tabListStyle,
			OnChange: func(value any) {
				tab, ok := value.(Tab)
				if ok && props.OnSelectTab != nil {
					props.OnSelectTab(tab)
				}
			},
		},
			kitex.Map(sidebarTabs, func(tab tabDef, _ int) kitex.Node {
				return components.Tab(components.TabProps{
					Value:       tab.Value,
					Variant:     components.ButtonText,
					Color:       components.ButtonBase,
					Style:       tabStyle,
					HoverStyle:  tabHoverStyle,
					ActiveStyle: tabActiveStyle,
				},
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Width(style.Percent(100)).
							TextAlign(style.TextAlignCenter),
					}, kitex.Text("["+tab.Label+"]")),
				)
			}),
			components.TabPanel(components.TabPanelProps{
				Value: TabExplorer,
				Style: bodyStyle,
			}, explorerPanel(props.Data, props.ExpandedPaths, props.OnTogglePath)),
			components.TabPanel(components.TabPanelProps{
				Value: TabOrchestrator,
				Style: bodyStyle,
			}, orchestratorPanel(props.Data, props.OnCreateAgent)),
			components.TabPanel(components.TabPanelProps{
				Value: TabSessions,
				Style: bodyStyle,
			}, sessionsPanel(props.Data, props.OnSelectSession, props.OnCreateSession, props.OnRenameSession, props.OnArchiveSession, props.OnDeleteSession)),
			components.TabPanel(components.TabPanelProps{
				Value: TabMetrics,
				Style: bodyStyle,
			}, metricsPanel(props.Data)),
		),
		sidebarFooter(props.Data),
	)
})

func sectionHeader(title, subtitle string, action kitex.Node) kitex.Node {
	c := useColors()

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			AlignItems(style.AlignCenter).
			Gap(1).
			PaddingHorizontal(1).
			PaddingVertical(1),
	},
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Flex(1),
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().Foreground(c.text).Bold(true),
			}, kitex.Text(strings.ToUpper(title))),
			kitex.If(subtitle != "", func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(c.subtle),
				}, kitex.Text(subtitle))
			}),
		),
		kitex.If(action != nil, func() kitex.Node { return action }),
	)
}

func sidebarFooter(data Data) kitex.Node {
	c := useColors()

	statusLabel := "SETUP REQUIRED"
	contextValue := "0%"
	if data.IsConfigured {
		statusLabel = " SYS: READY"
		total := len(data.AuthorizedTools)
		if total > 0 {
			contextValue = fmt.Sprintf("%d%%", (countEnabledTools(data.AuthorizedTools)*100)/total)
		}
	}
	if !data.IsConfigured {
		statusLabel = " SYS: SETUP"
	}

	footerStyle := style.S().
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter).
		JustifyContent(style.JustifyBetween).
		PaddingHorizontal(2).
		PaddingVertical(1).
		Foreground(c.subtle)

	return kitex.Box(kitex.BoxProps{Style: footerStyle},
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				AlignItems(style.AlignCenter).
				Gap(1).
				Foreground(c.subtle),
		},
			icon.Lightning,
			kitex.Text(statusLabel),
		),
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				AlignItems(style.AlignCenter).
				Gap(1).
				Foreground(c.subtle),
		},
			kitex.Text("CONTEXT:"),
			kitex.Box(kitex.BoxProps{
				Style: style.S().Foreground(c.success).Bold(true),
			}, kitex.Text(contextValue)),
		),
	)
}

func metricCard(title, value, detail string, colorName components.PaperColor) kitex.Node {
	c := useColors()

	return components.Card(components.CardProps{
		Color:   colorName,
		Variant: components.CardOutlined,
		Style: style.S().
			Padding(1).
			Gap(1),
	},
		components.CardContent(components.CardContentProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
		},
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.subtle)}, kitex.Text(strings.ToUpper(title))),
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.text).Bold(true)}, kitex.Text(value)),
			kitex.If(detail != "", func() kitex.Node {
				return kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.muted)}, kitex.Text(detail))
			}),
		),
	)
}
