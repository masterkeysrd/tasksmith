package components

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/core/diff"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// DiffProps defines the properties for rendering a unified diff.
type DiffProps struct {
	Text  string
	Path  string
	Lang  string
	Style style.Style
}

// AlignedRow represents a single line in a unified diff layout.
type AlignedRow struct {
	IsHeader   bool
	HeaderText string
	Op         diff.Op
	NumA       int
	NumB       int
	Text       string
}

func parseHunkHeader(line string) (aStart, bStart int, ok bool) {
	parts := strings.Split(line, "@@")
	if len(parts) < 3 {
		return 0, 0, false
	}
	spec := strings.TrimSpace(parts[1])
	fields := strings.Fields(spec)
	if len(fields) < 2 {
		return 0, 0, false
	}
	aStr := strings.TrimPrefix(fields[0], "-")
	bStr := strings.TrimPrefix(fields[1], "+")
	if idx := strings.IndexByte(aStr, ','); idx != -1 {
		aStr = aStr[:idx]
	}
	if idx := strings.IndexByte(bStr, ','); idx != -1 {
		bStr = bStr[:idx]
	}
	var errA, errB error
	aStart, errA = strconv.Atoi(aStr)
	bStart, errB = strconv.Atoi(bStr)
	if errA != nil || errB != nil {
		return 0, 0, false
	}
	return aStart, bStart, true
}

