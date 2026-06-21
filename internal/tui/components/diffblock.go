package components

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/core/diff"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// DiffLine represents a line in a diff pane.
type DiffLine struct {
	Op      diff.Op
	NumA    int
	NumB    int
	Content string
}

// AlignedRow represents a row in either split or unified view.
type AlignedRow struct {
	IsHeader   bool
	HeaderText string
	Left       *DiffLine
	Right      *DiffLine
}

// DiffBlockProps defines properties for the DiffBlock component.
type DiffBlockProps struct {
	Diff  string
	Lang  string
	Split bool
	Style style.Style
}

// DiffBlock renders a syntax-highlighted diff, supporting both
// unified and side-by-side split views.
var DiffBlock = kitex.FC("DiffBlock", func(props DiffBlockProps) kitex.Node {
	t := theme.UseTheme()
	diffStr := props.Diff
	lang := props.Lang

	if strings.TrimSpace(diffStr) == "" {
		return nil
	}

	rows := parseUnifiedDiff(diffStr)
	if len(rows) == 0 {
		return nil
	}

	// Fetch Chroma lexer
	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	// Theme colors
	var redBg color.Color
	var greenBg color.Color
	var cyanBg color.Color
	var textCol color.Color
	var borderCol color.Color

	if t != nil {
		redBg = t.Color.Surface.ErrorFocus
		greenBg = t.Color.Surface.SuccessFocus
		cyanBg = t.Color.Surface.InfoFocus
		textCol = t.Color.Text.Secondary
		borderCol = t.Color.Border.Primary
	}

	// Helper function to tokenize and highlight a line of code.
	highlightLine := func(line string, bg color.Color) []kitex.Node {
		iterator, err := lexer.Tokenise(nil, line)
		var spans []kitex.Node
		if err != nil {
			spans = []kitex.Node{kitex.Span(
				kitex.SpanProps{Style: style.S().Background(bg).Foreground(textCol)},
				kitex.Text(line),
			)}
		} else {
			for tok := iterator(); tok != chroma.EOF; tok = iterator() {
				tokenStyle := ResolveTokenStyle(t, tok.Type)
				if bg != nil {
					tokenStyle = tokenStyle.Background(bg)
				}
				spans = append(spans, kitex.Span(
					kitex.SpanProps{Style: tokenStyle},
					kitex.Text(tok.Value),
				))
			}
		}
		return spans
	}

	rowStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		Width(style.Percent(100))

	gutterNumStyle := style.S().
		Width(style.Cells(5)).
		TextAlign(style.TextAlignRight).
		PaddingRight(1)
	if t != nil {
		gutterNumStyle = gutterNumStyle.Foreground(t.Color.Text.Tertiary)
	}

	codeStyle := style.S().
		WhiteSpace(style.WhiteSpacePre).
		OverflowX(style.OverflowHidden)
	if t != nil {
		codeStyle = codeStyle.Foreground(t.Color.Text.Secondary)
	}

	wrapperStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100)).
		WhiteSpace(style.WhiteSpacePre).
		Merge(props.Style)

	var renderedRows []kitex.Node
 
	gutterStyle := style.S().
		Width(style.Cells(12)).
		WhiteSpace(style.WhiteSpacePre)
	if t != nil {
		gutterStyle = gutterStyle.Foreground(t.Color.Text.Tertiary)
	}
 
	separatorStyle := style.S().
		Width(style.Cells(1)).
		WhiteSpace(style.WhiteSpacePre)
	if t != nil {
		separatorStyle = separatorStyle.Foreground(t.Color.Border.Primary)
	}
 
	codeBoxStyle := style.S().
		Flex(1, 1, style.Cells(0)).
		MinHeight(style.Cells(0)).
		WhiteSpace(style.WhiteSpacePre).
		OverflowX(style.OverflowAuto)
	if t != nil {
		codeBoxStyle = codeBoxStyle.Foreground(t.Color.Text.Secondary)
	}
 
	if !props.Split {
		// Unified inline layout
		var gutterSpans []kitex.Node
		var sepSpans []kitex.Node
		var codeSpans []kitex.Node
 
		for _, r := range rows {
			if r.IsHeader {
				var bg color.Color
				var textFg color.Color = textCol
				if strings.HasPrefix(r.HeaderText, "@@") {
					bg = cyanBg
				}
 
				gutterSpans = append(gutterSpans, kitex.Span(
					kitex.SpanProps{Style: style.S().Background(bg).Foreground(textFg)},
					kitex.Text("            \n"), // 12 spaces
				))
 
				var sepCol color.Color
				if t != nil {
					sepCol = borderCol
				}
				sepSpans = append(sepSpans, kitex.Span(
					kitex.SpanProps{Style: style.S().Background(bg).Foreground(sepCol)},
					kitex.Text("│\n"),
				))
 
				codeSpans = append(codeSpans, kitex.Span(
					kitex.SpanProps{Style: style.S().Background(bg).Foreground(textFg).Bold(true)},
					kitex.Text(r.HeaderText),
				))
				if bg != nil {
					paddingLength := 500 - len(r.HeaderText)
					if paddingLength > 0 {
						codeSpans = append(codeSpans, kitex.Span(
							kitex.SpanProps{Style: style.S().Background(bg)},
							kitex.Text(strings.Repeat(" ", paddingLength)),
						))
					}
				}
				codeSpans = append(codeSpans, kitex.Span(
					kitex.SpanProps{Style: style.S().Background(bg)},
					kitex.Text("\n"),
				))
				continue
			}
 
			var line *DiffLine
			var numAStr, numBStr, sign string
			var bg color.Color

			if r.Left != nil && r.Left.Op == diff.OpDelete {
				line = r.Left
				numAStr = fmt.Sprintf("%d", r.Left.NumA)
				sign = "-"
				bg = redBg
			} else if r.Right != nil && r.Right.Op == diff.OpInsert {
				line = r.Right
				numBStr = fmt.Sprintf("%d", r.Right.NumB)
				sign = "+"
				bg = greenBg
			} else if r.Left != nil { // Unchanged
				line = r.Left
				numAStr = fmt.Sprintf("%d", r.Left.NumA)
				numBStr = fmt.Sprintf("%d", r.Right.NumB)
				sign = " "
			}
 
			if line == nil {
				continue
			}
 
			// Gutter span
			gutterText := fmt.Sprintf("%5s%5s %s\n", numAStr, numBStr, sign)
			var currGutterStyle style.Style
			if bg != nil {
				currGutterStyle = style.S().Background(bg)
			}
			if t != nil {
				currGutterStyle = currGutterStyle.Foreground(t.Color.Text.Tertiary)
			}
			gutterSpans = append(gutterSpans, kitex.Span(
				kitex.SpanProps{Style: currGutterStyle},
				kitex.Text(gutterText),
			))
 
			// Separator span
			var sepCol color.Color
			if t != nil {
				sepCol = borderCol
			}
			sepSpans = append(sepSpans, kitex.Span(
				kitex.SpanProps{Style: style.S().Background(bg).Foreground(sepCol)},
				kitex.Text("│\n"),
			))
 
			// Code spans (highlighted)
			codeSpans = append(codeSpans, highlightLine(line.Content, bg)...)
 
			// Padding to stretch background color
			if bg != nil {
				paddingLength := 500 - len(line.Content)
				if paddingLength > 0 {
					codeSpans = append(codeSpans, kitex.Span(
						kitex.SpanProps{Style: style.S().Background(bg)},
						kitex.Text(strings.Repeat(" ", paddingLength)),
					))
				}
			}
			codeSpans = append(codeSpans, kitex.Span(
				kitex.SpanProps{Style: style.S().Background(bg)},
				kitex.Text("\n"),
			))
		}
 
		renderedRows = append(renderedRows, kitex.Box(kitex.BoxProps{Style: rowStyle},
			kitex.Box(kitex.BoxProps{Style: gutterStyle}, gutterSpans...),
			kitex.Box(kitex.BoxProps{Style: separatorStyle}, sepSpans...),
			kitex.Box(kitex.BoxProps{Style: codeBoxStyle}, codeSpans...),
		))
	} else {
		// Side-by-side split layout
		var leftGutterSpans []kitex.Node
		var leftSepSpans []kitex.Node
		var leftCodeSpans []kitex.Node
		var sepSpans []kitex.Node
		var rightGutterSpans []kitex.Node
		var rightSepSpans []kitex.Node
		var rightCodeSpans []kitex.Node
 
		for _, r := range rows {
			if r.IsHeader {
				var bg color.Color
				var textFg color.Color = textCol
				if strings.HasPrefix(r.HeaderText, "@@") {
					bg = cyanBg
				}
 
				// Left Gutter
				leftGutterSpans = append(leftGutterSpans, kitex.Span(
					kitex.SpanProps{Style: style.S().Background(bg).Foreground(textFg)},
					kitex.Text("       \n"), // 7 spaces
				))
 
				// Left Separator
				var sepCol color.Color
				if t != nil {
					sepCol = borderCol
				}
				leftSepSpans = append(leftSepSpans, kitex.Span(
					kitex.SpanProps{Style: style.S().Background(bg).Foreground(sepCol)},
					kitex.Text("│\n"),
				))
 
				// Left Code
				leftCodeSpans = append(leftCodeSpans, kitex.Span(
					kitex.SpanProps{Style: style.S().Background(bg).Foreground(textFg).Bold(true)},
					kitex.Text(r.HeaderText),
				))
				if bg != nil {
					paddingLength := 500 - len(r.HeaderText)
					if paddingLength > 0 {
						leftCodeSpans = append(leftCodeSpans, kitex.Span(
							kitex.SpanProps{Style: style.S().Background(bg)},
							kitex.Text(strings.Repeat(" ", paddingLength)),
						))
					}
				}
				leftCodeSpans = append(leftCodeSpans, kitex.Span(
					kitex.SpanProps{Style: style.S().Background(bg)},
					kitex.Text("\n"),
				))
 
				// Middle Pane Separator
				sepSpans = append(sepSpans, kitex.Span(
					kitex.SpanProps{Style: style.S().Background(bg).Foreground(sepCol)},
					kitex.Text("│\n"),
				))
 
				// Right Gutter
				rightGutterSpans = append(rightGutterSpans, kitex.Span(
					kitex.SpanProps{Style: style.S().Background(bg).Foreground(textFg)},
					kitex.Text("       \n"), // 7 spaces
				))
 
				// Right Separator
				rightSepSpans = append(rightSepSpans, kitex.Span(
					kitex.SpanProps{Style: style.S().Background(bg).Foreground(sepCol)},
					kitex.Text("│\n"),
				))
 
				// Right Code
				if bg != nil {
					rightCodeSpans = append(rightCodeSpans, kitex.Span(
						kitex.SpanProps{Style: style.S().Background(bg)},
						kitex.Text(strings.Repeat(" ", 500)),
					))
				}
				rightCodeSpans = append(rightCodeSpans, kitex.Span(
					kitex.SpanProps{Style: style.S().Background(bg)},
					kitex.Text("\n"),
				))
				continue
			}
 
			// Render Left side (Original)
			var leftGNum, leftSign string
			var leftBg color.Color
			var leftLine *DiffLine
 
			if r.Left != nil {
				leftLine = r.Left
				leftGNum = fmt.Sprintf("%d", r.Left.NumA)
				if r.Left.Op == diff.OpDelete {
					leftSign = "-"
					leftBg = redBg
				} else {
					leftSign = " "
				}
			}
 
			// Render Right side (Modified)
			var rightGNum, rightSign string
			var rightBg color.Color
			var rightLine *DiffLine
 
			if r.Right != nil {
				rightLine = r.Right
				rightGNum = fmt.Sprintf("%d", r.Right.NumB)
				if r.Right.Op == diff.OpInsert {
					rightSign = "+"
					rightBg = greenBg
				} else {
					rightSign = " "
				}
			}
 
			// 1. Left Gutter
			var lgStyle style.Style
			if leftBg != nil {
				lgStyle = style.S().Background(leftBg)
			}
			if t != nil {
				lgStyle = lgStyle.Foreground(t.Color.Text.Tertiary)
			}
			leftGutterSpans = append(leftGutterSpans, kitex.Span(
				kitex.SpanProps{Style: lgStyle},
				kitex.Text(fmt.Sprintf("%5s %s\n", leftGNum, leftSign)),
			))
 
			// 2. Left Separator
			var sepCol color.Color
			if t != nil {
				sepCol = borderCol
			}
			leftSepSpans = append(leftSepSpans, kitex.Span(
				kitex.SpanProps{Style: style.S().Background(leftBg).Foreground(sepCol)},
				kitex.Text("│\n"),
			))
 
			// 3. Left Code
			if leftLine != nil {
				leftCodeSpans = append(leftCodeSpans, highlightLine(leftLine.Content, leftBg)...)
				if leftBg != nil {
					paddingLength := 500 - len(leftLine.Content)
					if paddingLength > 0 {
						leftCodeSpans = append(leftCodeSpans, kitex.Span(
							kitex.SpanProps{Style: style.S().Background(leftBg)},
							kitex.Text(strings.Repeat(" ", paddingLength)),
						))
					}
				}
			} else {
				if leftBg != nil {
					leftCodeSpans = append(leftCodeSpans, kitex.Span(
						kitex.SpanProps{Style: style.S().Background(leftBg)},
						kitex.Text(strings.Repeat(" ", 500)),
					))
				}
			}
			leftCodeSpans = append(leftCodeSpans, kitex.Span(
				kitex.SpanProps{Style: style.S().Background(leftBg)},
				kitex.Text("\n"),
			))
 
			// 4. Middle Pane Separator
			sepSpans = append(sepSpans, kitex.Span(
				kitex.SpanProps{Style: style.S().Foreground(sepCol)},
				kitex.Text("│\n"),
			))
 
			// 5. Right Gutter
			var rgStyle style.Style
			if rightBg != nil {
				rgStyle = style.S().Background(rightBg)
			}
			if t != nil {
				rgStyle = rgStyle.Foreground(t.Color.Text.Tertiary)
			}
			rightGutterSpans = append(rightGutterSpans, kitex.Span(
				kitex.SpanProps{Style: rgStyle},
				kitex.Text(fmt.Sprintf("%5s %s\n", rightGNum, rightSign)),
			))
 
			// 6. Right Separator
			rightSepSpans = append(rightSepSpans, kitex.Span(
				kitex.SpanProps{Style: style.S().Background(rightBg).Foreground(sepCol)},
				kitex.Text("│\n"),
			))
 
			// 7. Right Code
			if rightLine != nil {
				rightCodeSpans = append(rightCodeSpans, highlightLine(rightLine.Content, rightBg)...)
				if rightBg != nil {
					paddingLength := 500 - len(rightLine.Content)
					if paddingLength > 0 {
						rightCodeSpans = append(rightCodeSpans, kitex.Span(
							kitex.SpanProps{Style: style.S().Background(rightBg)},
							kitex.Text(strings.Repeat(" ", paddingLength)),
						))
					}
				}
			} else {
				if rightBg != nil {
					rightCodeSpans = append(rightCodeSpans, kitex.Span(
						kitex.SpanProps{Style: style.S().Background(rightBg)},
						kitex.Text(strings.Repeat(" ", 500)),
					))
				}
			}
			rightCodeSpans = append(rightCodeSpans, kitex.Span(
				kitex.SpanProps{Style: style.S().Background(rightBg)},
				kitex.Text("\n"),
			))
		}
 
		leftPane := kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				Flex(1, 1, style.Cells(0)).
				OverflowX(style.OverflowHidden),
		},
			kitex.Box(kitex.BoxProps{Style: style.S().Width(style.Cells(7)).WhiteSpace(style.WhiteSpacePre)}, leftGutterSpans...),
			kitex.Box(kitex.BoxProps{Style: separatorStyle}, leftSepSpans...),
			kitex.Box(kitex.BoxProps{Style: codeBoxStyle}, leftCodeSpans...),
		)
 
		middleSep := kitex.Box(kitex.BoxProps{Style: separatorStyle}, sepSpans...)
 
		rightPane := kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				Flex(1, 1, style.Cells(0)).
				OverflowX(style.OverflowHidden),
		},
			kitex.Box(kitex.BoxProps{Style: style.S().Width(style.Cells(7)).WhiteSpace(style.WhiteSpacePre)}, rightGutterSpans...),
			kitex.Box(kitex.BoxProps{Style: separatorStyle}, rightSepSpans...),
			kitex.Box(kitex.BoxProps{Style: codeBoxStyle}, rightCodeSpans...),
		)
 
		renderedRows = append(renderedRows, kitex.Box(kitex.BoxProps{Style: rowStyle},
			leftPane,
			middleSep,
			rightPane,
		))
	}
 
	return kitex.Box(kitex.BoxProps{Style: wrapperStyle}, renderedRows...)
})

