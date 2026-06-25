package analytics

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/masterkeysrd/tasksmith/internal/tui/tokenutils"
)

type Props struct {
	OnClose func()
}

type colors struct {
	background color.Color
	panel      color.Color
	surface    color.Color
	border     color.Color
	text       color.Color
	muted      color.Color
	subtle     color.Color
	info       color.Color
	success    color.Color
	warning    color.Color
	error      color.Color
	magenta    color.Color
	inverse    color.Color
}

func useColors() colors {
	t := theme.UseTheme()
	if t == nil {
		return colors{}
	}

	palette := func(name string, fallback color.Color) color.Color {
		if t.Palette != nil {
			if c, ok := t.Palette[name]; ok {
				return c
			}
		}
		return fallback
	}

	background := palette("bg_dark", t.Color.Surface.BaseHover)
	panel := palette("bg_dark", background)
	surface := palette("bg_highlight", t.Color.Surface.BaseFocus)
	text := palette("fg", t.Color.Text.Primary)
	muted := palette("fg_dark", t.Color.Text.Secondary)
	subtle := palette("comment", t.Color.Text.Tertiary)
	info := palette("cyan", t.Color.Surface.Primary)
	success := palette("green", t.Color.Surface.Success)
	warning := palette("yellow", t.Color.Surface.Tertiary)
	error := palette("red", t.Color.Surface.Error)
	inverse := palette("bg_dark", t.Color.Text.InversePrimary)
	magenta := palette("magenta", t.Color.Surface.Secondary)

	return colors{
		background: background,
		panel:      panel,
		surface:    surface,
		border:     t.Color.Border.Primary,
		text:       text,
		muted:      muted,
		subtle:     subtle,
		info:       info,
		success:    success,
		warning:    warning,
		error:      error,
		inverse:    inverse,
		magenta:    magenta,
	}
}

