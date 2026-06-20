package sidebar

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
)

func metricsPanel(data Data) kitex.Node {
	c := useColors()

	// Mock stats data as per mockup.tsx / instructions
	tokensUsed := 35500
	tokenLimit := 131072
	systemTokens := 6528
	toolTokens := 10496
	messageTokens := 11264
	toolResultTokens := 3584
	filesTokens := 2048
	otherTokens := 1628

	usedPercent := int(float64(tokensUsed) * 100.0 / float64(tokenLimit))
	tokensStr := fmt.Sprintf("%.1fK / %dK TOKENS", float64(tokensUsed)/1000.0, tokenLimit/1024)

	// Calculate bar cells (total bar length is 30)
	barLength := 30
	systemCells := int(float64(systemTokens) * float64(barLength) / float64(tokenLimit))
	toolCells := int(float64(toolTokens) * float64(barLength) / float64(tokenLimit))
	messageCells := int(float64(messageTokens) * float64(barLength) / float64(tokenLimit))
	toolResultCells := int(float64(toolResultTokens) * float64(barLength) / float64(tokenLimit))
	filesCells := int(float64(filesTokens) * float64(barLength) / float64(tokenLimit))
	otherCells := int(float64(otherTokens) * float64(barLength) / float64(tokenLimit))

	// Ensure at least 1 cell for non-zero values
	if systemTokens > 0 && systemCells == 0 {
		systemCells = 1
	}
	if toolTokens > 0 && toolCells == 0 {
		toolCells = 1
	}
	if messageTokens > 0 && messageCells == 0 {
		messageCells = 1
	}
	if toolResultTokens > 0 && toolResultCells == 0 {
		toolResultCells = 1
	}
	if filesTokens > 0 && filesCells == 0 {
		filesCells = 1
	}
	if otherTokens > 0 && otherCells == 0 {
		otherCells = 1
	}

	totalUsedCells := systemCells + toolCells + messageCells + toolResultCells + filesCells + otherCells
	unusedCells := barLength - totalUsedCells
	if unusedCells < 0 {
		unusedCells = 0
	}

	// Calculate percentages
	systemPct := float64(systemTokens) * 100.0 / float64(tokenLimit)
	toolPct := float64(toolTokens) * 100.0 / float64(tokenLimit)
	messagePct := float64(messageTokens) * 100.0 / float64(tokenLimit)
	toolResultPct := float64(toolResultTokens) * 100.0 / float64(tokenLimit)
	filesPct := float64(filesTokens) * 100.0 / float64(tokenLimit)
	otherPct := float64(otherTokens) * 100.0 / float64(tokenLimit)

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Background(c.panel),
	},
		// Header row
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				JustifyContent(style.JustifyBetween).
				BorderBottom(true, style.SingleBorder(), c.border),
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Foreground(c.warning).
					Bold(true),
			},
				icon.Fire,
				kitex.Text(" CONTEXT_RESOURCES_STATUS"),
			),
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.background)}, kitex.Text("█")),
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.success)}, kitex.Text("█")),
			),
		),

		// Tokens values
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				JustifyContent(style.JustifyBetween),
		},
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.muted).Bold(true)}, kitex.Text(tokensStr)),
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.info).Bold(true)}, kitex.Text(fmt.Sprintf("%d%%", usedPercent))),
		),

		// Visual allocation bar chart
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow),
		},
			blockSpan(c.info, systemCells),
			blockSpan(c.magenta, toolCells),
			blockSpan(c.success, messageCells),
			blockSpan(c.warning, toolResultCells),
			blockSpan(c.error, filesCells),
			blockSpan(c.subtle, otherCells),
			kitex.If(unusedCells > 0, func() kitex.Node {
				return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(c.surface)}, kitex.Text(strings.Repeat("░", unusedCells)))
			}),
		),

		// Avail dynamic head
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				JustifyContent(style.JustifyBetween).
				MarginBottom(1),
		},
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.subtle)}, kitex.Text("Avail dynamic head")),
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.muted)}, kitex.Text(fmt.Sprintf("%.1f%%", 100.0-float64(tokensUsed)*100.0/float64(tokenLimit)))),
		),

		// System Allocations
		metricSectionHeader(icon.CPU, "SYSTEM STRUCTURE CODES", c.info),
		metricRow(c.info, "System Directives", systemPct, c),
		metricRow(c.magenta, "Tool Call Defs", toolPct, c),

		// User Content Allocations
		kitex.Box(kitex.BoxProps{Style: style.S().MarginTop(1)},
			metricSectionHeader(icon.Robot, "ACTIVE WORKER PAYLOADS", c.success),
		),
		metricRow(c.success, "Messages Matrix", messagePct, c),
		metricRow(c.warning, "Tool Result Buff", toolResultPct, c),
		metricRow(c.error, "Codebase Inject", filesPct, c),

		// Uncategorized
		kitex.Box(kitex.BoxProps{Style: style.S().MarginTop(1)},
			metricSectionHeader(kitex.Text(" "), "OTHER ARTIFACTS", c.muted),
		),
		metricRow(c.subtle, "Misc Overhead", otherPct, c),

		// Action Button
		components.Button(components.ButtonProps{
			Variant: components.ButtonOutline,
			Color:   components.ButtonInfo,
			Style:   style.S().Width(style.Percent(100)).JustifyContent(style.JustifyCenter).MarginTop(1),
			OnClick: func() {
				// Click action
			},
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter),
			},
				icon.Fire,
				kitex.Text(" OPEN INTERACTIVE ANALYTICS"),
			),
		),
	)
}

func blockSpan(color color.Color, count int) kitex.Node {
	if count <= 0 {
		return nil
	}
	return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(color)}, kitex.Text(strings.Repeat("█", count)))
}

func metricSectionHeader(iconNode kitex.Node, label string, color color.Color) kitex.Node {
	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			Gap(1).
			Foreground(color).
			Bold(true),
	},
		iconNode,
		kitex.Text(" "+strings.ToUpper(label)),
	)
}

func metricRow(dotColor color.Color, name string, pct float64, c colors) kitex.Node {
	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			JustifyContent(style.JustifyBetween).
			PaddingLeft(1),
	},
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Gap(1),
		},
			kitex.Span(kitex.SpanProps{Style: style.S().Foreground(dotColor)}, kitex.Text("■")),
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.subtle)}, kitex.Text(name)),
		),
		kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.text)}, kitex.Text(fmt.Sprintf("%.1f%%", pct))),
	)
}