func parseUnifiedDiff(diffStr string) ([]AlignedRow, string) {
	lines := strings.Split(diffStr, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var rows []AlignedRow
	var extractedPath string
	lineA := 0
	lineB := 0

	for _, line := range lines {
		if strings.HasPrefix(line, "--- ") {
			continue
		}
		if after, ok := strings.CutPrefix(line, "+++ "); ok {
			if extractedPath == "" {
				p := strings.TrimSpace(after)
				p = strings.TrimPrefix(p, "b/")
				extractedPath = p
			}
			continue
		}
		if strings.HasPrefix(line, "@@") {
			aStart, bStart, ok := parseHunkHeader(line)
			if ok {
				lineA = aStart
				lineB = bStart
			}
			rows = append(rows, AlignedRow{
				IsHeader:   true,
				HeaderText: line,
			})
			continue
		}
		if strings.HasPrefix(line, "-") {
			rows = append(rows, AlignedRow{
				Op:   diff.OpDelete,
				NumA: lineA,
				Text: line[1:],
			})
			lineA++
			continue
		}
		if strings.HasPrefix(line, "+") {
			rows = append(rows, AlignedRow{
				Op:   diff.OpInsert,
				NumB: lineB,
				Text: line[1:],
			})
			lineB++
			continue
		}
		if strings.HasPrefix(line, " ") {
			rows = append(rows, AlignedRow{
				Op:   diff.OpEqual,
				NumA: lineA,
				NumB: lineB,
				Text: line[1:],
			})
			lineA++
			lineB++
			continue
		}
		if strings.HasPrefix(line, `\`) {
			continue
		}
		rows = append(rows, AlignedRow{
			Op:   diff.OpEqual,
			NumA: lineA,
			NumB: lineB,
			Text: line,
		})
		lineA++
		lineB++
	}
	return rows, extractedPath
}

// DiffView is a trail functional component rendering unified diff lines with syntax highlighting
// and theme-aware colors in a 3-column layout (gutter, separator, code).
var DiffView = trail.FC("DiffView", func(props DiffProps) trail.Node {
	diffText := strings.TrimSpace(props.Text)
	if diffText == "" {
		return nil
	}

	rows, extractedPath := parseUnifiedDiff(props.Text)
	if len(rows) == 0 {
		return nil
	}

	targetPath := props.Path
	if targetPath == "" {
		targetPath = extractedPath
	}

	var lexer chroma.Lexer
	if props.Lang != "" {
		lexer = lexers.Get(props.Lang)
	}
	if lexer == nil && targetPath != "" {
		lexer = lexers.Match(targetPath)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	scheme := theme.UseTheme()

	redBg := scheme.Color.Diff.RemovedBg
	greenBg := scheme.Color.Diff.AddedBg
	cyanBg := scheme.Color.Diff.HunkBg

	var gutterSpans []trail.Node
	var sepSpans []trail.Node
	var codeSpans []trail.Node

	for _, row := range rows {
		var bg color.Color
		if row.IsHeader {
			bg = cyanBg
		} else if row.Op == diff.OpDelete {
			bg = redBg
		} else if row.Op == diff.OpInsert {
			bg = greenBg
		}

		var gutterText string
		if row.IsHeader {
			gutterText = fmt.Sprintf("%12s\n", "")
		} else {
			numAStr := ""
			if row.NumA > 0 {
				numAStr = strconv.Itoa(row.NumA)
			}
			numBStr := ""
			if row.NumB > 0 {
				numBStr = strconv.Itoa(row.NumB)
			}
			sign := " "
			switch row.Op {
			case diff.OpDelete:
				sign = "-"
			case diff.OpInsert:
				sign = "+"
			}
			gutterText = fmt.Sprintf("%5s%5s %s\n", numAStr, numBStr, sign)
		}

		gutterStyle := style.S().
			WhiteSpace(style.WhiteSpacePre).
			Foreground(scheme.Color.Text.Muted)
		if bg != nil {
			gutterStyle = gutterStyle.Background(bg)
		}
		gutterSpans = append(gutterSpans, trail.Span(trail.SpanProps{
			Style: gutterStyle,
		}, trail.Text(gutterText)))

		sepStyle := style.S().
			WhiteSpace(style.WhiteSpacePre).
			Foreground(scheme.Color.Border.Default)
		if bg != nil {
			sepStyle = sepStyle.Background(bg)
		}
		sepSpans = append(sepSpans, trail.Span(trail.SpanProps{
			Style: sepStyle,
		}, trail.Text("│\n")))

		if row.IsHeader {
			headerStyle := style.S().
				Bold(true).
				WhiteSpace(style.WhiteSpacePre).
				Foreground(scheme.Color.Diff.HunkText)
			if bg != nil {
				headerStyle = headerStyle.Background(bg)
			}
			codeSpans = append(codeSpans, trail.Span(trail.SpanProps{
				Style: headerStyle,
			}, trail.Text(row.HeaderText)))
			if bg != nil {
				if paddingLength := 500 - len(row.HeaderText); paddingLength > 0 {
					codeSpans = append(codeSpans, trail.Span(trail.SpanProps{
						Style: style.S().
							Background(bg).
							WhiteSpace(style.WhiteSpacePre),
					},
						trail.Text(strings.Repeat(" ", paddingLength)),
					))
				}
			}
			nlStyle := style.S().WhiteSpace(style.WhiteSpacePre)
			if bg != nil {
				nlStyle = nlStyle.Background(bg)
			}
			codeSpans = append(codeSpans, trail.Span(trail.SpanProps{
				Style: nlStyle,
			}, trail.Text("\n")))
		} else {
			codeLine := strings.ReplaceAll(row.Text, "\t", "    ")
			iterator, err := lexer.Tokenise(nil, codeLine)
			if err != nil {
				fallbackStyle := style.S().
					WhiteSpace(style.WhiteSpacePre).
					Foreground(scheme.Color.Text.Primary)
				if bg != nil {
					fallbackStyle = fallbackStyle.Background(bg)
				}
				codeSpans = append(codeSpans, trail.Span(trail.SpanProps{
					Style: fallbackStyle,
				}, trail.Text(codeLine)))
			} else {
				for tok := iterator(); tok != chroma.EOF; tok = iterator() {
					tokStyle := uicomponents.ResolveTokenStyle(scheme, tok.Type)
					if bg != nil {
						tokStyle = tokStyle.Background(bg)
					}
					codeSpans = append(codeSpans, trail.Span(trail.SpanProps{
						Style: tokStyle,
					}, trail.Text(tok.Value)))
				}
			}
			if bg != nil {
				if paddingLength := 500 - len(codeLine); paddingLength > 0 {
					codeSpans = append(codeSpans, trail.Span(trail.SpanProps{
						Style: style.S().
							Background(bg).
							WhiteSpace(style.WhiteSpacePre),
					},
						trail.Text(strings.Repeat(" ", paddingLength)),
					))
				}
			}
			nlStyle := style.S().WhiteSpace(style.WhiteSpacePre)
			if bg != nil {
				nlStyle = nlStyle.Background(bg)
			}
			codeSpans = append(codeSpans, trail.Span(trail.SpanProps{
				Style: nlStyle,
			}, trail.Text("\n")))
		}
	}

	gutterBox := trail.Box(trail.BoxProps{
		Style: style.S().
			Width(style.Cells(12)).
			MinWidth(style.Cells(12)).
			MaxWidth(style.Cells(12)).
			Flex(0, 0),
	}, gutterSpans...)

	sepBox := trail.Box(trail.BoxProps{
		Style: style.S().
			Width(style.Cells(1)).
			MinWidth(style.Cells(1)).
			MaxWidth(style.Cells(1)).
			Flex(0, 0),
	}, sepSpans...)

	codeBox := trail.Box(trail.BoxProps{
		Style: style.S().
			Flex(1, 1).
			MinWidth(style.Cells(0)).
			Overflow(style.OverflowHidden),
	}, codeSpans...)

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Width(style.Percent(100)).
			MinWidth(style.Cells(0)).
			Merge(props.Style),
	}, gutterBox, sepBox, codeBox)
})