// View renders the Token Analytics Dashboard.
var View = kitex.FC("TokenAnalytics", func(props Props) kitex.Node {
	c := useColors()

	// Reactive states bound to the local active store
	timeframe := UseTimeframe()
	metricUnit := UseMetricUnit()
	providerFilter := UseProviderFilter()
	activeTab := UseActiveTab()

	// Fetch query-backed telemetry data
	query := queries.UseGetTokenAnalytics(api.GetTokenAnalyticsRequest{
		Timeframe:      timeframe,
		ProviderFilter: providerFilter,
	})

	data := &api.GetTokenAnalyticsResponse{}
	if query.Data != nil {
		data = query.Data
	}

	kitex.UseEffect(func() {
		if query.Data != nil && len(query.Data.ProvidersList) > 0 {
			SetAvailableProviders(query.Data.ProvidersList)
		}
	}, []any{query.Data})

	// Pre-filter tools for the Tools tab
	var coreTools []api.ToolAnalytics
	var mcpTools []api.ToolAnalytics
	for _, t := range data.Tools {
		if strings.HasPrefix(t.ToolName, "mcp_") || strings.Contains(t.ToolName, "mcp") {
			mcpTools = append(mcpTools, t)
		} else {
			coreTools = append(coreTools, t)
		}
	}

	// Layout Styles
	rootStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Flex(1, 1, style.Cells(0)).
		MinHeight(style.Cells(0)).
		Height(style.Percent(100)).
		Padding(1).
		Background(c.background)

	headerBoxStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Border(true, style.SingleBorder(), c.border).
		Padding(1).
		MarginBottom(1).
		Background(c.panel)

	headerRowStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		JustifyContent(style.JustifyBetween)

	headerStatsStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		Gap(3).
		PaddingTop(1).
		BorderTop(true, style.SingleBorder(), c.border)

	tabRowStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		JustifyContent(style.JustifyBetween).
		BorderBottom(true, style.SingleBorder(), c.border).
		MarginBottom(1)

	tabButtonStyle := style.S().
		PaddingHorizontal(2).
		PaddingVertical(0).
		Bold(true)

	tabActiveStyle := style.S().
		Background(c.info).
		Foreground(c.inverse)

	gridStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		Gap(2).
		Flex(1, 1, style.Cells(0)).
		MinHeight(style.Cells(0))

	columnStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Flex(1, 1, style.Cells(0)).
		MinHeight(style.Cells(0)).
		Overflow(style.OverflowAuto)

	// Format helpers
	costStr := formatCost(data.GlobalStats.EstimatedCostUSD)
	var metricValStr string
	if metricUnit == "calls" {
		metricValStr = strconv.Itoa(data.GlobalStats.TotalCalls)
	} else {
		metricValStr = formatVal(data.GlobalStats.TotalTokens, "tokens")
	}

	sessionsStr := strconv.Itoa(data.GlobalStats.TotalSessions)

	cacheHitPct := 0.0
	if data.GlobalStats.PromptTokens > 0 {
		cacheHitPct = float64(data.GlobalStats.CacheReadTokens) * 100.0 / float64(data.GlobalStats.PromptTokens)
	}
	cacheHitStr := fmt.Sprintf("%.1f%%", cacheHitPct)

	inboundStr := formatVal(data.GlobalStats.PromptTokens, "tokens")
	outboundStr := formatVal(data.GlobalStats.CompletionTokens, "tokens")

	// Resolve actual timeframe label
	var timeframeLabel string
	switch timeframe {
	case "today":
		timeframeLabel = "Today (24h)"
	case "7days":
		timeframeLabel = "7 Days"
	case "30days":
		timeframeLabel = "This Month"
	}

	return kitex.Box(kitex.BoxProps{Style: rootStyle},
		// Header Panel
		kitex.Box(kitex.BoxProps{Style: headerBoxStyle},
			kitex.Box(kitex.BoxProps{Style: headerRowStyle},
				kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).Gap(1),
				},
					kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.warning).Bold(true)}, kitex.Text("TOKEN ANALYTICS")),
					kitex.Box(kitex.BoxProps{Style: style.S().Background(c.surface).PaddingHorizontal(1)}, kitex.Text(timeframeLabel)),
				),
				// Header Controls
				kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).Gap(2),
				},
					kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.subtle)}, kitex.Text(fmt.Sprintf("PROVIDER: %s [P]", providerFilter))),
					kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.subtle)}, kitex.Text(fmt.Sprintf("METRIC: %s [T]", strings.ToUpper(metricUnit)))),
					components.Button(components.ButtonProps{
						Variant: components.ButtonOutline,
						Color:   components.ButtonError,
						Style:   style.S().PaddingHorizontal(1),
						OnClick: func() {
							if props.OnClose != nil {
								props.OnClose()
							}
						},
					}, kitex.Text("QUIT [Q]")),
				),
			),
			// Header Quick Stats Row
			kitex.Box(kitex.BoxProps{Style: headerStatsStyle},
				quickStat(c.warning, "COST", costStr),
				quickStat(c.info, "METRIC", fmt.Sprintf("%s (%s)", metricValStr, strings.ToUpper(metricUnit))),
				quickStat(c.success, "SESSIONS", sessionsStr),
				quickStat(c.magenta, "CACHE HIT", cacheHitStr),
				quickStat(c.subtle, "INBOUND", inboundStr),
				quickStat(c.subtle, "OUTBOUND", outboundStr),
			),
		),

		// Tabs bar
		kitex.Box(kitex.BoxProps{Style: tabRowStyle},
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
			},
				tabButton("SUMMARY", activeTab, c, tabButtonStyle, tabActiveStyle),
				tabButton("PROJECTS", activeTab, c, tabButtonStyle, tabActiveStyle),
				tabButton("TOOLS", activeTab, c, tabButtonStyle, tabActiveStyle),
			),
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.subtle)}, kitex.Text("[TAB] to cycle")),
		),

		// Grid Floor / Dynamic Panel based on activeTab
		func() kitex.Node {
			switch activeTab {
			case "SUMMARY":
				// Row-based layout (vertical stack of full-width rows)
				// We use 40 max blocks for bars to stretch across the wide panels
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Flex(1, 1, style.Cells(0)).
						MinHeight(style.Cells(0)).
						Overflow(style.OverflowAuto),
				},
					// Daily Activity Panel
					MetricsPanel(MetricsPanelProps{Title: "Daily Activity", Color: c.warning},
						MetricsTable(MetricsTableProps{
							Columns: []MetricsColumnDef{
								{Name: "Timeframe", MinSize: 8, Align: style.TextAlignLeft},
								{Name: "Bar Load", Flex: 1, Align: style.TextAlignCenter},
								{Name: "Cost", Size: 8, Align: style.TextAlignRight},
								{Name: "Val", Size: 8, Align: style.TextAlignRight},
							},
							Rows: mapStatsToCells(
								data.DailyActivity, metricUnit, 40, c.warning, c,
								func(d api.DailyActivity) string { return d.Day },
								func(d api.DailyActivity) int { return d.TotalCalls },
								func(d api.DailyActivity) int { return d.TotalTokens },
								func(d api.DailyActivity) float64 { return d.EstimatedCostUSD },
							),
						}),
					),
					// By Model Panel
					MetricsPanel(MetricsPanelProps{Title: "By Model", Color: c.magenta},
						MetricsTable(MetricsTableProps{
							Columns: []MetricsColumnDef{
								{Name: "Model Node", MinSize: 24, Align: style.TextAlignLeft},
								{Name: "Burn Share", Flex: 1, Align: style.TextAlignCenter},
								{Name: "Cost", Size: 8, Align: style.TextAlignRight},
								{Name: "Val", Size: 8, Align: style.TextAlignRight},
							},
							Rows: mapStatsToCells(
								data.ByModel, metricUnit, 40, c.magenta, c,
								func(m api.ByModelStats) string { return m.ModelName },
								func(m api.ByModelStats) int { return m.TotalCalls },
								func(m api.ByModelStats) int { return m.TotalTokens },
								func(m api.ByModelStats) float64 { return m.EstimatedCostUSD },
							),
						}),
					),
					// By Agent Panel
					MetricsPanel(MetricsPanelProps{Title: "By Agent", Color: c.success},
						MetricsTable(MetricsTableProps{
							Columns: []MetricsColumnDef{
								{Name: "Agent Identity", MinSize: 6, Align: style.TextAlignLeft},
								{Name: "Distribution", Flex: 1, Align: style.TextAlignCenter},
								{Name: "Cost", Size: 8, Align: style.TextAlignRight},
								{Name: "Val", Size: 8, Align: style.TextAlignRight},
							},
							Rows: mapStatsToCells(
								data.ByAgent, metricUnit, 40, c.success, c,
								func(a api.ByAgentStats) string { return a.AgentName },
								func(a api.ByAgentStats) int { return a.TotalCalls },
								func(a api.ByAgentStats) int { return a.TotalTokens },
								func(a api.ByAgentStats) float64 { return a.EstimatedCostUSD },
							),
						}),
					),
				)

			case "PROJECTS":
				// Column-based side-by-side layout
				// We use 14 max blocks for bars
				return kitex.Box(kitex.BoxProps{Style: gridStyle},
					// Left Panel Column
					kitex.Box(kitex.BoxProps{Style: columnStyle},
						MetricsPanel(MetricsPanelProps{Title: "By Project", Color: c.info},
							MetricsTable(MetricsTableProps{
								Columns: []MetricsColumnDef{
									{Name: "Project Name", MinSize: 12, Align: style.TextAlignLeft},
									{Name: "Distribution", Flex: 1, Align: style.TextAlignCenter},
									{Name: "Cost", Size: 6, Align: style.TextAlignRight},
									{Name: "Val", Size: 6, Align: style.TextAlignRight},
								},
								Rows: mapStatsToCells(
									data.ByProject, metricUnit, 14, c.info, c,
									func(p api.ByProjectStats) string { return p.ProjectName },
									func(p api.ByProjectStats) int { return p.TotalCalls },
									func(p api.ByProjectStats) int { return p.TotalTokens },
									func(p api.ByProjectStats) float64 { return p.EstimatedCostUSD },
								),
							}),
						),
					),
					// Right Panel Column
					kitex.Box(kitex.BoxProps{Style: columnStyle},
						MetricsPanel(MetricsPanelProps{Title: "MCP Servers (Local Nodes)", Color: c.error},
							MetricsTable(MetricsTableProps{
								Columns: []MetricsColumnDef{
									{Name: "Server", Size: 14, Align: style.TextAlignLeft},
									{Name: "Status", Flex: 1, Align: style.TextAlignCenter},
									{Name: "Links", Size: 6, Align: style.TextAlignRight},
								},
								Rows: [][]MetricsCell{
									{
										{Value: "playwright-mcp", Color: c.text},
										{Value: "nominal", Color: c.success},
										{Value: "3 links", Color: c.muted},
									},
									{
										{Value: "seq-thinking", Color: c.text},
										{Value: "nominal", Color: c.success},
										{Value: "1 link", Color: c.muted},
									},
									{
										{Value: "docker-manage", Color: c.text},
										{Value: "inactive", Color: c.error},
										{Value: "0 links", Color: c.muted},
									},
								},
							}),
						),
					),
				)

			case "TOOLS":
				// Column-based side-by-side layout
				// We use 14 max blocks for bars
				return kitex.Box(kitex.BoxProps{Style: gridStyle},
					// Left Panel Column
					kitex.Box(kitex.BoxProps{Style: columnStyle},
						MetricsPanel(MetricsPanelProps{Title: "Core Tools Invocations", Color: c.success},
							MetricsTable(MetricsTableProps{
								Columns: []MetricsColumnDef{
									{Name: "Tool Name", MinSize: 14, Align: style.TextAlignLeft},
									{Name: "Distribution", Flex: 1, Align: style.TextAlignCenter},
									{Name: "Cost", Size: 6, Align: style.TextAlignRight},
									{Name: "Val", Size: 6, Align: style.TextAlignRight},
								},
								Rows: mapStatsToCells(
									coreTools, metricUnit, 14, c.success, c,
									func(t api.ToolAnalytics) string { return t.ToolName },
									func(t api.ToolAnalytics) int { return t.TotalCalls },
									func(t api.ToolAnalytics) int { return t.TotalTokens },
									func(t api.ToolAnalytics) float64 { return t.EstimatedCostUSD },
								),
							}),
						),
						MetricsPanel(MetricsPanelProps{Title: "Core Tool Performance", Color: c.success},
							MetricsTable(MetricsTableProps{
								Columns: []MetricsColumnDef{
									{Name: "Tool Name", Size: 14, Align: style.TextAlignLeft},
									{Name: "Avg Latency", Flex: 1, Align: style.TextAlignCenter},
									{Name: "Success Rate", Size: 6, Align: style.TextAlignRight},
								},
								Rows: mapToolPerformanceToCells(coreTools, c),
							}),
						),
					),

					// Right Panel Column
					kitex.Box(kitex.BoxProps{Style: columnStyle},
						MetricsPanel(MetricsPanelProps{Title: "MCP Tools Invocations", Color: c.error},
							MetricsTable(MetricsTableProps{
								Columns: []MetricsColumnDef{
									{Name: "Tool Name", Size: 14, Align: style.TextAlignLeft},
									{Name: "Distribution", Flex: 1, Align: style.TextAlignCenter},
									{Name: "Cost", Size: 6, Align: style.TextAlignRight},
									{Name: "Val", Size: 6, Align: style.TextAlignRight},
								},
								Rows: mapStatsToCells(
									mcpTools, metricUnit, 14, c.error, c,
									func(t api.ToolAnalytics) string { return t.ToolName },
									func(t api.ToolAnalytics) int { return t.TotalCalls },
									func(t api.ToolAnalytics) int { return t.TotalTokens },
									func(t api.ToolAnalytics) float64 { return t.EstimatedCostUSD },
								),
							}),
						),
						MetricsPanel(MetricsPanelProps{Title: "MCP Tool Performance", Color: c.error},
							MetricsTable(MetricsTableProps{
								Columns: []MetricsColumnDef{
									{Name: "Tool Name", Size: 14, Align: style.TextAlignLeft},
									{Name: "Avg Latency", Flex: 1, Align: style.TextAlignCenter},
									{Name: "Success Rate", Size: 6, Align: style.TextAlignRight},
								},
								Rows: mapToolPerformanceToCells(mcpTools, c),
							}),
						),
					),
				)

			default:
				return kitex.Box(kitex.BoxProps{Style: style.S().Flex(1)})
			}
		}(),

		// Help bar at the bottom
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Border(true, style.SingleBorder(), c.border).
				Padding(1).
				MarginTop(1).
				Background(c.panel).
				Foreground(c.subtle).
				Bold(true),
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(3),
			},
				components.Button(components.ButtonProps{
					Variant: components.ButtonText,
					Color:   components.ButtonBase,
					Style:   style.S().Foreground(c.text),
					OnClick: func() {
						SetTimeframe("today")
					},
				}, kitex.Text("[1] TODAY")),
				components.Button(components.ButtonProps{
					Variant: components.ButtonText,
					Color:   components.ButtonBase,
					Style:   style.S().Foreground(c.text),
					OnClick: func() {
						SetTimeframe("7days")
					},
				}, kitex.Text("[2] 7 DAYS")),
				components.Button(components.ButtonProps{
					Variant: components.ButtonText,
					Color:   components.ButtonBase,
					Style:   style.S().Foreground(c.text),
					OnClick: func() {
						SetTimeframe("30days")
					},
				}, kitex.Text("[3] 30 DAYS")),
				components.Button(components.ButtonProps{
					Variant: components.ButtonText,
					Color:   components.ButtonInfo,
					Style:   style.S().Foreground(c.info),
					OnClick: func() {
						CycleProviderFilter()
					},
				}, kitex.Text("[P] CYCLE PROVIDER")),
				components.Button(components.ButtonProps{
					Variant: components.ButtonText,
					Color:   components.ButtonSuccess,
					Style:   style.S().Foreground(c.success),
					OnClick: func() {
						ToggleMetricUnit()
					},
				}, kitex.Text("[T] TOGGLE METRIC")),
				components.Button(components.ButtonProps{
					Variant: components.ButtonText,
					Color:   components.ButtonError,
					Style:   style.S().Foreground(c.error),
					OnClick: func() {
						if props.OnClose != nil {
							props.OnClose()
						}
					},
				}, kitex.Text("[Q/ESC] QUIT")),
			),
		),
	)
})

