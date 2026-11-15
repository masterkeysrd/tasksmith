package components

import (
	"fmt"
	"slices"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// DefaultPickerMaxVisible is the default number of visible options in a picker before scrolling.
const DefaultPickerMaxVisible = 8

// DefaultAutocompleteMaxVisible is the default number of visible options in autocomplete.
const DefaultAutocompleteMaxVisible = 5

// Column defines the width, alignment, and optional cell style for a column in OptionList.
type Column struct {
	Width int             // 0 = flexible / remaining space, >0 = fixed width in cells
	Align style.TextAlign // style.TextAlignLeft (default), style.TextAlignCenter, style.TextAlignRight
	Style style.Style     // optional style override for unselected rows
}

// Option represents a selectable item in an interactive command picker or autocomplete list.
type Option struct {
	Value       string
	Label       string
	Description string
	Cells       []string
	CellNodes   []trail.Node
	Current     bool
	// Icon renders in the cursor gutter between the selection marker and the first
	// cell, so a row can carry a status glyph without spending a column on it.
	Icon trail.Node
}

// OptionListProps defines the properties for the OptionList component.
type OptionListProps struct {
	Options    []Option
	Selected   int      // index of highlighted row, or -1 if no row is highlighted with "❯ "
	EmptyText  string   // optional message when len(Options) == 0 (e.g. "  No matching options.")
	MaxVisible int      // max number of rows visible simultaneously before sliding-window pagination
	Columns    []Column // column specifications (width, alignment, style override)
	Styles     *OptionListStyles
	// GroupLabel returns the heading a row belongs under. A muted heading row is
	// rendered whenever it changes, which keeps a listing scannable without
	// spending a column on the grouping.
	GroupLabel func(opt Option) string
}

// OptionListStyles optionally overrides the theme's list styles.
type OptionListStyles struct {
	Option      style.Style
	Selected    style.Style
	Description style.Style
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

// groupOf returns the heading an option belongs under, or "" when the listing is
// ungrouped or the accessor is unset.
func (p OptionListProps) groupOf(opt Option) string {
	if p.GroupLabel == nil {
		return ""
	}
	return strings.TrimSpace(p.GroupLabel(opt))
}

// optionSectionRow renders one muted group heading. It spans the table's columns
// so the heading sits at the listing's left edge instead of inside the first
// column, which carries the cursor gutter. The heading lines up with a row's
// status glyph, and it gets a blank line above it so adjacent groups read as
// separate blocks. The listing's very first row keeps that line for whatever the
// caller already printed above the table, such as a title or count.
func optionSectionRow(label string, numCols int, first bool, sty style.Style) trail.Node {
	top := 0
	if !first {
		top = 1
	}
	return kitex.TR(kitex.TRProps{}, kitex.TD(kitex.TDProps{
		Style:   style.S().Padding(top, 0, 0, 2),
		ColSpan: numCols,
	}, trail.Span(trail.SpanProps{
		Style: sty.WhiteSpace(style.WhiteSpacePre),
	}, trail.Text(label))))
}

// optionIconRow renders the first cell of a row that carries a status glyph: the
// cursor gutter, the glyph (already styled by the caller), then the cell's own
// content, which keeps its row styling.
func optionIconRow(prefix string, icon trail.Node, cell string, sty style.Style) trail.Node {
	children := []trail.Node{trail.Span(trail.SpanProps{
		Style: style.S().WhiteSpace(style.WhiteSpacePre),
	}, trail.Text(prefix))}
	if icon != nil {
		children = append(children, icon)
	}
	if cell != "" {
		children = append(children, trail.Span(trail.SpanProps{
			Style: sty,
		}, trail.Text(" "+cell)))
	}
	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow).MinWidth(style.Cells(0)),
	}, children...)
}

