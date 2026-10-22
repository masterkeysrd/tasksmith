package ui

import (
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
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