func quickStat(lblColor color.Color, label string, val string) kitex.Node {
	return kitex.Box(kitex.BoxProps{
		Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
	},
		kitex.Box(kitex.BoxProps{Style: style.S().Foreground(lblColor).Bold(true)}, kitex.Text(label+":")),
		kitex.Box(kitex.BoxProps{Style: style.S().Bold(true)}, kitex.Text(val)),
	)
}

func tabButton(tab string, activeTab string, c colors, baseStyle style.Style, activeStyle style.Style) kitex.Node {
	styleToUse := baseStyle
	if activeTab == tab {
		styleToUse = style.S().
			PaddingHorizontal(2).
			PaddingVertical(0).
			Bold(true).
			Background(c.info).
			Foreground(c.inverse)
	}

	return components.Button(components.ButtonProps{
		Variant: components.ButtonText,
		Color:   components.ButtonBase,
		Style:   styleToUse,
		OnClick: func() {
			SetActiveTab(tab)
		},
	}, kitex.Text("["+tab+"]"))
}

// MetricsPanelProps holds fields to render the panel component.
type MetricsPanelProps struct {
	Title    string
	Color    color.Color
	Children []kitex.Node
}

// MetricsColumnDef defines the metadata and layout options for a table column.
type MetricsColumnDef struct {
	Name    string
	Flex    int
	Size    int
	MinSize int
	Align   style.TextAlign
}

