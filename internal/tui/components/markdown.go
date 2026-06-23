package components

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// MarkdownProps defines properties for the Markdown rendering component.
type MarkdownProps struct {
	Source string
	Style  style.Style
}

// Markdown renders markdown content into Kite UI nodes using goldmark.
var Markdown = kitex.FC("Markdown", func(props MarkdownProps) kitex.Node {
	t := theme.UseTheme() // *theme.Scheme

	// Only re-parse when Source changes.
	node := kitex.UseMemo(func() kitex.Node {
		if strings.TrimSpace(props.Source) == "" {
			return nil
		}

		src := []byte(props.Source)
		md := goldmark.New(goldmark.WithExtensions(extension.Table))
		doc := md.Parser().Parse(text.NewReader(src))

		// renderInline renders inline nodes (Text, Span) inside a block.
		var renderInline func(n ast.Node) kitex.Node
		// renderBlock renders block-level nodes (Box).
		var renderBlock func(n ast.Node) kitex.Node
		// renderBlockChildren collects block children of a parent.
		var renderBlockChildren func(parent ast.Node) []kitex.Node
		// renderInlineChildren collects inline children into nodes, coalescing
		// consecutive *ast.Text nodes into a single kitex.Text so that embedded
		// \n characters produce visual line breaks (a single text node with \n
		// works; separate nodes with \n at the boundary do not).
		var renderInlineChildren = func(parent ast.Node) []kitex.Node {
			var out []kitex.Node
			var textBuf strings.Builder

			flushText := func() {
				if s := textBuf.String(); s != "" {
					out = append(out, kitex.Text(s))
					textBuf.Reset()
				}
			}

			for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
				if tn, ok := child.(*ast.Text); ok {
					textBuf.WriteString(string(tn.Value(src)))
					if tn.SoftLineBreak() || tn.HardLineBreak() {
						textBuf.WriteByte('\n')
					}
				} else {
					flushText()
					if n := renderInline(child); n != nil {
						out = append(out, n)
					}
				}
			}
			flushText()
			return out
		}

		renderBlockChildren = func(parent ast.Node) []kitex.Node {
			var out []kitex.Node
			for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
				if n := renderBlock(child); n != nil {
					out = append(out, n)
				}
			}
			return out
		}

		renderInline = func(n ast.Node) kitex.Node {
			switch node := n.(type) {
			case *ast.Text:
				// Fallback: normally text is coalesced in renderInlineChildren.
				val := string(node.Value(src))
				if val == "" {
					return nil
				}
				if node.SoftLineBreak() || node.HardLineBreak() {
					return kitex.Text(val + "\n")
				}
				return kitex.Text(val)

			case *ast.CodeSpan:
				var b strings.Builder
				for child := node.FirstChild(); child != nil; child = child.NextSibling() {
					if tn, ok := child.(*ast.Text); ok {
						b.Write(tn.Value(src))
					}
				}
				spanText := b.String()
				if spanText == "" {
					spanText = string(node.Text(src))
				}
				spanStyle := style.S().
					Display(style.DisplayInlineBlock).
					WhiteSpace(style.WhiteSpaceNoWrap)
				if t != nil {
					spanStyle = spanStyle.
						Background(t.Color.Surface.BaseFocus).
						Foreground(t.Color.Surface.Primary)
				}
				return kitex.Span(kitex.SpanProps{Style: spanStyle},
					kitex.Text(spanText),
				)

			case *ast.Emphasis:
				children := renderInlineChildren(node)
				if len(children) == 0 {
					return nil
				}
				empStyle := style.S()
				if node.Level == 1 {
					empStyle = empStyle.Italic(true)
				} else {
					empStyle = empStyle.Bold(true)
				}
				return kitex.Span(kitex.SpanProps{Style: empStyle}, children...)

			case *ast.Link:
				children := renderInlineChildren(node)
				if len(children) == 0 {
					return nil
				}
				linkStyle := style.S().Underline(true)
				if t != nil {
					linkStyle = linkStyle.Foreground(t.Color.Surface.Primary)
				}
				return kitex.Span(kitex.SpanProps{Style: linkStyle}, children...)

			default:
				// Fallback: try inline children, otherwise plain text.
				children := renderInlineChildren(node)
				if len(children) > 0 {
					return kitex.Fragment(children...)
				}
				if txt := strings.TrimSpace(string(n.Text(src))); txt != "" {
					return kitex.Text(txt)
				}
				return nil
			}
		}

		renderBlock = func(n ast.Node) kitex.Node {
			switch node := n.(type) {
			case *ast.Document:
				docStyle := style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					Merge(props.Style)
				if t != nil {
					docStyle = docStyle.Foreground(t.Color.Text.Secondary)
				}
				return kitex.Box(kitex.BoxProps{Style: docStyle},
					renderBlockChildren(node)...)

			case *ast.Paragraph:
				children := renderInlineChildren(node)
				if len(children) == 0 {
					return nil
				}
				pStyle := style.S()
				// Paragraphs inside a ListItem have no extra margin — the
				// list's own spacing is sufficient.
				_, inListItem := node.Parent().(*ast.ListItem)
				_, inBlockquote := node.Parent().(*ast.Blockquote)
				if inListItem {
					// If it is the first block child of the list item, we render it inline
					// so it doesn't cause a line break next to the bullet/marker.
					if node.Parent().FirstChild() == node {
						return kitex.Fragment(children...)
					}
					// Subsequent block children of the list item should stack as block boxes
					pStyle = pStyle.MarginTop(1)
				} else if inBlockquote {
					if node.NextSibling() != nil {
						pStyle = pStyle.MarginBottom(1)
					}
				} else {
					if node.NextSibling() != nil {
						pStyle = pStyle.MarginBottom(1)
					}
				}
				return kitex.Box(kitex.BoxProps{Style: pStyle}, children...)

			case *ast.TextBlock:
				children := renderInlineChildren(node)
				if len(children) == 0 {
					return nil
				}
				_, inListItem := node.Parent().(*ast.ListItem)
				if inListItem {
					if node.Parent().FirstChild() == node {
						return kitex.Fragment(children...)
					}
				}
				return kitex.Box(kitex.BoxProps{}, children...)

			case *ast.Heading:
				children := renderInlineChildren(node)
				if len(children) == 0 {
					return nil
				}
				marginBottom := 1
				if node.Level == 1 {
					marginBottom = 2
				}
				var hStyle style.Style
				if node.NextSibling() != nil {
					hStyle = style.S().MarginBottom(marginBottom)
				} else {
					hStyle = style.S()
				}
				spanStyle := style.S().Bold(true)
				if t != nil {
					spanStyle = spanStyle.Foreground(t.Color.Text.Primary)
				}
				return kitex.Box(kitex.BoxProps{Style: hStyle},
					kitex.Span(kitex.SpanProps{Style: spanStyle}, children...),
				)

			case *ast.CodeBlock:
				var b strings.Builder
				for i := 0; i < node.Lines().Len(); i++ {
					seg := node.Lines().At(i)
					b.Write(seg.Value(src))
				}
				marginBottom := 1
				if node.NextSibling() == nil {
					marginBottom = 0
				}
				return CodeBlock(CodeBlockProps{
					Code:  strings.TrimRight(b.String(), "\n"),
					Lang:  "code",
					Style: style.S().MarginBottom(marginBottom),
				})

			case *ast.FencedCodeBlock:
				var b strings.Builder
				for i := 0; i < node.Lines().Len(); i++ {
					seg := node.Lines().At(i)
					b.Write(seg.Value(src))
				}
				lang := "code"
				if node.Info != nil {
					if parts := strings.Fields(string(node.Info.Value(src))); len(parts) > 0 {
						lang = parts[0]
					}
				}
				marginBottom := 1
				if node.NextSibling() == nil {
					marginBottom = 0
				}
				return CodeBlock(CodeBlockProps{
					Code:  strings.TrimRight(b.String(), "\n"),
					Lang:  lang,
					Style: style.S().MarginBottom(marginBottom),
				})

			case *ast.List:
				return renderList(node, t, renderBlock)

			case *ast.Blockquote:
				children := renderBlockChildren(node)
				if len(children) == 0 {
					return nil
				}
				qStyle := style.S().PaddingLeft(1)
				if t != nil {
					qStyle = qStyle.BorderLeft(true, style.SingleBorder(), t.Color.Border.Primary)
				}
				innerBox := kitex.Box(kitex.BoxProps{Style: qStyle}, children...)
				marginBottom := 1
				if node.NextSibling() == nil {
					marginBottom = 0
				}
				return kitex.Box(kitex.BoxProps{Style: style.S().MarginBottom(marginBottom)}, innerBox)

			case *ast.ThematicBreak:
				marginBottom := 1
				if node.NextSibling() == nil {
					marginBottom = 0
				}
				tbStyle := style.S().MarginBottom(marginBottom)
				if t != nil {
					tbStyle = tbStyle.Foreground(t.Color.Border.Primary).
						BorderBottom(true, style.SingleBorder(), t.Color.Border.Primary)
				}
				return kitex.Box(kitex.BoxProps{Style: tbStyle}) // kitex.Text(strings.Repeat("─", 40)),

			case *extast.Table:
				return renderTable(node, t, renderInlineChildren)

			default:
				// Try block children first; if none, fall back to inline
				// children. This handles unknown nodes that wrap either
				// block or inline content (e.g. *ast.TextBlock in some
				// goldmark extensions).
				blockChildren := renderBlockChildren(node)
				if len(blockChildren) > 0 {
					return kitex.Fragment(blockChildren...)
				}
				inlineChildren := renderInlineChildren(node)
				if len(inlineChildren) > 0 {
					return kitex.Box(kitex.BoxProps{}, inlineChildren...)
				}
				return nil
			}
		}

		return renderBlock(doc)
	}, []any{props.Source})

	return node
})

