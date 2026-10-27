package components

import (
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

type tasksBarProps struct {
	Tasks []api.Task
}

var tasksBar = trail.FC("TasksBar", func(props tasksBarProps) trail.Node {
	scheme := theme.UseTheme()

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

	rows := make([]trail.Node, len(props.Tasks))
	for i, t := range props.Tasks {
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

		rows[i] = trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				Width(style.Percent(100)).
				MinWidth(style.Cells(0)).
				Overflow(style.OverflowHidden),
		}, rowChildren...)
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)),
	}, rows...)
})

// Tasks renders an indicator list of active background tasks.
var Tasks = trail.SimpleFC("Tasks", func() trail.Node {
	if store.UseHasActiveSlot() {
		return nil
	}
	running := store.UseRunningTasks()
	if len(running) == 0 {
		return nil
	}
	return tasksBar(tasksBarProps{Tasks: running})
})