// MetricsCell holds visual styling and cell values for a single table row.
type MetricsCell struct {
	Value  string
	Color  color.Color
	Render func() kitex.Node
}

// MetricsTableProps defines properties for standard tabular analytics widgets.
type MetricsTableProps struct {
	Columns []MetricsColumnDef
	Rows    [][]MetricsCell
}

// MetricsPanel renders a border-collapsed container with a header title bar.
var MetricsPanel = kitex.FCC("MetricsPanel", func(props MetricsPanelProps) kitex.Node {
	c := useColors()
	borderColor := props.Color
	if borderColor == nil {
		borderColor = c.border
	}
	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Border(true, style.SingleBorder(), borderColor).
			Background(c.panel).
			MarginBottom(1),
	},
		// Title Line
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				PaddingLeft(1).
				PaddingRight(1).
				BorderBottom(true, style.SingleBorder(), c.border),
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().Foreground(props.Color).Bold(true),
			}, kitex.Text(strings.ToUpper(props.Title))),
		),
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				PaddingLeft(1).
				PaddingRight(1).
				PaddingBottom(1),
		}, props.Children...),
	)
})

// MetricsHeader renders table headers aligned with column sizing.
var MetricsHeader = kitex.FC("MetricsHeader", func(columns []MetricsColumnDef) kitex.Node {
	c := useColors()
	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			JustifyContent(style.JustifyBetween).
			Foreground(c.subtle).
			Bold(true).
			Gap(1).
			MarginBottom(1),
	},
		kitex.Map(columns, func(col MetricsColumnDef, _ int) kitex.Node {
			s := style.S()
			if col.Size > 0 {
				s = s.Width(style.Cells(col.Size))
			} else if col.Flex > 0 {
				s = s.Flex(col.Flex, col.Flex, style.Cells(0))
			}
			if col.MinSize > 0 {
				s = s.MinWidth(style.Cells(col.MinSize))
			}
			if col.Align != 0 {
				s = s.TextAlign(col.Align)
			}
			return kitex.Box(kitex.BoxProps{Style: s}, kitex.Text(strings.ToUpper(col.Name)))
		}),
	)
})

