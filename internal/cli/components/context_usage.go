package components

import (
	"cmp"
	"fmt"
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

type categoryItem struct {
	glyph string
	color func(*theme.Scheme) color.Color
	label string
	count int
}

// ContextUsage is a trail functional component rendering a visual token usage grid
// and category breakdown table for /context.
var ContextUsage = trail.FC("ContextUsage", func(props store.ContextUsageData) trail.Node {
	scheme := theme.UseTheme()
	ctx := props.Context

	staticTokens := ctx.Breakdown.SystemPrompt + ctx.Breakdown.ContextFiles +
		ctx.Breakdown.SystemTools + ctx.Breakdown.CustomTools +
		ctx.Breakdown.MCPTools + ctx.Breakdown.Skills + ctx.Breakdown.Subagents
	convTokens := ctx.Breakdown.Summary + ctx.Breakdown.UserMessages +
		ctx.Breakdown.AgentResponses + ctx.Breakdown.ToolCalls
	reserveTokens := ctx.ReserveTokens
	freeTokens := ctx.FreeTokens

	const totalCells = 200
	const gridCols = 20
	const gridRows = 10

	var staticCells, convCells, reserveCells, freeCells int
	if ctx.MaxTokens > 0 {
		staticCells = int(float64(staticTokens)/float64(ctx.MaxTokens)*float64(totalCells) + 0.5)
		convCells = int(float64(convTokens)/float64(ctx.MaxTokens)*float64(totalCells) + 0.5)
		reserveCells = int(float64(reserveTokens)/float64(ctx.MaxTokens)*float64(totalCells) + 0.5)

		if staticTokens > 0 && staticCells == 0 {
			staticCells = 1
		}
		if convTokens > 0 && convCells == 0 {
			convCells = 1
		}
		if reserveTokens > 0 && reserveCells == 0 {
			reserveCells = 1
		}

		if staticCells > totalCells {
			staticCells = totalCells
		}
		if staticCells+convCells > totalCells {
			convCells = totalCells - staticCells
		}
		if staticCells+convCells+reserveCells > totalCells {
			reserveCells = totalCells - staticCells - convCells
		}
		freeCells = max(totalCells-staticCells-convCells-reserveCells, 0)
	}

	type cellInfo struct {
		glyph string
		col   color.Color
	}

	cells := make([]cellInfo, totalCells)
	for i := range totalCells {
		if i < staticCells {
			cells[i] = cellInfo{glyph: "⛁", col: scheme.Color.Agent.Thinking}
		} else if i < staticCells+convCells {
			cells[i] = cellInfo{glyph: "◉", col: scheme.Color.Agent.Generating}
		} else if i < staticCells+convCells+freeCells {
			cells[i] = cellInfo{glyph: "⛶", col: scheme.Color.Text.Secondary}
		} else {
			cells[i] = cellInfo{glyph: "⛝", col: scheme.Color.Text.Warning}
		}
	}

	gridRowNodes := make([]trail.Node, gridRows)
	for r := range gridRows {
		start := r * gridCols
		end := start + gridCols

		var spans []trail.Node
		var currentRun strings.Builder
		var currentColor color.Color

		for i := start; i < end; i++ {
			c := cells[i]
			if currentColor == nil {
				currentColor = c.col
			}
			if c.col != currentColor {
				spans = append(spans, trail.Span(trail.SpanProps{
					Style: style.S().Foreground(currentColor).WhiteSpace(style.WhiteSpacePre),
				}, trail.Text(currentRun.String())))
				currentRun.Reset()
				currentColor = c.col
			}
			if i > start {
				currentRun.WriteString(" ")
			}
			currentRun.WriteString(c.glyph)
		}
		if currentRun.Len() > 0 {
			spans = append(spans, trail.Span(trail.SpanProps{
				Style: style.S().Foreground(currentColor).WhiteSpace(style.WhiteSpacePre),
			}, trail.Text(currentRun.String())))
		}

		gridRowNodes[r] = trail.Box(trail.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow),
		}, spans...)
	}

	gridBox := trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			MarginRight(3),
	}, gridRowNodes...)

	// Right side: model name and token breakdown
	modelName := cmp.Or(strings.TrimSpace(props.ModelName), "default")
	totalPct := 0.0
	if ctx.MaxTokens > 0 {
		totalPct = float64(ctx.UsedTokens) / float64(ctx.MaxTokens) * 100.0
	}
	modelHeader := fmt.Sprintf("%s · %s/%s (%.1f%%)", modelName, format.Tokens(ctx.UsedTokens), format.Tokens(ctx.MaxTokens), totalPct)

	categoryHeader := "Token usage by category"
	if ctx.InputTokens == 0 {
		categoryHeader = "Estimated usage by category (awaiting generation)"
	}

	items := []categoryItem{
		{"⛁", func(s *theme.Scheme) color.Color { return s.Color.Agent.Thinking }, "System prompt", ctx.Breakdown.SystemPrompt},
		{"⛁", func(s *theme.Scheme) color.Color { return s.Color.Agent.Thinking }, "Context files", ctx.Breakdown.ContextFiles},
		{"⛁", func(s *theme.Scheme) color.Color { return s.Color.Agent.Thinking }, "System tools", ctx.Breakdown.SystemTools},
		{"⛁", func(s *theme.Scheme) color.Color { return s.Color.Agent.Thinking }, "Custom tools", ctx.Breakdown.CustomTools},
		{"⛁", func(s *theme.Scheme) color.Color { return s.Color.Agent.Thinking }, "MCP tools", ctx.Breakdown.MCPTools},
		{"⛁", func(s *theme.Scheme) color.Color { return s.Color.Agent.Thinking }, "Skills", ctx.Breakdown.Skills},
		{"⛁", func(s *theme.Scheme) color.Color { return s.Color.Agent.Thinking }, "Subagents", ctx.Breakdown.Subagents},
		{"◉", func(s *theme.Scheme) color.Color { return s.Color.Agent.Generating }, "Compacted summary", ctx.Breakdown.Summary},
		{"◉", func(s *theme.Scheme) color.Color { return s.Color.Agent.Generating }, "User messages", ctx.Breakdown.UserMessages},
		{"◉", func(s *theme.Scheme) color.Color { return s.Color.Agent.Generating }, "Agent responses", ctx.Breakdown.AgentResponses},
		{"◉", func(s *theme.Scheme) color.Color { return s.Color.Agent.Generating }, "Tool calls", ctx.Breakdown.ToolCalls},
		{"⛶", func(s *theme.Scheme) color.Color { return s.Color.Text.Secondary }, "Free space", freeTokens},
		{"⛝", func(s *theme.Scheme) color.Color { return s.Color.Text.Warning }, "Autocompact buffer", reserveTokens},
	}

	infoNodes := make([]trail.Node, 0, len(items)+2)
	infoNodes = append(infoNodes,
		trail.Box(trail.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow)},
			trail.Span(trail.SpanProps{
				Style: style.S().Bold(true).Foreground(scheme.Color.Text.Primary),
			}, trail.Text(modelHeader)),
		),
		trail.Box(trail.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).MarginTop(1),
		},
			trail.Span(trail.SpanProps{
				Style: style.S().Bold(true).Foreground(scheme.Color.Text.Primary),
			}, trail.Text(categoryHeader)),
		),
	)

	for _, item := range items {
		if item.count <= 0 {
			continue
		}
		pct := 0.0
		if ctx.MaxTokens > 0 {
			pct = float64(item.count) / float64(ctx.MaxTokens) * 100.0
		}
		rowText := fmt.Sprintf("%s: %s tokens (%.1f%%)", item.label, format.Tokens(item.count), pct)
		infoNodes = append(infoNodes, trail.Box(trail.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow),
		},
			trail.Span(trail.SpanProps{
				Style: style.S().Foreground(item.color(scheme)).WhiteSpace(style.WhiteSpacePre),
			}, trail.Text(item.glyph+" ")),
			trail.Span(trail.SpanProps{
				Style: style.S().Foreground(scheme.Color.Text.Secondary),
			}, trail.Text(rowText)),
		))
	}

	infoBox := trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn),
	}, infoNodes...)

	promptStyle := style.S().FlexDirection(style.FlexRow).Foreground(scheme.Color.Text.Primary)
	outStyle := style.S().FlexDirection(style.FlexRow).Foreground(scheme.Color.Text.Secondary)

	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn).MarginBottom(1),
	},
		trail.Box(trail.BoxProps{Style: promptStyle},
			trail.Span(trail.SpanProps{Style: style.S().Bold(true)}, trail.Text("> ")),
			trail.Span(trail.SpanProps{}, trail.Text("/context")),
		),
		trail.Box(trail.BoxProps{Style: outStyle},
			trail.Span(trail.SpanProps{Style: style.S().WhiteSpace(style.WhiteSpacePre)}, trail.Text("  ⎿  ")),
			trail.Span(trail.SpanProps{Style: style.S().Bold(true)}, trail.Text("Context Usage")),
		),
		trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				MarginLeft(5),
		}, gridBox, infoBox),
	)
})
