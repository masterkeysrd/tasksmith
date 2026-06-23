package sidebar

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
)

var (
	agentCardStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Gap(1)

	agentHeaderRowStyle = style.S().
				Display(style.DisplayFlex).
				AlignItems(style.AlignCenter).
				JustifyContent(style.JustifyBetween).
				Padding(1)

	agentHeaderLeftStyle = style.S().
				Display(style.DisplayFlex).
				AlignItems(style.AlignCenter).
				Gap(1)

	agentBodyStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Gap(1).
			PaddingLeft(1)

	agentTodoRowStyle = style.S().
				Display(style.DisplayFlex).
				AlignItems(style.AlignCenter).
				Gap(1)

	agentCompileButtonStyle = style.S().
				Width(style.Percent(100)).
				JustifyContent(style.JustifyCenter)
)

func agentCard(agent api.Agent, todos []api.Todo) kitex.Node {
	c := useColors()

	task := strings.TrimSpace(agent.Description)
	if task == "" {
		task = "No description provided."
	}

	doneTodos := 0
	for _, t := range todos {
		if t.Status == "completed" {
			doneTodos++
		}
	}

	return kitex.Box(kitex.BoxProps{
		Style: agentCardStyle.
			Merge(style.S().
				Border(style.SingleBorder().Color(c.border)).
				Background(c.background)),
	},
		// Header: ● NAME  [READY]
		kitex.Box(kitex.BoxProps{
			Style: agentHeaderRowStyle.
				Merge(style.S().
					Background(c.surface),
				),
		},
			kitex.Box(kitex.BoxProps{Style: agentHeaderLeftStyle},
				kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(c.success),
				}, kitex.Text("●")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(c.text).Bold(true),
				}, kitex.Text(strings.ToUpper(agent.Name))),
			),
			kitex.Box(kitex.BoxProps{
				Style: style.S().Foreground(c.success).Bold(true),
			}, kitex.Text("[READY]")),
		),

		// Body
		kitex.Box(kitex.BoxProps{Style: agentBodyStyle},
			// > task
			kitex.Box(kitex.BoxProps{
				Style: style.S().Foreground(c.info),
			}, kitex.Text("> "+task)),

			// Progress indicators row
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					AlignItems(style.AlignCenter).
					JustifyContent(style.JustifyBetween),
			},
				kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(c.subtle).Bold(true),
				}, kitex.Text("Progress Indicators")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(c.muted).Bold(true),
				}, kitex.Text(fmt.Sprintf("[%d/%d DONE]", doneTodos, len(todos)))),
			),

			// Collapsible subtasks via Accordion
			components.Accordion(components.AccordionProps{},
				components.AccordionSummary(components.AccordionSummaryProps{
					Style: style.S().
						Foreground(c.accent).
						PaddingHorizontal(1).
						Background(c.surface),
				}, kitex.Text(fmt.Sprintf("SUBTASKS (%d/%d)", doneTodos, len(todos)))),

				components.AccordionDetails(components.AccordionDetailsProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Padding(1).
						Gap(1).
						Background(c.surface),
				},
					kitex.If(len(todos) == 0, func() kitex.Node {
						return kitex.Box(kitex.BoxProps{
							Style: style.S().Foreground(c.muted).Padding(1),
						}, kitex.Text("No active subtasks."))
					}),
					kitex.Map(todos, func(todo api.Todo, _ int) kitex.Node {
						checkIcon := "󰄱"
						iconColor := c.subtle
						textColor := c.muted
						var activeTextNode kitex.Node

						if todo.Status == "completed" {
							checkIcon = "󰄲"
							iconColor = c.success
							textColor = c.subtle
						} else if todo.Status == "in_progress" {
							checkIcon = "󰄰"
							iconColor = c.accent
							textColor = c.text
							if todo.ActiveText != "" {
								activeTextNode = kitex.Box(kitex.BoxProps{
									Style: style.S().
										Foreground(c.info).
										PaddingLeft(3).
										Italic(true),
								}, kitex.Text(todo.ActiveText))
							}
						}

						return kitex.Box(kitex.BoxProps{
							Style: style.S().
								Display(style.DisplayFlex).
								FlexDirection(style.FlexColumn),
						},
							kitex.Box(kitex.BoxProps{Style: agentTodoRowStyle},
								kitex.Box(kitex.BoxProps{
									Style: style.S().Foreground(iconColor),
								}, kitex.Text(checkIcon)),
								kitex.Box(kitex.BoxProps{
									Style: style.S().Foreground(textColor),
								}, kitex.Text(todo.Description)),
							),
							kitex.If(activeTextNode != nil, func() kitex.Node {
								return activeTextNode
							}),
						)
					}),
				),
			),
		),
	)
}

func orchestratorPanel(data Data, onCreateAgent func()) kitex.Node {
	c := useColors()

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Gap(1).
			Background(c.panel),
	},
		components.Button(components.ButtonProps{
			Variant: components.ButtonOutline,
			Color:   components.ButtonInfo,
			Style:   agentCompileButtonStyle,
			OnClick: onCreateAgent,
		}, kitex.Text("+ SPAWN BACKGROUND AGENT")),

		kitex.If(len(data.Agents) == 0, func() kitex.Node {
			return emptyState("No agents defined in this workspace.")
		}),

		kitex.Map(data.Agents, func(agent api.Agent, _ int) kitex.Node {
			return agentCard(agent, data.Todos)
		}),
	)
}