// OptionList is a trail functional component rendering a selectable or informational list of options as a table.
var OptionList = trail.FC("OptionList", func(props OptionListProps) trail.Node {
	offset, setOffset := kitex.UseState(0)
	scheme := theme.UseTheme()
	styles := OptionListStyles{
		Option:      style.S().Foreground(scheme.Color.Text.Secondary),
		Selected:    style.S().Bold(true).Foreground(scheme.Color.Icon.Primary),
		Description: style.S().Foreground(scheme.Color.Text.Muted),
	}
	if props.Styles != nil {
		styles = *props.Styles
	}
	// Group headings read as chrome rather than content, so they always take the
	// muted heading style no matter which option styles a caller overrode.
	sectionStyle := style.S().Bold(true).Foreground(scheme.Color.Text.Muted)

	start := 0
	end := len(props.Options)
	targetOff := 0

	if props.MaxVisible > 0 && len(props.Options) > props.MaxVisible {
		off := offset()
		if props.Selected >= 0 {
			if props.Selected < off {
				off = props.Selected
			} else if props.Selected >= off+props.MaxVisible {
				off = props.Selected - props.MaxVisible + 1
			}
		}
		maxOff := len(props.Options) - props.MaxVisible
		if off < 0 {
			off = 0
		} else if off > maxOff {
			off = maxOff
		}

		targetOff = off
		start = off
		end = off + props.MaxVisible
	}

	kitex.UseEffect(func() {
		if targetOff != offset() {
			setOffset(targetOff)
		}
	}, []any{targetOff})

	if len(props.Options) == 0 {
		if props.EmptyText == "" {
			return nil
		}
		return trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, trail.Span(trail.SpanProps{
			Style: styles.Option.WhiteSpace(style.WhiteSpacePre),
		}, trail.Text(props.EmptyText)))
	}

	above := start
	below := len(props.Options) - end

	visibleOpts := props.Options[start:end]
	hasCells := len(visibleOpts[0].Cells) > 0
	trs := make([]trail.Node, 0, len(visibleOpts))
	numCols := max(len(props.Columns), 1)
	previousGroup := ""
	if start > 0 {
		previousGroup = props.groupOf(props.Options[start-1])
	}

	for relIdx, opt := range visibleOpts {
		origIdx := start + relIdx
		rowStyle := styles.Option
		prefix := "  "
		if props.Selected < 0 {
			rowStyle = styles.Description
		} else if origIdx == props.Selected {
			rowStyle = styles.Selected
			prefix = "❯ "
		}

		group := props.groupOf(opt)
		if group != "" && group != previousGroup {
			trs = append(trs, optionSectionRow(group, numCols, len(trs) == 0, sectionStyle))
		}
		previousGroup = group

		var tds []trail.Node
		if hasCells && len(opt.Cells) > 0 {
			cells := slices.Clone(opt.Cells)
			last := len(cells) - 1
			cells[last] = withCurrentSuffix(cells[last], opt.Current)

			for colIdx, cell := range cells {
				tdStyle := style.S().Padding(0, 2, 0, 0)
				text := cell
				spanStyle := rowStyle
				if colIdx == 0 {
					tdStyle = tdStyle.WhiteSpace(style.WhiteSpacePre)
					text = prefix + cell
					spanStyle = rowStyle.WhiteSpace(style.WhiteSpacePre)
				}
				if colIdx < len(props.Columns) {
					colSpec := props.Columns[colIdx]
					if colSpec.Width > 0 {
						tdStyle = tdStyle.Width(style.Cells(colSpec.Width)).
							MinWidth(style.Cells(colSpec.Width))
						if colIdx != 0 {
							tdStyle = tdStyle.WhiteSpace(style.WhiteSpaceNoWrap)
						}
					} else {
						tdStyle = tdStyle.Width(style.Percent(100))
					}
					if colSpec.Align != 0 {
						tdStyle = tdStyle.TextAlign(colSpec.Align)
						if colSpec.Align == style.TextAlignRight {
							if colIdx == len(cells)-1 {
								tdStyle = tdStyle.Padding(0, 0, 0, 0)
							} else {
								tdStyle = tdStyle.Padding(0, 2, 0, 0)
							}
						}
					}
					if props.Selected != origIdx {
						spanStyle = rowStyle.Merge(colSpec.Style)
					}
				}
				if colIdx == 0 && opt.Icon != nil {
					tds = append(tds, kitex.TD(kitex.TDProps{
						Style: tdStyle,
					}, optionIconRow(prefix, opt.Icon, cell, spanStyle)))
				} else if colIdx < len(opt.CellNodes) && opt.CellNodes[colIdx] != nil {
					tds = append(tds, kitex.TD(kitex.TDProps{
						Style: tdStyle,
					}, opt.CellNodes[colIdx]))
				} else {
					tds = append(tds, kitex.TD(kitex.TDProps{
						Style: tdStyle,
					}, trail.Span(trail.SpanProps{Style: spanStyle}, trail.Text(text))))
				}
			}
		} else {
			desc := format.CompactSingleLine(opt.Description, 60)
			labelCell := trail.Span(trail.SpanProps{
				Style: rowStyle.WhiteSpace(style.WhiteSpacePre),
			}, trail.Text(prefix+opt.Label))
			if opt.Icon != nil {
				labelCell = optionIconRow(prefix, opt.Icon, opt.Label, rowStyle.WhiteSpace(style.WhiteSpacePre))
			}
			tds = []trail.Node{
				kitex.TD(kitex.TDProps{
					Style: style.S().Padding(0, 2, 0, 0).WhiteSpace(style.WhiteSpacePre),
				}, labelCell),
				kitex.TD(kitex.TDProps{
					Style: style.S(),
				}, trail.Span(trail.SpanProps{
					Style: styles.Description,
				}, trail.Text(withCurrentSuffix(desc, opt.Current)))),
			}
		}

		trs = append(trs, kitex.TR(kitex.TRProps{}, tds...))
	}

	tableStyle := style.S()
	if len(props.Columns) > 0 {
		tableStyle = tableStyle.Width(style.Percent(100))
	}
	tableNode := kitex.Table(kitex.TableProps{
		Style: tableStyle,
	}, kitex.TBody(kitex.TBodyProps{}, trs...))

	if above == 0 && below == 0 {
		return tableNode
	}

	var nodes []trail.Node
	if above > 0 {
		nodes = append(nodes, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, trail.Span(trail.SpanProps{
			Style: styles.Description.WhiteSpace(style.WhiteSpacePre),
		}, trail.Text(fmt.Sprintf("  ↑ %d more", above)))))
	}

	nodes = append(nodes, tableNode)

	if below > 0 {
		nodes = append(nodes, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, trail.Span(trail.SpanProps{
			Style: styles.Description.WhiteSpace(style.WhiteSpacePre),
		}, trail.Text(fmt.Sprintf("  ↓ %d more", below)))))
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn).Width(style.Percent(100)),
	}, nodes...)
})