// parseUnifiedDiff parses a unified diff string and aligns the operations line-by-line.
func parseUnifiedDiff(diffStr string) []AlignedRow {
	var rows []AlignedRow
	lines := strings.Split(diffStr, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var lineA, lineB int
	var pendingDeletes []*DiffLine
	var pendingInserts []*DiffLine

	flushPending := func() {
		nDel := len(pendingDeletes)
		nIns := len(pendingInserts)
		maxRows := nDel
		if nIns > maxRows {
			maxRows = nIns
		}

		for idx := 0; idx < maxRows; idx++ {
			var left, right *DiffLine
			if idx < nDel {
				left = pendingDeletes[idx]
			}
			if idx < nIns {
				right = pendingInserts[idx]
			}
			rows = append(rows, AlignedRow{
				Left:  left,
				Right: right,
			})
		}
		pendingDeletes = nil
		pendingInserts = nil
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			flushPending()
			rows = append(rows, AlignedRow{
				IsHeader:   true,
				HeaderText: line,
			})
			continue
		}

		if strings.HasPrefix(line, "@@ ") {
			flushPending()
			var aStart, aLen, bStart, bLen int
			_, _ = fmt.Sscanf(line, "@@ -%d,%d +%d,%d @@", &aStart, &aLen, &bStart, &bLen)
			if aStart == 0 {
				_, _ = fmt.Sscanf(line, "@@ -%d +%d @@", &aStart, &bStart)
			}
			lineA = aStart
			lineB = bStart

			rows = append(rows, AlignedRow{
				IsHeader:   true,
				HeaderText: line,
			})
			continue
		}

		if strings.HasPrefix(line, "-") {
			pendingDeletes = append(pendingDeletes, &DiffLine{
				Op:      diff.OpDelete,
				NumA:    lineA,
				Content: line[1:],
			})
			lineA++
			continue
		}

		if strings.HasPrefix(line, "+") {
			pendingInserts = append(pendingInserts, &DiffLine{
				Op:      diff.OpInsert,
				NumB:    lineB,
				Content: line[1:],
			})
			lineB++
			continue
		}

		if strings.HasPrefix(line, " ") {
			flushPending()
			rows = append(rows, AlignedRow{
				Left: &DiffLine{
					Op:      diff.OpEqual,
					NumA:    lineA,
					Content: line[1:],
				},
				Right: &DiffLine{
					Op:      diff.OpEqual,
					NumB:    lineB,
					Content: line[1:],
				},
			})
			lineA++
			lineB++
			continue
		}

		flushPending()
		rows = append(rows, AlignedRow{
			IsHeader:   true,
			HeaderText: line,
		})
	}
	flushPending()

	return rows
}
