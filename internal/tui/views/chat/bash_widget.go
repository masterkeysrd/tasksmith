package chat

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/geom"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/core/preview"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// BashToolWidget renders bash execution status, command input, and formatted stdout/stderr output.
var BashToolWidget = kitex.FC("BashToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	isOpen, setIsOpen := kitex.UseState(true)

	docFunc := kitex.UseDocument()
	doc := docFunc()

	var initialSize geom.Size
	if doc != nil {
		if view := doc.DefaultView(); view != nil {
			initialSize = view.ViewportSize()
		}
	}

	size, setSize := kitex.UseState(initialSize)
	elRef := kitex.UseRef[dom.Node](nil)
	measuredWidth, setMeasuredWidth := kitex.UseState(0)

	kitex.UseEffectCleanup(func() func() {
		if doc == nil {
			return nil
		}

		updateSize := func() {
			if view := doc.DefaultView(); view != nil {
				// 1. Update viewport size
				currSize := view.ViewportSize()
				if currSize != size() {
					setSize(currSize)
				}
				// 2. Update widget layout width bounds
				if elRef.Current != nil {
					if rect, ok := view.GetBoundingClientRect(elRef.Current.(dom.Element)); ok {
						if rect.Size.Width != measuredWidth() {
							setMeasuredWidth(rect.Size.Width)
						}
					}
				}
			}
		}

		// Sync sizes after render/layout runs
		updateSize()

		sub := doc.AddEventListener(event.EventResize, func(ev event.Event) {
			updateSize()
		})

		return func() {
			sub.Cancel()
		}
	}, []any{doc, elRef.Current})

	viewportWidth := size().Width
	fallbackWrapWidth := getDynamicWrapWidth(viewportWidth)

	var wrapWidth int
	if measuredWidth() > 0 {
		wrapWidth = measuredWidth() - 2
		if wrapWidth < 20 {
			wrapWidth = 20
		}
	} else {
		wrapWidth = fallbackWrapWidth
	}

	tc := props.ToolCall
	tm := props.ToolMessage

	isAutoApproved := false
	if tm != nil && tm.GetMetadata() != nil {
		if val, ok := tm.GetMetadata()["auto_approved"].(bool); ok && val {
			isAutoApproved = true
		} else if val, ok := tm.GetMetadata()["auto_approved"].(string); ok && val == "true" {
			isAutoApproved = true
		}
	}

	// Extract command and description from args
	command := ""
	description := ""
	if tc != nil && len(tc.Args) > 0 {
		if cmdVal, ok := tc.Args["command"]; ok {
			command, _ = cmdVal.(string)
		}
		if descVal, ok := tc.Args["description"]; ok {
			description, _ = descVal.(string)
		}
	}

	containerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100)).
		MaxWidth(style.Percent(100)).
		MinWidth(style.Percent(0)).
		Overflow(style.OverflowHidden)

	headerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		JustifyContent(style.JustifyBetween).
		Padding(0, 1).
		Width(style.Percent(100)).
		MaxWidth(style.Percent(100)).
		Overflow(style.OverflowHidden)

	bodyStyle := style.S().
		Padding(1).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Gap(1).
		Width(style.Percent(100)).
		MaxWidth(style.Percent(100)).
		MinWidth(style.Percent(0)).
		Overflow(style.OverflowHidden)

	var iconNode kitex.Node
	var statusLabel string
	var headerBg color.Color
	var headerFg color.Color
	var borderCol color.Color

	// Determine status and widget color
	isFinished := tm != nil && (tm.GetMetadata() == nil || tm.GetMetadata()["status"] != "running")
	hasErr := false
	statusMsg := ""

	if isFinished {
		if tm.IsError {
			hasErr = true
		}
		// Try parsing exit code from structured content
		var exitCode int
		if tm.StructuredContent != nil {
			if m, ok := tm.StructuredContent.(map[string]any); ok {
				if ecVal, ok := m["exitCode"]; ok {
					switch ec := ecVal.(type) {
					case float64:
						exitCode = int(ec)
					case int:
						exitCode = ec
					case int64:
						exitCode = int(ec)
					}
					if exitCode != 0 {
						hasErr = true
					}
				}
				if statusVal, ok := m["status"]; ok {
					statusMsg, _ = statusVal.(string)
				}
			}
		}

		if statusMsg == "running" {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Info)
			statusLabel = "BASH RUNNING IN BACKGROUND"
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Surface.Info
			borderCol = t.Color.Surface.Info
		} else if hasErr {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			statusLabel = "BASH ERROR"
			if statusMsg != "" {
				statusLabel += fmt.Sprintf(" (%s)", strings.ToUpper(statusMsg))
			}
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Text.Error
			borderCol = t.Color.Text.Error
		} else {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.FontAwesomeTerminal)
			statusLabel = "BASH SUCCESS"
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Surface.Success
			borderCol = t.Color.Surface.Success
		}
	} else {
		iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
		statusLabel = "RUNNING BASH COMMAND"
		headerBg = t.Color.Surface.BaseFocus
		headerFg = t.Color.Surface.Info
		borderCol = t.Color.Surface.Info
	}

	if t != nil {
		containerStyle = containerStyle.
			Border(true, style.SingleBorder(), borderCol).
			Background(t.Color.Surface.BaseHover)

		headerStyle = headerStyle.
			Background(headerBg).
			Foreground(headerFg)

		bodyStyle = bodyStyle.
			Background(t.Color.Surface.BaseHover)
	}

	return kitex.Fragment(
		kitex.Box(kitex.BoxProps{Ref: elRef, Style: containerStyle},
			components.Button(components.ButtonProps{
				Variant: components.ButtonText,
				Color:   components.ButtonBase,
				Style:   headerStyle,
				OnClick: func() {
					setIsOpen(!isOpen())
				},
			},
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						AlignItems(style.AlignCenter).
						Gap(1),
				},
					iconNode,
					kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(" "+statusLabel)),
				),
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						AlignItems(style.AlignCenter).
						Gap(2),
				},
					kitex.If(isAutoApproved, func() kitex.Node {
						return kitex.Span(kitex.SpanProps{
							Style: style.S().Foreground(t.Color.Text.Magenta).Bold(true),
						}, kitex.Text("[󰚩 Auto-Approved]"))
					}),
					kitex.If(isFinished, func() kitex.Node {
						var label string
						if isOpen() {
							label = "▲ COLLAPSE"
						} else {
							label = "▼ EXPAND"
						}
						var textCol color.Color
						if t != nil {
							textCol = t.Color.Text.Secondary
						}
						return kitex.Span(kitex.SpanProps{
							Style: style.S().Foreground(textCol),
						}, kitex.Text(label))
					}),
				),
			),
			kitex.If(isOpen(), func() kitex.Node {
				return kitex.Box(kitex.BoxProps{Style: bodyStyle},
					// Description
					kitex.If(description != "", func() kitex.Node {
						var textCol color.Color
						if t != nil {
							textCol = t.Color.Text.Primary
						}
						return kitex.Box(kitex.BoxProps{
							Style: style.S().
								Foreground(textCol).
								Italic(true),
						}, kitex.Text(description))
					}),
					// Input: codeblock without header or borders
					kitex.If(command != "", func() kitex.Node {
						var cbStyle style.Style
						if t != nil {
							cbStyle = style.S().Background(t.Color.Surface.Base)
						}
						return kitex.Box(kitex.BoxProps{
							Style: style.S().
								Width(style.Percent(100)).
								MinWidth(style.Percent(0)),
						},
							components.CodeBlock(components.CodeBlockProps{
								Code:       "$ " + command,
								Lang:       "bash",
								HideHeader: true,
								Wrap:       true,
								Compact:    true,
								Style:      cbStyle.Padding(1),
							}),
						)
					}),
					// Output (stdout/stderr) without outer header or borders
					kitex.If(tm != nil, func() kitex.Node {
						originalOutText := getToolOutput(tm.Content)
						outText := originalOutText
						return kitex.Fragment(
							kitex.If(strings.TrimSpace(outText) != "", func() kitex.Node {
								lines := strings.Split(outText, "\n")
								isTruncated := len(lines) > 10

								var inlineText string
								if isTruncated {
									inlineText = strings.Join(lines[len(lines)-10:], "\n")
								} else {
									inlineText = outText
								}

								parts := strings.Split(inlineText, "[stderr]\n")
								var elements []kitex.Node

								var textCol color.Color
								if t != nil {
									textCol = t.Color.Text.Primary
								}

								outputContainerStyle := style.S().
									Display(style.DisplayFlex).
									FlexDirection(style.FlexColumn).
									Flex(0, 1, style.Auto).
									Width(style.Percent(100)).
									MaxWidth(style.Percent(100)).
									MinWidth(style.Percent(0)).
									Overflow(style.OverflowHidden)

								// First part is stdout
								stdoutText := strings.TrimSpace(parts[0])
								if stdoutText != "" {
									stdoutText = strings.ReplaceAll(stdoutText, "\t", "    ")
									elements = append(elements, kitex.Box(kitex.BoxProps{
										Style: style.S().
											Foreground(textCol).
											Width(style.Cells(wrapWidth)).
											MaxWidth(style.Cells(wrapWidth)).
											MinWidth(style.Percent(0)).
											WhiteSpace(style.WhiteSpacePre).
											ScrollbarX(true).
											OverflowX(style.OverflowAuto),
									}, kitex.Text(stdoutText)))
								}

								// Subsequent parts are stderr
								if len(parts) > 1 {
									stderrText := strings.TrimSpace(strings.Join(parts[1:], ""))
									if stderrText != "" {
										stderrText = strings.ReplaceAll(stderrText, "\t", "    ")
										elements = append(elements, kitex.Box(kitex.BoxProps{
											Style: style.S().
												Foreground(t.Color.Text.Error).
												Width(style.Cells(wrapWidth)).
												MaxWidth(style.Cells(wrapWidth)).
												MinWidth(style.Percent(0)).
												WhiteSpace(style.WhiteSpacePre).
												ScrollbarX(true).
												OverflowX(style.OverflowAuto).
												MarginTop(1),
										}, kitex.Text("[stderr]\n"+stderrText)))
									}
								}

								var buttonNode kitex.Node
								if isTruncated {
									buttonNode = components.Button(components.ButtonProps{
										Variant: components.ButtonText,
										Color:   components.ButtonBase,
										Style: style.S().
											Foreground(t.Color.Surface.Info).
											MarginTop(1).
											Bold(true),
										OnClick: func() {
											if props.OnViewPreview != nil {
												props.OnViewPreview(
													"Command Output",
													preview.DefaultTextPreview{Text: originalOutText},
												)
											}
										},
									}, kitex.Fragment(
										kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.FontAwesomeTerminal),
										kitex.Text(" VIEW FULL OUTPUT"),
									),
									)
								}

								return kitex.Box(kitex.BoxProps{
									Style: style.S().
										Display(style.DisplayFlex).
										FlexDirection(style.FlexColumn).
										Width(style.Percent(100)).
										MaxWidth(style.Percent(100)).
										MinWidth(style.Percent(0)).
										Overflow(style.OverflowHidden),
								},
									kitex.Box(kitex.BoxProps{Style: outputContainerStyle}, elements...),
									buttonNode,
								)
							}),
						)
					}),
				)
			}),
		),
	)
})

