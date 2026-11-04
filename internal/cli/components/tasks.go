package components

import (
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

type tasksBarProps struct {
	Subagents []api.Session
	Tasks     []api.Task
}

var tasksBar = trail.FC("TasksBar", func(props tasksBarProps) trail.Node {
	scheme := theme.UseTheme()

	tick, setTick := kitex.UseState(0)
	if len(props.Subagents) > 0 {
		kitex.UseInterval(func() {
			setTick(tick() + 1)
		}, time.Second, []any{len(props.Subagents)})
	}

	dotStyle := style.S().Flex(0, 0).WhiteSpace(style.WhiteSpaceNoWrap).Foreground(scheme.Color.Icon.Primary)
	timeStyle := style.S().Flex(0, 0).WhiteSpace(style.WhiteSpaceNoWrap).Foreground(scheme.Color.Text.Secondary)
	summaryStyle := style.S().
		Flex(1, 1).
		MinWidth(style.Cells(0)).
		WhiteSpace(style.WhiteSpaceNoWrap).
		Overflow(style.OverflowHidden).
		TextOverflow(style.TextOverflowEllipsis).
		Foreground(scheme.Color.Text.Primary)
	statusStyle := style.S().Flex(0, 0).WhiteSpace(style.WhiteSpaceNoWrap).Margin(0, 0, 0, 2).Foreground(scheme.Color.Icon.Primary)

	border := style.SingleBorder().Top(false).Bottom(true).Left(false).Right(false).Color(scheme.Color.Border.Default)

	rows := make([]trail.Node, 0, len(props.Subagents)+len(props.Tasks))

	for _, sub := range props.Subagents {
		agentColor := SubagentColor(scheme, sub.Config.Agent)
		title := strings.TrimSpace(strings.ReplaceAll(sub.Title, "\n", " "))
		if title == "" {
			title = sub.ID
		}
		activity := subagentActivityText(sub)
		if runes := []rune(activity); len(runes) > 60 {
			activity = string(runes[:60]) + "..."
		}
		elapsed := "--"
		if !sub.CreatedAt.IsZero() {
			elapsed = format.Duration(time.Since(sub.CreatedAt).Truncate(time.Second))
		}

		subagentRow := trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				Width(style.Percent(100)).
				MinWidth(style.Cells(0)).
				Overflow(style.OverflowHidden),
		},
			trail.Span(trail.SpanProps{Style: style.S().Flex(0, 0).WhiteSpace(style.WhiteSpacePre).Foreground(agentColor)}, trail.Text("● ")),
			trail.Span(trail.SpanProps{Style: style.S().Flex(0, 0).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Primary)}, trail.Text("Agent(")),
			trail.Span(trail.SpanProps{Style: style.S().Flex(0, 0).WhiteSpace(style.WhiteSpacePre).Bold(true).Foreground(agentColor)}, trail.Text(sub.Config.Agent)),
			trail.Span(trail.SpanProps{Style: style.S().Flex(0, 0).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Primary)}, trail.Text(": "+title+")")),
			trail.Span(trail.SpanProps{
				Style: style.S().
					Flex(1, 1).
					MinWidth(style.Cells(0)).
					Margin(0, 0, 0, 2).
					WhiteSpace(style.WhiteSpaceNoWrap).
					Overflow(style.OverflowHidden).
					TextOverflow(style.TextOverflowEllipsis).
					Foreground(scheme.Color.Text.Secondary),
			}, trail.Text(activity)),
			trail.Span(trail.SpanProps{Style: style.S().Flex(0, 0).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary)}, trail.Text(" · "+elapsed)),
		)
		rows = append(rows, subagentRow)
	}

	for _, t := range props.Tasks {
		timeStr := "--:--:--"
		if !t.StartedAt.IsZero() {
			timeStr = t.StartedAt.Local().Format("15:04:05")
		}

		title := strings.TrimSpace(strings.ReplaceAll(t.Title, "\n", " "))
		cmd := strings.TrimSpace(strings.ReplaceAll(t.Command, "\n", " "))

		var summaryText string
		if title != "" && cmd != "" && title != cmd {
			summaryText = title + " · " + cmd
		} else if title != "" {
			summaryText = title
		} else if cmd != "" {
			summaryText = cmd
		} else {
			summaryText = t.ID
		}

		rowChildren := []trail.Node{
			trail.Span(trail.SpanProps{Style: dotStyle}, trail.Text("● ")),
			trail.Span(trail.SpanProps{Style: timeStyle}, trail.Text("["+timeStr+"] ")),
			trail.Span(trail.SpanProps{Style: summaryStyle}, trail.Text(summaryText)),
			trail.Span(trail.SpanProps{Style: statusStyle}, trail.Text(string(t.Status))),
		}

		rows = append(rows, trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				Width(style.Percent(100)).
				MinWidth(style.Cells(0)).
				Overflow(style.OverflowHidden),
		}, rowChildren...))
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)),
	}, rows...)
})

// subagentActivityText describes what a running subagent is doing. The daemon
// reports a live activity line while one is available; otherwise the status
// alone is shown.
func subagentActivityText(sub api.Session) string {
	activity := strings.TrimSpace(strings.ReplaceAll(sub.Activity, "\n", " "))
	if activity != "" {
		return activity
	}
	switch sub.Status {
	case api.StatusWaitingPermission:
		return "Waiting for permission"
	case api.StatusWaitingQuestion:
		return "Waiting for answer"
	case api.StatusThinking:
		return "Thinking..."
	case api.StatusGenerating:
		return "Generating"
	case api.StatusExecuting:
		return "Executing"
	case api.StatusCompacting:
		return "Compacting"
	case "":
		return ""
	default:
		return string(sub.Status)
	}
}

// Tasks renders an indicator list of active background tasks and running subagents.
var Tasks = trail.SimpleFC("Tasks", func() trail.Node {
	if store.UseHasActiveSlot() {
		return nil
	}
	runningSubs := store.UseRunningSubagents()
	runningTasks := store.UseRunningTasks()
	if len(runningSubs) == 0 && len(runningTasks) == 0 {
		return nil
	}
	return tasksBar(tasksBarProps{Subagents: runningSubs, Tasks: runningTasks})
})
