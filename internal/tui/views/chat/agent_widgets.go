package chat

import (
	"encoding/json"
	"fmt"
	"image/color"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

func parseInvokeAgentOutput(raw any) (tools.InvokeAgentOutput, bool) {
	if raw == nil {
		return tools.InvokeAgentOutput{}, false
	}
	if out, ok := raw.(tools.InvokeAgentOutput); ok {
		return out, true
	}
	if out, ok := raw.(*tools.InvokeAgentOutput); ok && out != nil {
		return *out, true
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return tools.InvokeAgentOutput{}, false
	}
	var out tools.InvokeAgentOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return tools.InvokeAgentOutput{}, false
	}
	return out, true
}

// AgentToolWidget renders the execution state and results of the "invoke_agent" tool.
var AgentToolWidget = kitex.FC("AgentToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	isOpen, setIsOpen := kitex.UseState(true)

	tc := props.ToolCall
	tm := props.ToolMessage

	agentRef := "agent"
	taskDesc := ""
	if tc != nil && len(tc.Args) > 0 {
		if val, ok := tc.Args["agent_ref"].(string); ok {
			agentRef = val
		}
		if val, ok := tc.Args["task"].(string); ok {
			taskDesc = val
		}
	}

	var out tools.InvokeAgentOutput
	var hasStructured bool
	if tm != nil {
		out, hasStructured = parseInvokeAgentOutput(tm.StructuredContent)
	}

	taskID := out.TaskId
	isFinished := tm != nil

	var statusLabel string
	var iconNode kitex.Node
	var headerBg color.Color
	var headerFg color.Color
	var borderCol color.Color

	if t != nil {
		if !isFinished {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
			statusLabel = "SUBAGENT RUNNING"
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Surface.Info
			borderCol = t.Color.Surface.Info
		} else if out.Error != "" || tm.IsError || out.Status == "failed" {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			statusLabel = "SUBAGENT ERROR"
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Text.Error
			borderCol = t.Color.Text.Error
		} else {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.Robot)
			statusLabel = "SUBAGENT SUCCESS"
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Surface.Success
			borderCol = t.Color.Surface.Success
		}
	}

	containerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100)).
		MaxWidth(style.Percent(100)).
		MinWidth(style.Percent(0)).
		Overflow(style.OverflowHidden).
		Margin(1, 0)

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

	return kitex.Box(kitex.BoxProps{Style: containerStyle},
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
				kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(fmt.Sprintf(" %s (%s)", statusLabel, agentRef))),
			),
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Gap(2),
			},
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
				// Custom task description block
				kitex.If(taskDesc != "", func() kitex.Node {
					return components.Markdown(components.MarkdownProps{
						Source: taskDesc,
					})
				}),

				// Markdown Text Output wrapped in a Card
				kitex.If(hasStructured && out.Output != "", func() kitex.Node {
					return components.Card(components.CardProps{
						Color:   components.PaperBase,
						Variant: components.CardOutlined,
						Style:   style.S().Padding(1).MarginTop(1),
					},
						components.Markdown(components.MarkdownProps{
							Source: out.Output,
						}),
					)
				}),

				// Error output wrapped in a Card
				kitex.If(hasStructured && out.Error != "", func() kitex.Node {
					var errBorder color.Color
					if t != nil {
						errBorder = t.Color.Text.Error
					}
					return components.Card(components.CardProps{
						Color:   components.PaperBase,
						Variant: components.CardOutlined,
						Style:   style.S().Border(errBorder).Padding(1).MarginTop(1),
					},
						components.Markdown(components.MarkdownProps{
							Source: out.Error,
						}),
					)
				}),

				// Actions inside the collapsible body
				kitex.If(taskID != "", func() kitex.Node {
					return kitex.Box(kitex.BoxProps{
						Style: style.S().Padding(1, 0, 0, 0),
					},
						components.Button(components.ButtonProps{
							Variant: components.ButtonText,
							Style:   style.S().Foreground(t.Color.Text.Secondary).Bold(true),
							OnClick: func() {
								if props.OnViewSubagent != nil {
									props.OnViewSubagent(fmt.Sprintf("Subagent Log: %s", agentRef), taskID)
								}
							},
						},
							kitex.Box(kitex.BoxProps{
								Style: style.S().
									Display(style.DisplayFlex).
									FlexDirection(style.FlexRow).
									AlignItems(style.AlignCenter).
									Gap(1),
							},
								kitex.Span(kitex.SpanProps{}, icon.History),
								kitex.Text("View Chat"),
							),
						),
					)
				}),
			)
		}),
	)
})