func renderList(
	list *ast.List,
	t *theme.Scheme,
	renderBlock func(ast.Node) kitex.Node,
) kitex.Node {
	var items []kitex.Node
	idx := list.Start
	if idx <= 0 {
		idx = 1
	}

	for child := list.FirstChild(); child != nil; child = child.NextSibling() {
		li, ok := child.(*ast.ListItem)
		if !ok {
			continue
		}

		var bullet string
		if list.IsOrdered() {
			bullet = fmt.Sprintf("%d. ", idx)
			idx++
		} else {
			bullet = "• "
		}

		// Use renderBlock for every list-item child so that multi-paragraph
		// (loose) list items preserve the break between their paragraphs.
		// Paragraphs rendered inside a ListItem parent already suppress their
		// MarginBottom, so stacking is tight without extra gaps.
		var content []kitex.Node
		for liChild := li.FirstChild(); liChild != nil; liChild = liChild.NextSibling() {
			if n := renderBlock(liChild); n != nil {
				content = append(content, n)
			}
		}
		if len(content) == 0 {
			continue
		}

		bulletStyle := style.S().WhiteSpace(style.WhiteSpaceNoWrap)
		if t != nil {
			bulletStyle = bulletStyle.Foreground(t.Color.Surface.Primary)
		}

		items = append(items, kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow),
		},
			kitex.Box(kitex.BoxProps{Style: bulletStyle}, kitex.Text(bullet)),
			kitex.Box(kitex.BoxProps{Style: style.S().Flex(1)}, content...),
		))
	}

	if len(items) == 0 {
		return nil
	}

	marginBottom := 1
	if list.NextSibling() == nil {
		marginBottom = 0
	}
	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			MarginBottom(marginBottom).
			PaddingLeft(2),
	}, items...)
}

