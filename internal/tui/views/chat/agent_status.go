package chat

import (
	"fmt"
	"image/color"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/masterkeysrd/tasksmith/internal/tui/tokenutils"
)

// AgentStatusProps defines the properties for the AgentStatus widget.
type AgentStatusProps struct {
	Sending             bool
	ThinkingTime        int
	LastFinishedTime    int
	RunPromptTokens     int
	RunCompletionTokens int
	RunTotalTokens      int
	IsGenerating        bool
	Phase               string
	ActiveTip           string
}

// AgentStatus renders the agent thinking/streaming/completed status bar.
var AgentStatus = kitex.FC("AgentStatus", func(props AgentStatusProps) kitex.Node {
	t := theme.UseTheme()
	if t == nil {
		return nil
	}

	blueColor := t.Color.Surface.Info
	greenColor := t.Color.Surface.Success
	timeStr := fmt.Sprintf("[%02d:%02d]", props.ThinkingTime/60, props.ThinkingTime%60)

	upColor := t.Color.Text.Tertiary
	downColor := t.Color.Text.Tertiary
	if props.Sending {
		if props.IsGenerating {
			downColor = t.Color.Surface.Success
		} else {
			upColor = t.Color.Surface.Info
		}
	}

	if props.Sending {
		var statusText string
		var statusColor color.Color
		switch props.Phase {
		case "thinking":
			statusText = "Thinking"
			statusColor = blueColor
		case "answering":
			statusText = "Answering"
			statusColor = greenColor
		default:
			statusText = "Processing"
			statusColor = t.Color.Text.Tertiary
		}

		containerStyle := style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			Gap(1).
			PaddingLeft(2).
			Foreground(statusColor)
		dotsStyle := style.S().Foreground(statusColor).Width(style.Cells(5))
		labelStyle := style.S().Foreground(statusColor).Bold(true)
		timeStyle := style.S().Foreground(t.Color.Text.Tertiary)

		var cumNodes []kitex.Node
		if props.RunPromptTokens > 0 || props.RunCompletionTokens > 0 || props.RunTotalTokens > 0 {
			if props.RunPromptTokens > 0 || props.RunCompletionTokens > 0 {
				cumNodes = append(cumNodes, kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).Foreground(t.Color.Text.Tertiary),
				},
					kitex.Text("("),
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(upColor)}, kitex.Text(fmt.Sprintf("↑%s", tokenutils.FormatTokens(props.RunPromptTokens)))),
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(downColor)}, kitex.Text(fmt.Sprintf("↓%s", tokenutils.FormatTokens(props.RunCompletionTokens)))),
					kitex.Text(")"),
				))
			} else {
				cumNodes = append(cumNodes, kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text(fmt.Sprintf("(%s TOTAL)", tokenutils.FormatTokens(props.RunTotalTokens)))))
			}
		}

		statusRow := kitex.Box(kitex.BoxProps{Style: containerStyle},
			kitex.Box(kitex.BoxProps{Style: dotsStyle}, components.Pulse(components.PulseProps{
				Stages:    []string{"○", "⊙", "◎", "◉", "●"},
				Count:     3,
				LoopStyle: components.LoopBreathe,
				Interval:  120 * time.Millisecond,
			})),
			kitex.Box(kitex.BoxProps{Style: labelStyle}, kitex.Text(statusText)),
			kitex.Box(kitex.BoxProps{Style: timeStyle}, kitex.Text(timeStr)),
			kitex.If(len(cumNodes) > 0, func() kitex.Node { return cumNodes[0] }),
		)

		if props.ActiveTip != "" {
			tipStyle := style.S().
				Foreground(t.Color.Text.Tertiary).
				Italic(true).
				MarginTop(0).
				PaddingLeft(3) // Slightly indented relative to the 2-cell statusRow padding

			return kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn),
			},
				statusRow,
				kitex.Box(kitex.BoxProps{Style: tipStyle}, kitex.Text("⤷ Tip: "+props.ActiveTip)),
			)
		}

		return statusRow
	}

	if props.LastFinishedTime >= 0 {
		finishedTimeStr := fmt.Sprintf("[%02d:%02d]", props.LastFinishedTime/60, props.LastFinishedTime%60)
		containerStyle := style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			Gap(1).
			PaddingLeft(2).
			Foreground(t.Color.Text.Secondary)
		checkStyle := style.S().Foreground(greenColor)
		labelStyle := style.S().Foreground(t.Color.Text.Secondary)
		timeStyle := style.S().Foreground(t.Color.Text.Secondary)

		var cumNodes []kitex.Node
		if props.RunPromptTokens > 0 || props.RunCompletionTokens > 0 || props.RunTotalTokens > 0 {
			var tokenStr string
			if props.RunPromptTokens > 0 || props.RunCompletionTokens > 0 {
				tokenStr = fmt.Sprintf("(↑%s ↓%s)", tokenutils.FormatTokens(props.RunPromptTokens), tokenutils.FormatTokens(props.RunCompletionTokens))
			} else {
				tokenStr = fmt.Sprintf("(%s TOTAL)", tokenutils.FormatTokens(props.RunTotalTokens))
			}
			cumNodes = append(cumNodes, kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(" "+tokenStr)))
		}

		return kitex.Box(kitex.BoxProps{Style: containerStyle},
			kitex.Box(kitex.BoxProps{Style: checkStyle}, icon.Checkmark),
			kitex.Box(kitex.BoxProps{Style: labelStyle}, kitex.Text("Agent completed in")),
			kitex.Box(kitex.BoxProps{Style: timeStyle}, kitex.Text(finishedTimeStr)),
			kitex.If(len(cumNodes) > 0, func() kitex.Node { return cumNodes[0] }),
		)
	}

	return nil
})