// MetricsRowProps defines fields to render a row with column alignment.
type MetricsRowProps struct {
	Columns []MetricsColumnDef
	Cells   []MetricsCell
}

// MetricsRow renders a row of columns mapped to cells.
var MetricsRow = kitex.FC("MetricsRow", func(props MetricsRowProps) kitex.Node {
	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Gap(1).
			JustifyContent(style.JustifyBetween),
	},
		kitex.Map(props.Cells, func(cell MetricsCell, idx int) kitex.Node {
			var col MetricsColumnDef
			if idx < len(props.Columns) {
				col = props.Columns[idx]
			}

			s := style.S()
			if col.Size > 0 {
				s = s.Width(style.Cells(col.Size))
			} else if col.Flex > 0 {
				s = s.Flex(col.Flex, col.Flex, style.Cells(0))
			}
			if col.Align != 0 {
				s = s.TextAlign(col.Align)
			}
			if col.MinSize > 0 {
				s = s.MinWidth(style.Cells(col.MinSize))
			}
			if cell.Color != nil {
				s = s.Foreground(cell.Color)
			}

			var content kitex.Node
			if cell.Render != nil {
				content = cell.Render()
			} else {
				content = kitex.Text(cell.Value)
			}

			if cell.Render != nil {
				wrapperStyle := s.Display(style.DisplayFlex)
				if col.Align == style.TextAlignCenter {
					wrapperStyle = wrapperStyle.JustifyContent(style.JustifyCenter)
				} else if col.Align == style.TextAlignRight {
					wrapperStyle = wrapperStyle.JustifyContent(style.JustifyEnd)
				}
				return kitex.Box(kitex.BoxProps{Style: wrapperStyle}, content)
			}

			return kitex.Box(kitex.BoxProps{Style: s}, content)
		}),
	)
})

