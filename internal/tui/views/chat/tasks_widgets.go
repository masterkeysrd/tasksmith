package chat

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/core/preview"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type RunningTasksWidgetProps struct {
	Tasks []api.RunningTaskInfo
}

var RunningTasksWidget = kitex.FC("RunningTasksWidget", func(props RunningTasksWidgetProps) kitex.Node {
	t := theme.UseTheme()
	if len(props.Tasks) == 0 {
		return nil
	}

	taskWord := "task"
	if len(props.Tasks) > 1 {
		taskWord = "tasks"
	}

	summaryText := fmt.Sprintf("%d %s running", len(props.Tasks), taskWord)

	var taskRows []kitex.Node
	for _, task := range props.Tasks {
		dispDetails := task.Details
		if dispDetails == "" {
			dispDetails = "-"
		}

		// Truncate task command if too long
		dispName := task.Name
		if len(dispName) > 40 {
			dispName = dispName[:37] + "..."
		}

		taskRows = append(taskRows, kitex.TR(kitex.TRProps{},
			kitex.TD(kitex.TDProps{Style: style.S().Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text(task.ID)),
			kitex.TD(kitex.TDProps{Style: style.S().Foreground(t.Color.Surface.Info).PaddingRight(1).Width(style.MaxContent)}, kitex.Text(strings.ToUpper(task.Type))),
			kitex.TD(kitex.TDProps{Style: style.S().Foreground(t.Color.Surface.Success).PaddingRight(1).Width(style.MaxContent)}, kitex.Text(dispDetails)),
			kitex.TD(kitex.TDProps{Style: style.S().Foreground(t.Color.Text.Primary).Width(style.Percent(100))}, kitex.Text(dispName)),
		))
	}

	headerRow := kitex.TR(kitex.TRProps{},
		// Columns: TASK ID | TYPE | DETAILS | COMMAND / NAME
		kitex.TD(kitex.TDProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text("TASK ID")),
		kitex.TD(kitex.TDProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text("TYPE")),
		kitex.TD(kitex.TDProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text("DETAILS")),
		kitex.TD(kitex.TDProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary).Width(style.Percent(100))}, kitex.Text("COMMAND / NAME")),
	)

	allRows := append([]kitex.Node{headerRow}, taskRows...)

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			MarginTop(1).
			MarginBottom(1).
			Width(style.Percent(100)).
			MaxWidth(style.Percent(90)).
			AlignSelf(style.AlignStart),
	},
		components.Accordion(components.AccordionProps{
			Color:   components.PaperSurface,
			Variant: components.PaperOutlined,
		},
			components.AccordionSummary(components.AccordionSummaryProps{},
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Primary).Bold(true)}, kitex.Text(summaryText)),
			),
			components.AccordionDetails(components.AccordionDetailsProps{
				Style: style.S().Padding(1, 1),
			},
				kitex.Table(kitex.TableProps{},
					kitex.TBody(kitex.TBodyProps{},
						allRows...,
					),
				),
			),
		),
	)
})

