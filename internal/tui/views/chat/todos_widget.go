package chat

import (
	"fmt"
	"image/color"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// TodosToolWidget renders the list of subtasks as a compact, interactive accordion.
var TodosToolWidget = kitex.FC("TodosToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()

	tc := props.ToolCall
	tm := props.ToolMessage

	var todos []localTodo
	if tm != nil && !tm.IsError {
		out, ok := parseStructuredOutput[tools.TodosOutput](tm.StructuredContent)
		if ok {
			for _, item := range out.Todos {
				todos = append(todos, localTodo{
					Description: item.Description,
					Status:      item.Status,
					ActiveText:  item.ActiveText,
				})
			}
		}
	}
	if len(todos) == 0 && tc != nil && tc.Args != nil {
		inputArgs, ok := parseStructuredOutput[tools.TodosArgs](tc.Args)
		if ok {
			for _, item := range inputArgs.Todos {
				todos = append(todos, localTodo{
					Description: item.Description,
					Status:      item.Status,
					ActiveText:  item.ActiveText,
				})
			}
		}
	}

	// Calculate counts and identify active task
	total := len(todos)
	completed := 0
	var activeTaskDesc string

	for _, item := range todos {
		if item.Status == "completed" {
			completed++
		} else if item.Status == "in_progress" && activeTaskDesc == "" {
			activeTaskDesc = item.Description
		}
	}

	var statusLabel string
	var iconNode kitex.Node
	var borderCol color.Color

	if t != nil {
		if tm == nil {
			statusLabel = "Updating Checklist"
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			borderCol = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = "Checklist Error"
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			borderCol = t.Color.Text.Error
		} else {
			statusLabel = "Checklist"
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Calendar)
			borderCol = t.Color.Text.Tertiary
		}
	}

	accordionStyle := style.S()
	if t != nil {
		accordionStyle = accordionStyle.Border(borderCol).MarginVertical(1)
	}

	return components.Accordion(components.AccordionProps{
		Color:   components.PaperHover,
		Variant: components.PaperOutlined,
		Style:   accordionStyle,
	},
		components.AccordionSummary(components.AccordionSummaryProps{
			HideExpandIcon: tm == nil || tm.IsError,
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
				kitex.If(total > 0, func() kitex.Node {
					return kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							AlignItems(style.AlignCenter).
							Gap(1),
					},
						kitex.Span(kitex.SpanProps{
							Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true),
						}, kitex.Text(fmt.Sprintf("[%d/%d]", completed, total))),
						kitex.If(activeTaskDesc != "", func() kitex.Node {
							return kitex.Box(kitex.BoxProps{
								Style: style.S().
									Display(style.DisplayFlex).
									FlexDirection(style.FlexRow).
									AlignItems(style.AlignCenter).
									Gap(1),
							},
								kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("|")),
								kitex.Span(kitex.SpanProps{
									Style: style.S().Foreground(t.Color.Text.Secondary).Italic(true),
								}, kitex.Text(activeTaskDesc)),
							)
						}),
					)
				}),
			),
		),
		components.AccordionDetails(components.AccordionDetailsProps{
			Style: style.S().PaddingHorizontal(1).PaddingBottom(1),
		},
			kitex.If(tm != nil && tm.IsError, func() kitex.Node {
				details := getToolOutput(tm.Content)
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(t.Color.Text.Error),
				}, kitex.Text(details))
			}),
			kitex.If(tm != nil && !tm.IsError && len(todos) > 0, func() kitex.Node {
				rows := make([]kitex.Node, len(todos))
				for i, item := range todos {
					rows[i] = todoRow(t, item.Description, item.Status, item.ActiveText)
				}
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Gap(0),
				}, rows...)
			}),
		),
	)
})

type localTodo struct {
	Description string
	Status      string
	ActiveText  string
}

func todoRow(t *theme.Scheme, description string, status string, activeText string) kitex.Node {
	checkIcon := "󰄱"
	var iconColor color.Color
	var textColor color.Color
	var activeTextNode kitex.Node

	if t != nil {
		iconColor = t.Color.Text.Tertiary
		textColor = t.Color.Text.Secondary

		switch status {
		case "completed":
			checkIcon = "󰄲"
			iconColor = t.Color.Surface.Success
			textColor = t.Color.Text.Secondary
		case "in_progress":
			checkIcon = "󰄰"
			iconColor = t.Color.Surface.Info
			textColor = t.Color.Text.Primary
			if activeText != "" {
				activeTextNode = kitex.Box(kitex.BoxProps{
					Style: style.S().
						Foreground(t.Color.Surface.Info).
						PaddingLeft(3).
						Italic(true),
				}, kitex.Text(activeText))
			}
		}
	}

	rowStyle := style.S().
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter).
		Gap(1)

	var iconSpan kitex.Node
	if t != nil {
		iconSpan = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(iconColor)}, kitex.Text(checkIcon))
	} else {
		iconSpan = kitex.Text(checkIcon)
	}

	var textSpan kitex.Node
	if t != nil {
		textSpan = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(textColor)}, kitex.Text(description))
	} else {
		textSpan = kitex.Text(description)
	}

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn),
	},
		kitex.Box(kitex.BoxProps{Style: rowStyle},
			iconSpan,
			textSpan,
		),
		kitex.If(activeTextNode != nil, func() kitex.Node {
			return activeTextNode
		}),
	)
}