// MetricsTable renders a table containing header and rows.
var MetricsTable = kitex.FC("MetricsTable", func(props MetricsTableProps) kitex.Node {
	return kitex.Fragment(
		MetricsHeader(props.Columns),
		kitex.Map(props.Rows, func(row []MetricsCell, _ int) kitex.Node {
			return MetricsRow(MetricsRowProps{
				Columns: props.Columns,
				Cells:   row,
			})
		}),
	)
})

func mapStatsToCells[T any](
	items []T,
	metricUnit string,
	barWidth int,
	themeColor color.Color,
	c colors,
	getName func(T) string,
	getCalls func(T) int,
	getTokens func(T) int,
	getCost func(T) float64,
) [][]MetricsCell {
	maxVal := 1
	for _, item := range items {
		v := getCalls(item)
		if metricUnit == "tokens" {
			v = getTokens(item)
		}
		if v > maxVal {
			maxVal = v
		}
	}
	rows := make([][]MetricsCell, len(items))
	for i, item := range items {
		val := getCalls(item)
		if metricUnit == "tokens" {
			val = getTokens(item)
		}
		pct := int(float64(val) * 100.0 / float64(maxVal))
		rows[i] = []MetricsCell{
			{Value: getName(item), Color: c.text},
			{Render: func() kitex.Node { return drawBlockBar(pct, barWidth, themeColor, c) }},
			{Value: formatCost(getCost(item)), Color: themeColor},
			{Value: formatVal(val, metricUnit), Color: c.muted},
		}
	}
	return rows
}

