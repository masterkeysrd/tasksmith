package ui

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// DiffProps defines the properties for rendering a unified diff.
type DiffProps struct {
	Text string
}

// Diff is a trail functional component rendering unified diff lines styled with
// theme.DiffHeaderStyle, theme.DiffHunkStyle, theme.DiffAddStyle, and theme.DiffDelStyle.
var Diff = trail.FC("Diff", func(props DiffProps) trail.Node {
	if props.Text == "" {
		return nil
	}

	lines := strings.Split(props.Text, "\n")
	var nodes []trail.Node
	for _, line := range lines {
		s := style.S()
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			s = theme.DiffHeaderStyle
		} else if strings.HasPrefix(line, "@@") {
			s = theme.DiffHunkStyle
		} else if strings.HasPrefix(line, "+") {
			s = theme.DiffAddStyle
		} else if strings.HasPrefix(line, "-") {
			s = theme.DiffDelStyle
		}

		nodes = append(nodes, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, trail.Span(trail.SpanProps{
			Style: s,
		}, trail.Text(line))))
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn),
	}, nodes...)
})

// ChangesListProps defines the properties for rendering tracked file changes.
type ChangesListProps struct {
	Changes []api.FileChange
}

// ChangesList is a trail functional component rendering a table of tracked file changes.
var ChangesList = trail.FC("ChangesList", func(props ChangesListProps) trail.Node {
	if len(props.Changes) == 0 {
		return CommandOutput(CommandOutputProps{
			Command: "/changes",
			Output:  "No tracked file changes.",
		})
	}

	var rows []trail.Node
	for _, c := range props.Changes {
		diffStats := fmt.Sprintf("+%d/-%d", c.Additions, c.Deletions)
		rows = append(rows, kitex.TR(kitex.TRProps{},
			kitex.TD(kitex.TDProps{
				Style: style.S().Padding(0, 2, 0, 0),
			}, trail.Span(trail.SpanProps{
				Style: theme.ToolCallStyle,
			}, trail.Text(c.ID))),
			kitex.TD(kitex.TDProps{
				Style: style.S().Padding(0, 2, 0, 0),
			}, trail.Span(trail.SpanProps{
				Style: theme.ActionBadgeStyle,
			}, trail.Text(c.Kind))),
			kitex.TD(kitex.TDProps{
				Style: style.S().Padding(0, 2, 0, 0),
			}, trail.Span(trail.SpanProps{
				Style: theme.AssistantMsgStyle,
			}, trail.Text(c.Path))),
			kitex.TD(kitex.TDProps{},
				trail.Span(trail.SpanProps{
					Style: theme.DiffAddStyle,
				}, trail.Text(diffStats))),
		))
	}

	table := kitex.Table(kitex.TableProps{
		Style: style.S().Margin(0, 0, 0, 2),
	}, kitex.TBody(kitex.TBodyProps{}, rows...))

	return CommandOutput(CommandOutputProps{
		Command: "/changes",
		Output:  "Tracked file changes:",
	}, table)
})
