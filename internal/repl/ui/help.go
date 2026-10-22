package ui

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// HelpEntry describes a command entry in the help component.
type HelpEntry struct {
	Name        string
	Aliases     []string
	Usage       string
	Description string
}

// HelpProps defines the properties for the Help component.
type HelpProps struct {
	Entries []HelpEntry
}

// Help is a trail functional component rendering slash commands in a structured table.
var Help = trail.FC("Help", func(props HelpProps) trail.Node {
	var rows []trail.Node

	for _, entry := range props.Entries {
		usage := entry.Usage
		if len(entry.Aliases) > 0 {
			aliases := make([]string, len(entry.Aliases))
			for i, a := range entry.Aliases {
				aliases[i] = "/" + a
			}
			usage = fmt.Sprintf("%s (%s)", usage, strings.Join(aliases, ", "))
		}

		rows = append(rows, kitex.TR(kitex.TRProps{},
			kitex.TD(kitex.TDProps{
				Style: style.S().Padding(0, 3, 0, 0),
			}, trail.Span(trail.SpanProps{
				Style: theme.ToolCallStyle,
			}, trail.Text(usage))),
			kitex.TD(kitex.TDProps{},
				trail.Span(trail.SpanProps{
					Style: theme.AssistantMsgStyle,
				}, trail.Text(entry.Description))),
		))
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn),
	},
		trail.Span(trail.SpanProps{
			Style: theme.ApprovalTitleStyle,
		}, trail.Text("Available commands:")),
		kitex.Table(kitex.TableProps{
			Style: style.S().Margin(0, 0, 0, 2),
		}, kitex.TBody(kitex.TBodyProps{}, rows...)),
	)
})
