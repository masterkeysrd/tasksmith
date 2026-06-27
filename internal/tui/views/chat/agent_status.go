package chat

import (
	"fmt"
	"image/color"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/masterkeysrd/tasksmith/internal/tui/tokenutils"
)

func renderAgentStatus(t *theme.Scheme, sending bool, thinkingTime int, lastFinishedTime int, currentDots string, runPromptTokens, runCompletionTokens, runTotalTokens int, isGenerating bool, phase string, activeTip string) kitex.Node {
	if t == nil {
		return nil
	}
	blueColor := t.Color.Surface.Info
	greenColor := t.Color.Surface.Success
	timeStr := fmt.Sprintf("[%02d:%02d]", thinkingTime/60, thinkingTime%60)

	upColor := t.Color.Text.Tertiary
	downColor := t.Color.Text.Tertiary
	if sending {
		if isGenerating {
			downColor = t.Color.Surface.Success // highlight down when streaming text
		} else {
			upColor = t.Color.Surface.Info // highlight up when processing/waiting
		}
	}

	if sending {
		var statusText string
		var statusColor color.Color
		switch phase {
		case "thinking":
			statusText = "Thinking"
			statusColor = blueColor
		case "answering":
			statusText = "Answering"
			statusColor = greenColor
		default: // "processing"
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
		dotsStyle := style.S().Foreground(statusColor).Width(style.Cells(3))
		labelStyle := style.S().Foreground(statusColor).Bold(true)
		timeStyle := style.S().Foreground(t.Color.Text.Tertiary)

		var cumNodes []kitex.Node
		if runPromptTokens > 0 || runCompletionTokens > 0 || runTotalTokens > 0 {
			if runPromptTokens > 0 || runCompletionTokens > 0 {
				cumNodes = append(cumNodes, kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).Foreground(t.Color.Text.Tertiary),
				},
					kitex.Text("("),
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(upColor)}, kitex.Text(fmt.Sprintf("↑%s", tokenutils.FormatTokens(runPromptTokens)))),
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(downColor)}, kitex.Text(fmt.Sprintf("↓%s", tokenutils.FormatTokens(runCompletionTokens)))),
					kitex.Text(")"),
				))
			} else {
				cumNodes = append(cumNodes, kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text(fmt.Sprintf("(%s TOTAL)", tokenutils.FormatTokens(runTotalTokens)))))
			}
		}

		statusRow := kitex.Box(kitex.BoxProps{Style: containerStyle},
			kitex.Box(kitex.BoxProps{Style: dotsStyle}, kitex.Text(currentDots)),
			kitex.Box(kitex.BoxProps{Style: labelStyle}, kitex.Text(statusText)),
			kitex.Box(kitex.BoxProps{Style: timeStyle}, kitex.Text(timeStr)),
			kitex.If(len(cumNodes) > 0, func() kitex.Node { return cumNodes[0] }),
		)

		if activeTip != "" {
			tipStyle := style.S().
				Foreground(t.Color.Text.Tertiary).
				Italic(true).
				MarginTop(1).
				PaddingLeft(6)

			return kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn),
			},
				statusRow,
				kitex.Box(kitex.BoxProps{Style: tipStyle}, kitex.Text("Tip: "+activeTip)),
			)
		}

		return statusRow
	}
	if lastFinishedTime >= 0 {
		finishedTimeStr := fmt.Sprintf("[%02d:%02d]", lastFinishedTime/60, lastFinishedTime%60)
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
		if runPromptTokens > 0 || runCompletionTokens > 0 || runTotalTokens > 0 {
			var tokenStr string
			if runPromptTokens > 0 || runCompletionTokens > 0 {
				tokenStr = fmt.Sprintf("(↑%s ↓%s)", tokenutils.FormatTokens(runPromptTokens), tokenutils.FormatTokens(runCompletionTokens))
			} else {
				tokenStr = fmt.Sprintf("(%s TOTAL)", tokenutils.FormatTokens(runTotalTokens))
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
}
