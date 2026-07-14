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

	expanded, setExpanded := kitex.UseState(false)

	const previewLines = 10
	var previewText string
	var detailsText string
	var hasMore bool

	if strings.TrimSpace(props.Content) != "" {
		previewText, detailsText, hasMore = getPreviewAndDetails(props.Content, previewLines, expanded())
	}

	var fg color.Color
	if t != nil {
		fg = t.Color.Text.Tertiary
	}

	bodyStyle := style.S().
		Padding(1).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn)

	isExp := expanded()
	return components.Accordion(components.AccordionProps{
		Color:    components.PaperHover,
		Variant:  components.PaperOutlined,
		Expanded: &isExp,
		OnChange: func(val bool) {
			setExpanded(val)
		},
	},
		components.AccordionSummary(components.AccordionSummaryProps{
			HideExpandIcon: !hasMore,
			EndContent: kitex.If(hasMore, func() kitex.Node {
				var fg color.Color
				if t != nil {
					fg = t.Color.Text.Secondary
				}
				moreLinesCount := countRemainingLines(props.Content, previewLines)
				label := fmt.Sprintf("%d more lines", moreLinesCount)
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
		kitex.If(hasMore && expanded(), func() kitex.Node {
			return components.AccordionDetails(components.AccordionDetailsProps{Style: bodyStyle},
				kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(fg).WhiteSpace(style.WhiteSpacePreWrap),
				}, kitex.Text(detailsText)),
			)
		}),
	)
})

func getPreviewAndDetails(s string, maxLines int, needDetails bool) (preview string, details string, hasMore bool) {
	pos := 0
	count := 0
	for count < maxLines {
		idx := strings.IndexByte(s[pos:], '\n')
		if idx == -1 {
			return strings.TrimSpace(s), "", false
		}
		pos += idx + 1
		count++
	}

	// Check if there is actual content after the maxLines-th newline
	if strings.TrimSpace(s[pos:]) == "" {
		return strings.TrimSpace(s), "", false
	}

	preview = strings.TrimSuffix(s[:pos], "\n")
	hasMore = true
	if needDetails {
		details = strings.TrimSpace(s[pos:])
	}
	return preview, details, hasMore
}

func countRemainingLines(s string, maxLines int) int {
	pos := 0
	count := 0
	for count < maxLines {
		idx := strings.IndexByte(s[pos:], '\n')
		if idx == -1 {
			return 0
		}
		pos += idx + 1
		count++
	}

	if strings.TrimSpace(s[pos:]) == "" {
		return 0
	}

	remaining := 0
	sub := s[pos:]
	for {
		idx := strings.IndexByte(sub, '\n')
		if idx == -1 {
			if strings.TrimSpace(sub) != "" {
				remaining++
			}
			break
		}
		remaining++
		sub = sub[idx+1:]
	}
	return remaining
}

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