func getDynamicWrapWidth(viewportWidth int) int {
	if viewportWidth <= 0 {
		return 80
	}
	// Conservative estimate: assume 34 cells are taken by the sidebar if open.
	availableWidth := viewportWidth
	if availableWidth > 34 {
		availableWidth -= 34
	}
	// Apply bubble 90% MaxWidth constraint
	bubbleWidth := int(float64(availableWidth) * 0.90)
	// Subtract layout padding and margins (approx 6 cells)
	wrapWidth := bubbleWidth - 6
	if wrapWidth < 20 {
		return 20 // Sane lower boundary
	}
	return wrapWidth
}

func wrapLongLines(text string, maxLen int) string {
	text = strings.ReplaceAll(text, "\r", "")
	lines := strings.Split(text, "\n")
	var result []string

	for _, line := range lines {
		if len(line) <= maxLen {
			result = append(result, line)
			continue
		}

		// Wrap this line
		var wrappedLine strings.Builder
		runes := []rune(line)
		start := 0
		for start < len(runes) {
			// If remaining part fits, write it and finish
			if len(runes)-start <= maxLen {
				wrappedLine.WriteString(string(runes[start:]))
				break
			}

			// Search for the last space/tab within maxLen characters
			spaceIdx := -1
			for i := start + maxLen; i > start; i-- {
				if runes[i] == ' ' || runes[i] == '\t' {
					spaceIdx = i
					break
				}
			}

			if spaceIdx != -1 {
				// Wrap at the space
				wrappedLine.WriteString(string(runes[start:spaceIdx]))
				wrappedLine.WriteRune('\n')
				// Skip the space itself
				start = spaceIdx + 1
			} else {
				// No space found (unbreakable word). Break at maxLen.
				wrappedLine.WriteString(string(runes[start : start+maxLen]))
				wrappedLine.WriteRune('\n')
				start += maxLen
			}
		}
		result = append(result, wrappedLine.String())
	}

	return strings.Join(result, "\n")
}