func renderTable(
	table *extast.Table,
	t *theme.Scheme,
	renderInlineChildren func(ast.Node) []kitex.Node,
) kitex.Node {
	var headRows []kitex.Node
	var bodyRows []kitex.Node
	rowIdx := 0

	for child := table.FirstChild(); child != nil; child = child.NextSibling() {
		switch node := child.(type) {
		case *extast.TableHeader:
			var cells []kitex.Node
			for cellChild := node.FirstChild(); cellChild != nil; cellChild = cellChild.NextSibling() {
				if cell, ok := cellChild.(*extast.TableCell); ok {
					tdProps := kitex.TDProps{
						Style: getHeaderCellStyle(cell.Alignment),
					}
					inlineNodes := renderInlineChildren(cell)
					cells = append(cells, kitex.TD(tdProps, inlineNodes...))
				}
			}
			if len(cells) > 0 {
				trStyle := style.S()
				if t != nil {
					trStyle = trStyle.Background(t.Color.Surface.BaseDisabled)
				}
				headRows = append(headRows, kitex.TR(kitex.TRProps{Style: trStyle}, cells...))
			}

		case *extast.TableRow:
			var cells []kitex.Node
			for cellChild := node.FirstChild(); cellChild != nil; cellChild = cellChild.NextSibling() {
				if cell, ok := cellChild.(*extast.TableCell); ok {
					tdProps := kitex.TDProps{
						Style: getBodyCellStyle(cell.Alignment),
					}
					inlineNodes := renderInlineChildren(cell)
					cells = append(cells, kitex.TD(tdProps, inlineNodes...))
				}
			}
			if len(cells) > 0 {
				trStyle := style.S()
				if t != nil && rowIdx%2 == 1 {
					trStyle = trStyle.Background(t.Color.Surface.BaseHover)
				}
				bodyRows = append(bodyRows, kitex.TR(kitex.TRProps{Style: trStyle}, cells...))
				rowIdx++
			}
		}
	}

	var tableChildren []kitex.Node
	if len(headRows) > 0 {
		tableChildren = append(tableChildren, kitex.THead(kitex.THeadProps{}, headRows...))
	}
	if len(bodyRows) > 0 {
		tableChildren = append(tableChildren, kitex.TBody(kitex.TBodyProps{}, bodyRows...))
	}

	if len(tableChildren) == 0 {
		return nil
	}

	marginBottom := 1
	if table.NextSibling() == nil {
		marginBottom = 0
	}
	tableStyle := style.S().MarginBottom(marginBottom)
	return kitex.Table(kitex.TableProps{Style: tableStyle}, tableChildren...)
}

func getHeaderCellStyle(align extast.Alignment) style.Style {
	s := style.S().Bold(true).PaddingHorizontal(1)
	switch align {
	case extast.AlignLeft:
		s = s.TextAlign(style.TextAlignLeft)
	case extast.AlignCenter:
		s = s.TextAlign(style.TextAlignCenter)
	case extast.AlignRight:
		s = s.TextAlign(style.TextAlignRight)
	}
	return s
}

func getBodyCellStyle(align extast.Alignment) style.Style {
	s := style.S().PaddingHorizontal(1)
	switch align {
	case extast.AlignLeft:
		s = s.TextAlign(style.TextAlignLeft)
	case extast.AlignCenter:
		s = s.TextAlign(style.TextAlignCenter)
	case extast.AlignRight:
		s = s.TextAlign(style.TextAlignRight)
	}
	return s
}
