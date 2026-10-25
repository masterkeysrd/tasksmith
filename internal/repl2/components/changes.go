package components

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// DiffProps defines the properties for rendering a unified diff.
type DiffProps struct {
	Text string
}

// DiffView is a trail functional component rendering unified diff lines styled with theme.UseTheme().
var DiffView = trail.FC("DiffView", func(props DiffProps) trail.Node {
	if props.Text == "" {
		return nil
	}

	scheme := theme.UseTheme()

	headerStyle := style.S().Bold(true)
	hunkStyle := style.S()
	addStyle := style.S()
	delStyle := style.S()
	plainStyle := style.S()

	if scheme != nil {
		if scheme.Color.Text.Primary != nil {
			headerStyle = headerStyle.Foreground(scheme.Color.Text.Primary)
		}
		if scheme.Color.Surface.Info != nil {
			hunkStyle = hunkStyle.Foreground(scheme.Color.Surface.Info)
		}
		if scheme.Color.Surface.Success != nil {
			addStyle = addStyle.Foreground(scheme.Color.Surface.Success)
		}
		if scheme.Color.Text.Error != nil {
			delStyle = delStyle.Foreground(scheme.Color.Text.Error)
		}
		if scheme.Color.Text.Secondary != nil {
			plainStyle = plainStyle.Foreground(scheme.Color.Text.Secondary)
		}
	}

	lines := strings.Split(props.Text, "\n")
	var nodes []trail.Node
	for _, line := range lines {
		s := plainStyle
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			s = headerStyle
		} else if strings.HasPrefix(line, "@@") {
			s = hunkStyle
		} else if strings.HasPrefix(line, "+") {
			s = addStyle
		} else if strings.HasPrefix(line, "-") {
			s = delStyle
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
		return trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, trail.Text("No tracked file changes."))
	}

	scheme := theme.UseTheme()
	idStyle := style.S()
	kindStyle := style.S().Bold(true)
	pathStyle := style.S()
	addStyle := style.S()

	if scheme != nil {
		if scheme.Color.Text.Secondary != nil {
			idStyle = idStyle.Foreground(scheme.Color.Text.Secondary)
		}
		if scheme.Color.Text.Purple != nil {
			kindStyle = kindStyle.Foreground(scheme.Color.Text.Purple)
		}
		if scheme.Color.Text.Primary != nil {
			pathStyle = pathStyle.Foreground(scheme.Color.Text.Primary)
		}
		if scheme.Color.Surface.Success != nil {
			addStyle = addStyle.Foreground(scheme.Color.Surface.Success)
		}
	}

	var rows []trail.Node
	for _, c := range props.Changes {
		diffStats := fmt.Sprintf("+%d/-%d", c.Additions, c.Deletions)
		rows = append(rows, kitex.TR(kitex.TRProps{},
			kitex.TD(kitex.TDProps{
				Style: style.S().Padding(0, 2, 0, 0),
			}, trail.Span(trail.SpanProps{
				Style: idStyle,
			}, trail.Text(c.ID))),
			kitex.TD(kitex.TDProps{
				Style: style.S().Padding(0, 2, 0, 0),
			}, trail.Span(trail.SpanProps{
				Style: kindStyle,
			}, trail.Text(c.Kind))),
			kitex.TD(kitex.TDProps{
				Style: style.S().Padding(0, 2, 0, 0),
			}, trail.Span(trail.SpanProps{
				Style: pathStyle,
			}, trail.Text(c.Path))),
			kitex.TD(kitex.TDProps{},
				trail.Span(trail.SpanProps{
					Style: addStyle,
				}, trail.Text(diffStats))),
		))
	}

	return kitex.Table(kitex.TableProps{
		Style: style.S().Margin(0, 0, 0, 2),
	}, kitex.TBody(kitex.TBodyProps{}, rows...))
})
