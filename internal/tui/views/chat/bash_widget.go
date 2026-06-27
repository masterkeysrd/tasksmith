package chat

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// BashToolWidget renders bash execution status, command input, and formatted stdout/stderr output.
var BashToolWidget = kitex.FC("BashToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	isOpen, setIsOpen := kitex.UseState(true)
	showModal, setShowModal := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

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
		iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
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
		kitex.Box(kitex.BoxProps{Style: containerStyle},
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
						outText := getToolOutput(tm.Content)
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
											Width(style.Percent(100)).
											MinWidth(style.Percent(0)).
											WhiteSpace(style.WhiteSpacePreWrap),
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
												Width(style.Percent(100)).
												MinWidth(style.Percent(0)).
												WhiteSpace(style.WhiteSpacePreWrap).
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
											setShowModal(true)
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
		components.Modal(components.ModalProps{
			IsOpen: showModal(),
			Title:  kitex.Text("Command Output"),
			OnClose: func() {
				setShowModal(false)
			},
		},
			kitex.If(showModal(), func() kitex.Node {
				outText := getToolOutput(tm.Content)
				parts := strings.Split(outText, "[stderr]\n")
				var elements []kitex.Node

				var textCol color.Color
				if t != nil {
					textCol = t.Color.Text.Primary
				}

				outputContainerStyle := style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					Background(t.Color.Surface.BaseHover).
					Padding(1).
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
							Width(style.Percent(100)).
							MinWidth(style.Percent(0)).
							WhiteSpace(style.WhiteSpacePreWrap),
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
								Width(style.Percent(100)).
								MinWidth(style.Percent(0)).
								WhiteSpace(style.WhiteSpacePreWrap).
								MarginTop(1),
						}, kitex.Text("[stderr]\n"+stderrText)))
					}
				}

				return kitex.Box(kitex.BoxProps{Style: outputContainerStyle}, elements...)
			}),
		),
	)
})
