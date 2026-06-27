package chat

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type CollapsibleThinkingProps struct {
	Content  string
	Duration time.Duration
	Tokens   int
}

var CollapsibleThinking = kitex.FC("CollapsibleThinking", func(props CollapsibleThinkingProps) kitex.Node {
	t := theme.UseTheme()

	lines := strings.Split(strings.TrimSpace(props.Content), "\n")
	const previewLines = 10
	hasMore := len(lines) > previewLines

	var fg color.Color
	if t != nil {
		fg = t.Color.Text.Tertiary
	}

	previewText := ""
	detailsText := ""

	if hasMore {
		previewText = strings.Join(lines[:previewLines], "\n")
		detailsText = strings.Join(lines[previewLines:], "\n")
	} else {
		previewText = strings.TrimSpace(props.Content)
	}

	bodyStyle := style.S().
		Padding(1).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn)

	return components.Accordion(components.AccordionProps{
		Color:   components.PaperHover,
		Variant: components.PaperOutlined,
	},
		components.AccordionSummary(components.AccordionSummaryProps{
			HideExpandIcon: !hasMore,
			EndContent: kitex.If(hasMore, func() kitex.Node {
				var fg color.Color
				if t != nil {
					fg = t.Color.Text.Secondary
				}
				label := fmt.Sprintf("%d more lines", len(lines)-previewLines)
				return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(fg)}, kitex.Text(label))
			}),
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Gap(1),
			},
				kitex.If(t != nil, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Tertiary)}, kitex.Text("≈"))
				}),
				kitex.If(t != nil, func() kitex.Node {
					titleStr := "Thought"
					durStr := formatThinkingDuration(props.Duration)
					if durStr != "" {
						titleStr += " for " + durStr
					}
					if props.Tokens > 0 {
						if durStr != "" {
							titleStr += ", "
						} else {
							titleStr += " for "
						}
						titleStr += fmt.Sprintf("%d tokens", props.Tokens)
					}
					return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text(titleStr))
				}),
			),
		),
		// Preview: first N lines, always visible.
		components.AccordionPreview(components.AccordionPreviewProps{Style: bodyStyle},
			kitex.Box(kitex.BoxProps{
				Style: style.S().Foreground(fg).WhiteSpace(style.WhiteSpacePreWrap),
			}, kitex.Text(previewText)),
		),
		// Details: overflow lines, only visible when expanded.
		kitex.If(hasMore, func() kitex.Node {
			return components.AccordionDetails(components.AccordionDetailsProps{Style: bodyStyle},
				kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(fg).WhiteSpace(style.WhiteSpacePreWrap),
				}, kitex.Text(detailsText)),
			)
		}),
	)
})

func formatThinkingDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	secs := int(d.Round(time.Second).Seconds())
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	mins := secs / 60
	remSecs := secs % 60
	if remSecs == 0 {
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%dm %ds", mins, remSecs)
}