// TasksToolWidget renders background task queries action and result output.
// TasksToolWidget renders background task queries action and result output.
var TasksToolWidget = kitex.FC("TasksToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	isOpen, setIsOpen := kitex.UseState(true)

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

	action := ""
	targetTaskID := ""
	inputVal := ""
	if tc != nil && len(tc.Args) > 0 {
		if actVal, ok := tc.Args["action"]; ok {
			action, _ = actVal.(string)
		}
		if tidVal, ok := tc.Args["taskId"]; ok {
			targetTaskID, _ = tidVal.(string)
		}
		if inputValVal, ok := tc.Args["input"]; ok {
			inputVal, _ = inputValVal.(string)
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

	isFinished := tm != nil
	hasErr := tm != nil && tm.IsError

	// Parse structured result
	var out tools.TasksOutput
	var hasStructured bool
	if tm != nil {
		out, hasStructured = parseTasksOutput(tm.StructuredContent)
	}

	if isFinished {
		if hasErr {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			statusLabel = fmt.Sprintf("TASKS %s ERROR", strings.ToUpper(action))
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Text.Error
			borderCol = t.Color.Text.Error
		} else {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.Checkmark)
			statusLabel = fmt.Sprintf("TASKS %s SUCCESS", strings.ToUpper(action))
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Surface.Success
			borderCol = t.Color.Surface.Success
		}
	} else {
		iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
		statusLabel = fmt.Sprintf("RUNNING TASKS %s", strings.ToUpper(action))
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
					kitex.If(isFinished && action != "kill", func() kitex.Node {
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
					// Result Output depending on action
					kitex.If(tm != nil, func() kitex.Node {
						if hasStructured {
							if action == "list" {
								if len(out.Tasks) == 0 {
									return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).Italic(true)}, kitex.Text("No active or background tasks found in this session."))
								}

								// Header row for table
								headerRow := kitex.TR(kitex.TRProps{},
									kitex.TD(kitex.TDProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text("TASK ID")),
									kitex.TD(kitex.TDProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text("STATUS")),
									kitex.TD(kitex.TDProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text("STARTED")),
									kitex.TD(kitex.TDProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary).Width(style.Percent(100))}, kitex.Text("COMMAND / NAME")),
								)

								var taskRows []kitex.Node
								taskRows = append(taskRows, headerRow)

								for _, task := range out.Tasks {
									var statText string
									var statCol color.Color
									switch task.Status {
									case "running":
										statText = "● RUNNING"
										statCol = t.Color.Surface.Info
									case "finished", "completed":
										if task.ExitCode == 0 {
											statText = "✔ COMPLETED"
											statCol = t.Color.Surface.Success
										} else {
											statText = fmt.Sprintf("✘ FAILED (%d)", task.ExitCode)
											statCol = t.Color.Text.Error
										}
									case "killed":
										statText = "⏹ KILLED"
										statCol = t.Color.Text.Secondary
									default:
										statText = strings.ToUpper(task.Status)
										statCol = t.Color.Text.Primary
									}

									startedTime := task.StartedAt
									if pt, err := time.Parse(time.RFC3339, task.StartedAt); err == nil {
										startedTime = pt.Format("15:04:05")
									}

									shortID := task.TaskId
									if len(shortID) > 12 {
										shortID = shortID[:12] + "…"
									}

									taskRows = append(taskRows, kitex.TR(kitex.TRProps{},
										kitex.TD(kitex.TDProps{Style: style.S().Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text(shortID)),
										kitex.TD(kitex.TDProps{Style: style.S().Foreground(statCol).PaddingRight(1).Width(style.MaxContent)}, kitex.Text(statText)),
										kitex.TD(kitex.TDProps{Style: style.S().Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text(startedTime)),
										kitex.TD(kitex.TDProps{Style: style.S().Foreground(t.Color.Text.Primary).Width(style.Percent(100))}, kitex.Text(task.Name)),
									))
								}

								return kitex.Box(kitex.BoxProps{
									Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Width(style.Percent(100)),
								},
									kitex.Table(kitex.TableProps{},
										kitex.TBody(kitex.TBodyProps{}, taskRows...),
									),
								)
							}

							if action == "status" {
								var statusText string
								var statusCol color.Color
								switch out.Status {
								case "running":
									statusText = "● RUNNING"
									statusCol = t.Color.Surface.Info
								case "finished", "completed":
									if out.ExitCode == 0 {
										statusText = "✔ COMPLETED"
										statusCol = t.Color.Surface.Success
									} else {
										statusText = fmt.Sprintf("✘ FAILED (%d)", out.ExitCode)
										statusCol = t.Color.Text.Error
									}
								case "killed":
									statusText = "⏹ KILLED"
									statusCol = t.Color.Text.Secondary
								default:
									statusText = strings.ToUpper(out.Status)
									statusCol = t.Color.Text.Primary
								}

								stdoutLines := strings.Split(out.StdoutTail, "\n")
								stderrLines := strings.Split(out.StderrTail, "\n")

								isStdoutTruncated := len(stdoutLines) > 10
								isStderrTruncated := len(stderrLines) > 10
								hasAnyTruncation := isStdoutTruncated || isStderrTruncated

								var displayStdout string
								if isStdoutTruncated {
									displayStdout = strings.Join(stdoutLines[len(stdoutLines)-10:], "\n")
								} else {
									displayStdout = out.StdoutTail
								}

								var displayStderr string
								if isStderrTruncated {
									displayStderr = strings.Join(stderrLines[len(stderrLines)-10:], "\n")
								} else {
									displayStderr = out.StderrTail
								}

								var logElements []kitex.Node

								if strings.TrimSpace(displayStdout) != "" {
									logElements = append(logElements,
										kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).Bold(true).MarginBottom(1)}, kitex.Text("STDOUT:")),
										kitex.Box(kitex.BoxProps{
											Style: style.S().
												Foreground(t.Color.Text.Primary).
												Background(t.Color.Surface.BaseHover).
												Border(true, style.SingleBorder(), t.Color.Border.Primary).
												Padding(1).
												MarginBottom(1).
												WhiteSpace(style.WhiteSpacePreWrap),
										}, kitex.Text(strings.ReplaceAll(displayStdout, "\t", "    "))),
									)
								}

								if strings.TrimSpace(displayStderr) != "" {
									logElements = append(logElements,
										kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error).Bold(true).MarginBottom(1)}, kitex.Text("STDERR:")),
										kitex.Box(kitex.BoxProps{
											Style: style.S().
												Foreground(t.Color.Text.Error).
												Background(t.Color.Surface.BaseHover).
												Border(true, style.SingleBorder(), t.Color.Text.Error).
												Padding(1).
												MarginBottom(1).
												WhiteSpace(style.WhiteSpacePreWrap),
										}, kitex.Text(strings.ReplaceAll(displayStderr, "\t", "    "))),
									)
								}

								if len(logElements) == 0 {
									logElements = append(logElements, kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).Italic(true)}, kitex.Text("No command output logged yet.")))
								}

								return kitex.Box(kitex.BoxProps{
									Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Width(style.Percent(100)),
								},
									kitex.Box(kitex.BoxProps{
										Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).Gap(1).MarginBottom(1),
									},
										kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).Bold(true)}, kitex.Text("Status:")),
										kitex.Span(kitex.SpanProps{Style: style.S().Foreground(statusCol).Bold(true)}, kitex.Text(statusText)),
										kitex.If(out.Message != "", func() kitex.Node {
											return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(" — "+out.Message))
										}),
									),
									kitex.Box(kitex.BoxProps{
										Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn),
									}, logElements...),
									kitex.If(hasAnyTruncation, func() kitex.Node {
										return components.Button(components.ButtonProps{
											Variant: components.ButtonText,
											Color:   components.ButtonBase,
											Style: style.S().
												Foreground(t.Color.Surface.Info).
												MarginTop(1).
												Bold(true),
											OnClick: func() {
												if props.OnViewPreview != nil {
													var modalLogText string
													if strings.TrimSpace(out.StdoutTail) != "" {
														modalLogText += "FULL STDOUT:\n" + strings.ReplaceAll(out.StdoutTail, "\t", "    ") + "\n\n"
													}
													if strings.TrimSpace(out.StderrTail) != "" {
														modalLogText += "FULL STDERR:\n" + strings.ReplaceAll(out.StderrTail, "\t", "    ") + "\n"
													}
													props.OnViewPreview(
														fmt.Sprintf("Task Logs: %s", targetTaskID),
														preview.DefaultTextPreview{Text: modalLogText},
													)
												}
											},
										}, kitex.Fragment(
											kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.FontAwesomeTerminal),
											kitex.Text(" VIEW FULL OUTPUT"),
										),
										)
									}),
								)
							}

							if action == "kill" {
								return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Primary)}, kitex.Text(out.Message))
							}

							if action == "send_input" {
								var statusCol color.Color
								switch out.Status {
								case "running":
									statusCol = t.Color.Surface.Info
								case "finished", "completed":
									statusCol = t.Color.Surface.Success
								default:
									statusCol = t.Color.Text.Primary
								}

								return kitex.Box(kitex.BoxProps{
									Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
								},
									kitex.Box(kitex.BoxProps{
										Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).Gap(1),
									},
										kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).Bold(true)}, kitex.Text("Stdin Input:")),
										kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Primary).Italic(true)}, kitex.Text(fmt.Sprintf("%q", inputVal))),
									),
									kitex.Box(kitex.BoxProps{
										Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).Gap(1),
									},
										kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Primary)}, kitex.Text(out.Message)),
										kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(" (Task Status: ")),
										kitex.Span(kitex.SpanProps{Style: style.S().Foreground(statusCol).Bold(true)}, kitex.Text(strings.ToUpper(out.Status))),
										kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(")")),
									),
								)
							}
						}

						// Fallback to text blocks
						outText := getToolOutput(tm.Content)
						if strings.TrimSpace(outText) != "" {
							return kitex.Box(kitex.BoxProps{
								Style: style.S().
									Foreground(t.Color.Text.Primary).
									WhiteSpace(style.WhiteSpacePreWrap),
							}, kitex.Text(outText))
						}
						return nil
					}),
				)
			}),
		),
	)
})
