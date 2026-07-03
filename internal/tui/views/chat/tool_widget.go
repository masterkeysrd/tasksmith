package chat

import (
	"encoding/json"
	"fmt"
	"image/color"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/preview"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// ToolExecutionProps are the shared props for all tool execution widgets.
type ToolExecutionProps struct {
	ToolCall         *message.ToolCall
	ToolMessage      *message.Tool
	OnViewFullOutput func(title, cachedPath string)
	OnViewPreview    func(title string, p preview.ToolPreview)
}

func toolPulse() kitex.Node {
	return components.Pulse(components.PulseProps{
		Stages:    []string{"○", "⊙", "◎", "◉", "●"},
		Count:     1,
		LoopStyle: components.LoopBreathe,
		Interval:  120 * time.Millisecond,
	})
}

// DeniedToolWidget renders a denied tool call badge.
var DeniedToolWidget = kitex.FC("DeniedToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	if t == nil {
		return nil
	}

	tc := props.ToolCall
	tm := props.ToolMessage

	var denyReason string
	if tm != nil {
		meta := tm.GetMetadata()
		if meta != nil {
			if val, ok := meta["deny_reason"].(string); ok {
				denyReason = val
			}
		}
		if denyReason == "" {
			outText := getToolOutput(tm.Content)
			if strings.HasPrefix(outText, "Authorization denied by user") {
				if idx := strings.Index(outText, `": `); idx != -1 {
					denyReason = outText[idx+3:]
				}
			}
		}
	}

	containerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		JustifyContent(style.JustifyBetween).
		Padding(0, 1).
		Border(true, style.SingleBorder(), t.Color.Text.Error).
		Background(t.Color.Surface.BaseHover).
		Width(style.Percent(100))

	label := fmt.Sprintf("DENIED: %s", tc.Name)
	if denyReason != "" {
		label = fmt.Sprintf("DENIED: %s (%s)", tc.Name, denyReason)
	}

	return kitex.Box(kitex.BoxProps{Style: containerStyle},
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Gap(1),
		},
			kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error),
			kitex.Span(kitex.SpanProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Error)}, kitex.Text(label)),
		),
	)
})

// ToolExecution dispatches to the correct tool widget based on tool name, and
// wraps it with an auto-approved badge when applicable.
var ToolExecution = kitex.FC("ToolExecution", func(props ToolExecutionProps) kitex.Node {
	var node kitex.Node

	isDenied := false
	if props.ToolMessage != nil {
		meta := props.ToolMessage.GetMetadata()
		if meta != nil {
			if _, ok := meta["deny_reason"].(string); ok {
				isDenied = true
			}
		}
		if !isDenied {
			outText := getToolOutput(props.ToolMessage.Content)
			if strings.HasPrefix(outText, "Authorization denied by user") {
				isDenied = true
			}
		}
	}

	if isDenied {
		node = DeniedToolWidget(props)
	} else if props.ToolCall != nil {
		switch props.ToolCall.Name {
		case "bash":
			node = BashToolWidget(props)
		case "view":
			node = ViewToolWidget(props)
		case "ls":
			node = LsToolWidget(props)
		case "glob":
			node = GlobToolWidget(props)
		case "lsp_diagnostics":
			node = LspDiagnosticsToolWidget(props)
		case "lsp_restart":
			node = LspRestartToolWidget(props)
		case "lsp_symbols":
			node = LspSymbolsToolWidget(props)
		case "lsp_inspect":
			node = LspInspectToolWidget(props)
		case "grep":
			node = GrepToolWidget(props)
		case "write":
			node = WriteToolWidget(props)
		case "edit":
			node = EditToolWidget(props)
		case "multi_edit":
			node = MultiEditToolWidget(props)
		case "remove":
			node = RemoveToolWidget(props)
		case "tasks":
			node = TasksToolWidget(props)
		case "web_search":
			node = WebSearchToolWidget(props)
		case "web_fetch":
			node = WebFetchToolWidget(props)
		case "download":
			node = DownloadToolWidget(props)
		case "fetch":
			node = FetchToolWidget(props)
		case "activate_skill":
			node = ActivateSkillToolWidget(props)
		case "todos":
			node = TodosToolWidget(props)
		default:
			node = GenericToolWidget(props)
		}
	}

	if node == nil {
		return nil
	}

	t := theme.UseTheme()
	if t == nil {
		return node
	}

	isAutoApproved := false
	if props.ToolMessage != nil {
		meta := props.ToolMessage.GetMetadata()
		log.Info(fmt.Sprintf("[TUI] ToolExecution for %q: metadata=%+v", props.ToolCall.Name, meta))
		if meta != nil {
			if val, ok := meta["auto_approved"].(bool); ok && val {
				isAutoApproved = true
			} else if val, ok := meta["auto_approved"].(string); ok && val == "true" {
				isAutoApproved = true
			}
		}
	} else {
		log.Info(fmt.Sprintf("[TUI] ToolExecution for %q: ToolMessage is nil", props.ToolCall.Name))
	}

	if isAutoApproved && props.ToolCall.Name != "bash" && props.ToolCall.Name != "tasks" && props.ToolCall.Name != "todos" {
		return kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				JustifyContent(style.JustifyBetween).
				Width(style.Percent(100)),
		},
			node,
			kitex.Span(kitex.SpanProps{
				Style: style.S().Foreground(t.Color.Text.Magenta).Bold(true).MarginRight(1),
			}, kitex.Text("[󰚩 Auto-Approved]")),
		)
	}

	return node
})

