package sidebar

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/tokenutils"
)

func metricsPanel(data Data) kitex.Node {
	c := useColors()

	var activeSession *api.Session
	for _, s := range data.Sessions {
		if s.ID == data.ActiveSessionID {
			sess := s
			activeSession = &sess
			break
		}
	}

	metrics := data.LastTurnMetrics

	tokensUsed := 0
	tokenLimit := 131072 // Default fallback
	outputLimit := 4096  // Default fallback

	if activeSession != nil {
		for _, p := range data.Providers {
			if p.Name == activeSession.Settings.ProviderName {
				for _, m := range p.Models {
					if (m.ID == activeSession.Settings.ModelName || m.Name == activeSession.Settings.ModelName) && m.ContextWindow > 0 {
						tokenLimit = m.ContextWindow
						if m.MaxOutputTokens > 0 {
							outputLimit = m.MaxOutputTokens
						}
					}
				}
			}
		}
	}
	systemTokens := 0
	toolTokens := 0
	messageTokens := 0
	toolResultTokens := 0
	filesTokens := 0
	otherTokens := 0

	if metrics != nil {
		tokensUsed = metrics.TotalTokens

		totalApprox := metrics.SystemTokens + metrics.ToolsTokens + metrics.WorkspaceFileTokens + metrics.ToolResultTokens + metrics.ChatTokens
		if totalApprox > 0 {
			factor := float64(metrics.PromptTokens) / float64(totalApprox)
			systemTokens = int(float64(metrics.SystemTokens) * factor)
			toolTokens = int(float64(metrics.ToolsTokens) * factor)
			toolResultTokens = int(float64(metrics.ToolResultTokens) * factor)
			filesTokens = int(float64(metrics.WorkspaceFileTokens) * factor)
			messageTokens = metrics.PromptTokens - systemTokens - toolTokens - toolResultTokens - filesTokens
			if messageTokens < 0 {
				messageTokens = 0
			}
		} else {
			messageTokens = metrics.PromptTokens
		}

		otherTokens = metrics.CompletionTokens
	}

	usedPercent := int(float64(tokensUsed) * 100.0 / float64(tokenLimit))
	tokensStr := tokenutils.FormatTokens(tokensUsed) + " / " + tokenutils.FormatTokens(tokenLimit) + " TOKENS"

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
	outputCells := int(float64(outputLimit) * float64(barLength) / float64(tokenLimit))
	if outputLimit > 0 && outputCells == 0 {
		outputCells = 1
	}

	renderedOutputCells := outputCells
	if totalUsedCells+renderedOutputCells > barLength {
		renderedOutputCells = barLength - totalUsedCells
	}

	unusedCells := barLength - totalUsedCells - renderedOutputCells
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
				kitex.Text(" CONTEXT RESOURCES"),
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
			kitex.If(renderedOutputCells > 0, func() kitex.Node {
				return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(c.border)}, kitex.Text(strings.Repeat("▒", renderedOutputCells)))
			}),
		),

		// Avail dynamic head
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				JustifyContent(style.JustifyBetween),
		},
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.subtle)}, kitex.Text("Available Context Window")),
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.muted)}, kitex.Text(fmt.Sprintf("%.1f%%", 100.0-float64(tokensUsed)*100.0/float64(tokenLimit)))),
		),

		// Output Reserved Limit
		func() kitex.Node {
			outputPct := float64(outputLimit) * 100.0 / float64(tokenLimit)
			outputStr := fmt.Sprintf("%.1fK", float64(outputLimit)/1024.0)
			return kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					JustifyContent(style.JustifyBetween).
					MarginBottom(1),
			},
				kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.subtle)}, kitex.Text("Reserved for Output")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
				},
					kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.subtle)}, kitex.Text(outputStr)),
					kitex.Box(kitex.BoxProps{Style: style.S().Foreground(c.muted)}, kitex.Text(fmt.Sprintf("(%.1f%%)", outputPct))),
				),
			)
		}(),

		// System Allocations
		metricSectionHeader(icon.CPU, "SYSTEM PROMPT & DEFINITIONS", c.info),
		metricRow(c.info, "System Instructions", systemPct, c),
		metricRow(c.magenta, "Tool Definitions", toolPct, c),

		// User Content Allocations
		kitex.Box(kitex.BoxProps{Style: style.S().MarginTop(1)},
			metricSectionHeader(icon.Robot, "CHAT HISTORY & CONTEXT", c.success),
		),
		metricRow(c.success, "Chat Messages", messagePct, c),
		metricRow(c.warning, "Tool Execution Results", toolResultPct, c),
		metricRow(c.error, "Workspace Files Context", filesPct, c),

		// Uncategorized
		kitex.Box(kitex.BoxProps{Style: style.S().MarginTop(1)},
			metricSectionHeader(kitex.Text(" "), "MODEL OUTPUTS", c.muted),
		),
		metricRow(c.subtle, "Agent Generations", otherPct, c),

		// Action Button
		components.Button(components.ButtonProps{
			Variant: components.ButtonOutline,
			Color:   components.ButtonInfo,
			Style:   style.S().Width(style.Percent(100)).JustifyContent(style.JustifyCenter).MarginTop(1),
			OnClick: func() {
				active.SetScreen("analytics")
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