func mapToolPerformanceToCells(
	tools []api.ToolAnalytics,
	c colors,
) [][]MetricsCell {
	rows := make([][]MetricsCell, len(tools))
	for i, t := range tools {
		successPct := int(t.SuccessRate * 100)
		successColor := c.success
		if t.SuccessRate < 0.8 {
			successColor = c.error
		} else if t.SuccessRate < 0.95 {
			successColor = c.warning
		}
		rows[i] = []MetricsCell{
			{Value: t.ToolName, Color: c.text},
			{Value: fmt.Sprintf("%dms", t.AvgLatencyMs), Color: c.warning},
			{Value: fmt.Sprintf("%d%% OK", successPct), Color: successColor},
		}
	}
	return rows
}

func drawBlockBar(pct int, maxBlocks int, color color.Color, c colors) kitex.Node {
	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Height(style.Cells(1)).
			Width(style.Percent(100)).
			Background(c.border),
	},
		kitex.If(pct > 0, func() kitex.Node {
			valPct := pct
			if valPct < 1 {
				valPct = 1
			}
			return kitex.Box(kitex.BoxProps{
				Style: style.S().
					Height(style.Cells(1)).
					Width(style.Percent(float32(valPct))).
					Background(color),
			})
		}),
	)
}

func formatCost(val float64) string {
	if val == 0 {
		return "$0.00"
	}
	if val < 1 {
		return fmt.Sprintf("$%.3f", val)
	}
	return fmt.Sprintf("$%.2f", val)
}

func formatVal(val int, unit string) string {
	if unit == "calls" {
		return strconv.Itoa(val)
	}
	return tokenutils.FormatTokens(val)
}