// GenericToolWidget is a fallback collapsible widget for tool calls that have
// no dedicated widget.
var GenericToolWidget = kitex.FC("genericToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	isOpen, setIsOpen := kitex.UseState(true)
	hasAutoCollapsed, setHasAutoCollapsed := kitex.UseState(false)
	showFullOutput, setShowFullOutput := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

	if tm != nil && !tm.IsError && !hasAutoCollapsed() {
		setIsOpen(false)
		setHasAutoCollapsed(true)
	}

	var outText string
	if tm != nil {
		outText = getToolOutput(tm.Content)
	}

	var argsStr string
	if len(tc.Args) > 0 {
		if data, err := json.Marshal(tc.Args); err == nil {
			argsStr = string(data)
		}
	}

	containerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100)).
		MaxWidth(style.Percent(100)).
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
		Width(style.Percent(100)).
		MaxWidth(style.Percent(100)).
		Overflow(style.OverflowHidden)

	var iconNode kitex.Node
	var statusLabel string
	var headerBg color.Color
	var headerFg color.Color
	var borderCol color.Color

	if t != nil {
		if tm == nil {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
			statusLabel = fmt.Sprintf("RUNNING TOOL: %s", tc.Name)
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Surface.Info
			borderCol = t.Color.Surface.Info
		} else if tm.IsError {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			statusLabel = fmt.Sprintf("TOOL ERROR: %s", tc.Name)
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Text.Error
			borderCol = t.Color.Text.Error
		} else {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.Checkmark)
			statusLabel = fmt.Sprintf("TOOL SUCCESS: %s", tc.Name)
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Surface.Success
			borderCol = t.Color.Surface.Success
		}

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
					kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(statusLabel)),
				),
				kitex.If(tm != nil, func() kitex.Node {
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
					kitex.If(argsStr != "", func() kitex.Node {
						var textCol color.Color
						var valCol color.Color
						if t != nil {
							textCol = t.Color.Text.Secondary
							valCol = t.Color.Text.Tertiary
						}
						return kitex.Box(kitex.BoxProps{
							Style: style.S().
								MarginBottom(1).
								Display(style.DisplayFlex).
								FlexDirection(style.FlexColumn).
								Gap(0),
						},
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(textCol).Bold(true)}, kitex.Text("Parameters:")),
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(valCol).WhiteSpace(style.WhiteSpacePreWrap).OverflowWrap(style.OverflowWrapBreakWord)}, kitex.Text(argsStr)),
						)
					}),
					kitex.If(tm != nil, func() kitex.Node {
						meta := tm.GetMetadata()
						isTruncated := false
						var cachedPath string
						if meta != nil {
							if tr, ok := meta["truncated"].(bool); ok && tr {
								isTruncated = true
							}
							if cp, ok := meta["full_content_path"].(string); ok {
								cachedPath = cp
							}
						}
						return kitex.Fragment(
							kitex.If(isTruncated && cachedPath != "" && props.OnViewFullOutput != nil, func() kitex.Node {
								return kitex.Box(kitex.BoxProps{
									Style: style.S().MarginBottom(1),
								},
									components.Button(components.ButtonProps{
										Variant: components.ButtonSolid,
										Color:   components.ButtonPrimary,
										OnClick: func() {
											props.OnViewFullOutput(fmt.Sprintf("Full Output: %s", tc.Name), cachedPath)
										},
									}, kitex.Box(kitex.BoxProps{
										Style: style.S().
											Display(style.DisplayFlex).
											FlexDirection(style.FlexRow).
											AlignItems(style.AlignCenter).
											Gap(1),
									},
										icon.Search,
										kitex.Text("VIEW FULL OUTPUT IN MODAL"),
									)),
								)
							}),
							kitex.If(strings.TrimSpace(outText) != "", func() kitex.Node {
								var borderCol color.Color
								var textCol color.Color
								if t != nil {
									borderCol = t.Color.Border.Primary
									textCol = t.Color.Text.Secondary
								}
								outputContainerStyle := style.S().
									Display(style.DisplayFlex).
									FlexDirection(style.FlexColumn).
									Border(true, style.SingleBorder(), borderCol).
									Background(t.Color.Surface.BaseHover).
									Padding(1).
									Width(style.Percent(100)).
									MaxWidth(style.Percent(100)).
									Overflow(style.OverflowHidden).
									Foreground(textCol).
									WhiteSpace(style.WhiteSpacePreWrap).
									OverflowWrap(style.OverflowWrapBreakWord)

								lines := strings.Split(outText, "\n")
								isInlineTruncated := len(lines) > 15 || len(outText) > 1000
								var displayText string
								if isInlineTruncated {
									if len(lines) > 15 {
										displayText = strings.Join(lines[:15], "\n") + "\n\n... (truncated for display, click button below to view full output)"
									} else {
										displayText = outText[:1000] + "\n\n... (truncated for display, click button below to view full output)"
									}
								} else {
									displayText = outText
								}

								cleanText := strings.ReplaceAll(displayText, "\t", "    ")
								return kitex.Fragment(
									kitex.Box(kitex.BoxProps{Style: outputContainerStyle},
										kitex.Text(cleanText),
									),
									kitex.If(isInlineTruncated, func() kitex.Node {
										return kitex.Box(kitex.BoxProps{
											Style: style.S().MarginTop(1),
										},
											components.Button(components.ButtonProps{
												Variant: components.ButtonSolid,
												Color:   components.ButtonPrimary,
												OnClick: func() {
													setShowFullOutput(true)
												},
											}, kitex.Box(kitex.BoxProps{
												Style: style.S().
													Display(style.DisplayFlex).
													FlexDirection(style.FlexRow).
													AlignItems(style.AlignCenter).
													Gap(1),
											},
												icon.Search,
												kitex.Text(" VIEW ENTIRE OUTPUT"),
											)),
										)
									}),
								)
							}),
							kitex.If(strings.TrimSpace(outText) == "", func() kitex.Node {
								var textCol color.Color
								if t != nil {
									textCol = t.Color.Text.Tertiary
								}
								return kitex.Box(kitex.BoxProps{
									Style: style.S().Foreground(textCol).Italic(true),
								}, kitex.Text("(no output)"))
							}),
						)
					}),
				)
			}),
		),
		components.Modal(components.ModalProps{
			IsOpen:  showFullOutput(),
			Title:   kitex.Text(fmt.Sprintf("Full Output: %s", tc.Name)),
			OnClose: func() { setShowFullOutput(false) },
		},
			kitex.If(showFullOutput(), func() kitex.Node {
				var borderCol color.Color
				var textCol color.Color
				if t != nil {
					borderCol = t.Color.Border.Primary
					textCol = t.Color.Text.Secondary
				}
				outputStyle := style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					Border(true, style.SingleBorder(), borderCol).
					Background(t.Color.Surface.BaseHover).
					Padding(1).
					Width(style.Percent(100)).
					MaxWidth(style.Percent(100)).
					Foreground(textCol).
					WhiteSpace(style.WhiteSpacePreWrap).
					OverflowWrap(style.OverflowWrapBreakWord)

				cleanText := strings.ReplaceAll(outText, "\t", "    ")
				return kitex.Box(kitex.BoxProps{Style: outputStyle},
					kitex.Text(cleanText),
				)
			}),
		),
	)
})
