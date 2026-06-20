package sidebar

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
)

// agentTodo is a mock subtask item shown inside an agent card.
type agentTodo struct {
	ID        string
	Task      string
	Completed bool
}

// mockAgentTodos provides sample data to demonstrate the subtask UI.
var mockAgentTodos = []agentTodo{
	{ID: "1", Task: "Analyze workspace configuration", Completed: true},
	{ID: "2", Task: "Generate agent specification", Completed: true},
	{ID: "3", Task: "Validate tool permissions", Completed: false},
	{ID: "4", Task: "Initialize agent runtime", Completed: false},
}

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

func agentCard(agent api.Agent) kitex.Node {
	c := useColors()

	task := strings.TrimSpace(agent.Description)
	if task == "" {
		task = "No description provided."
	}

	todos := mockAgentTodos
	doneTodos := 0
	for _, t := range todos {
		if t.Completed {
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
					kitex.Map(todos, func(todo agentTodo, _ int) kitex.Node {
						checkIcon := "󰄱"
						iconColor := c.subtle
						textColor := c.muted
						if todo.Completed {
							checkIcon = "󰄲"
							iconColor = c.success
							textColor = c.subtle
						}
						return kitex.Box(kitex.BoxProps{Style: agentTodoRowStyle},
							kitex.Box(kitex.BoxProps{
								Style: style.S().Foreground(iconColor),
							}, kitex.Text(checkIcon)),
							kitex.Box(kitex.BoxProps{
								Style: style.S().Foreground(textColor),
							}, kitex.Text(todo.Task)),
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
			return agentCard(agent)
		}),
	)
}
