package chat

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// TodosToolWidget renders the list of subtasks as a collapsible accordion showing the in-progress status.
var TodosToolWidget = kitex.FC("TodosToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)

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

	// Calculate counts and identify active/in-progress task
	pendings := 0
	inProgress := 0
	completed := 0
	var activeTaskDesc string

	for _, item := range todos {
		switch item.Status {
		case "pending":
			pendings++
		case "in_progress":
			inProgress++
			if activeTaskDesc == "" {
				activeTaskDesc = item.Description
			}
		case "completed":
			completed++
		}
	}

	// Build status counts suffix
	var countParts []string
	if completed > 0 {
		countParts = append(countParts, fmt.Sprintf("%d completed", completed))
	}
	if inProgress > 0 {
		countParts = append(countParts, fmt.Sprintf("%d in progress", inProgress))
	}
	if pendings > 0 {
		countParts = append(countParts, fmt.Sprintf("%d pending", pendings))
	}

	var statusLabel string
	var labelNode kitex.Node
	var iconNode kitex.Node
	var themeColor color.Color
	var details string

	if tm != nil && tm.IsError {
		details = getToolOutput(tm.Content)
	}

	if t != nil {
		var actionText string
		if tm == nil {
			actionText = "Updating Checklist "
			statusLabel = "Updating Checklist"
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			actionText = "Checklist Error "
			statusLabel = "Checklist Error"
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
		} else {
			actionText = "Updated Checklist "
			statusLabel = "Checklist"
			iconNode = nil // remove checkmark completely on success
			themeColor = t.Color.Surface.Success
		}

		baseFocusBg := t.Color.Surface.BaseFocus
		calendarIconColor := t.Color.Surface.Info

		labelNode = kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter),
		},
			kitex.Span(kitex.SpanProps{
				Style: style.S().
					Bold(true).
					Foreground(color.RGBA{255, 255, 255, 255}),
			}, kitex.Text(actionText)),
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Background(baseFocusBg).
					PaddingHorizontal(1).
					Gap(1),
			},
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(calendarIconColor)}, icon.Calendar),
				kitex.Span(kitex.SpanProps{
					Style: style.S().
						Foreground(color.RGBA{255, 255, 255, 255}).
						Bold(true),
				}, kitex.Text("Checklist")),
			),
			kitex.If(len(countParts) > 0, func() kitex.Node {
				return kitex.Span(kitex.SpanProps{
					Style: style.S().
						Foreground(t.Color.Text.Secondary).
						PaddingLeft(1),
				}, kitex.Text(fmt.Sprintf("(%s)", strings.Join(countParts, ", "))))
			}),
		)
	}

	var onClick func()
	if tm != nil {
		onClick = func() { setShowModal(true) }
	}

	badgeNode := components.ToolBadge(components.ToolBadgeProps{
		Icon:      iconNode,
		Label:     statusLabel,
		LabelNode: labelNode,
		Color:     themeColor,
		OnClick:   onClick,
	})

	return kitex.Fragment(
		badgeNode,
		components.Modal(components.ModalProps{
			IsOpen: showModal(),
			Title: kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Gap(1),
			},
				kitex.If(t != nil && tm != nil && tm.IsError, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
				}),
				kitex.If(tm != nil && tm.IsError, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{}, kitex.Text("Checklist Error"))
				}),
				kitex.If(tm != nil && !tm.IsError, func() kitex.Node {
					return kitex.Fragment(
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Calendar),
						kitex.Span(kitex.SpanProps{}, kitex.Text("Checklist")),
					)
				}),
			),
			OnClose: func() { setShowModal(false) },
		},
			kitex.If(showModal() && tm != nil && tm.IsError && details != "", func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Padding(1).
						Width(style.Percent(100)).
						MinWidth(style.Percent(0)).
						Foreground(t.Color.Text.Secondary).
						WhiteSpace(style.WhiteSpacePreWrap),
				}, kitex.Text(details))
			}),
			kitex.If(showModal() && tm != nil && !tm.IsError && len(todos) > 0, func() kitex.Node {
				rows := make([]kitex.Node, len(todos))
				for i, item := range todos {
					rows[i] = todoRow(t, item.Description, item.Status, item.ActiveText)
				}
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Padding(1).
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
