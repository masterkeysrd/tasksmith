package ui

import (
	"slices"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// Option represents a selectable item in an interactive command picker or autocomplete list.
type Option struct {
	Value       string
	Label       string
	Description string
	Columns     []string
	Current     bool
}

// OptionListProps defines the properties for the OptionList component.
type OptionListProps struct {
	Options   []Option
	Selected  int    // index of highlighted row, or -1 if no row is highlighted with "❯ "
	EmptyText string // optional message when len(Options) == 0 (e.g. "  No matching options.")
}

// withCurrentSuffix appends " • current" (or returns "current" if text is empty) when current is true.
func withCurrentSuffix(text string, current bool) string {
	if !current {
		return text
	}
	if text != "" {
		return text + " • current"
	}
	return "current"
}

// OptionList is a trail functional component rendering a selectable or informational list of options as a table.
var OptionList = trail.FC("OptionList", func(props OptionListProps) trail.Node {
	if len(props.Options) == 0 {
		if props.EmptyText == "" {
			return nil
		}
		return trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, trail.Span(trail.SpanProps{
			Style: theme.PickerOptionStyle.WhiteSpace(style.WhiteSpacePre),
		}, trail.Text(props.EmptyText)))
	}

	hasColumns := len(props.Options[0].Columns) > 0
	trs := make([]trail.Node, 0, len(props.Options))

	for i, opt := range props.Options {
		rowStyle := theme.PickerOptionStyle
		prefix := "  "
		if props.Selected < 0 {
			rowStyle = theme.StatusBarStyle
		} else if i == props.Selected {
			rowStyle = theme.PickerSelectedStyle
			prefix = "❯ "
		}

		var tds []trail.Node
		if hasColumns && len(opt.Columns) > 0 {
			cols := slices.Clone(opt.Columns)
			last := len(cols) - 1
			cols[last] = withCurrentSuffix(cols[last], opt.Current)

			tds = append(tds, kitex.TD(kitex.TDProps{
				Style: style.S().Padding(0, 2, 0, 0).WhiteSpace(style.WhiteSpacePre),
			}, trail.Span(trail.SpanProps{
				Style: rowStyle.WhiteSpace(style.WhiteSpacePre),
			}, trail.Text(prefix+cols[0]))))

			for _, col := range cols[1:] {
				tds = append(tds, kitex.TD(kitex.TDProps{
					Style: style.S().Padding(0, 1, 0, 1),
				}, trail.Span(trail.SpanProps{Style: rowStyle}, trail.Text(col))))
			}
		} else {
			tds = []trail.Node{
				kitex.TD(kitex.TDProps{
					Style: style.S().Padding(0, 2, 0, 0).WhiteSpace(style.WhiteSpacePre),
				}, trail.Span(trail.SpanProps{
					Style: rowStyle.WhiteSpace(style.WhiteSpacePre),
				}, trail.Text(prefix+opt.Label))),
				kitex.TD(kitex.TDProps{
					Style: style.S(),
				}, trail.Span(trail.SpanProps{
					Style: theme.StatusBarStyle,
				}, trail.Text(withCurrentSuffix(opt.Description, opt.Current)))),
			}
		}

		trs = append(trs, kitex.TR(kitex.TRProps{}, tds...))
	}

	return kitex.Table(kitex.TableProps{
		Style: style.S().Width(style.Percent(100)),
	}, kitex.TBody(kitex.TBodyProps{}, trs...))
})
