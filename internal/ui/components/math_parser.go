package components

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

var (
	mathInlineKind = ast.NewNodeKind("MathInline")
	mathBlockKind  = ast.NewNodeKind("MathBlock")
	mathCacheKey   = parser.NewContextKey()
)

type MathInlineNode struct {
	ast.BaseInline
	Raw string
}

func (*MathInlineNode) Kind() ast.NodeKind { return mathInlineKind }
func (n *MathInlineNode) Dump(src []byte, level int) {
	ast.DumpHelper(n, src, level, map[string]string{"Raw": n.Raw}, nil)
}

type MathBlockNode struct {
	ast.BaseBlock
	Raw       string
	closeWith string
	parts     []string
}

func (*MathBlockNode) Kind() ast.NodeKind { return mathBlockKind }
func (n *MathBlockNode) IsRaw() bool      { return true }
func (n *MathBlockNode) Dump(src []byte, level int) {
	ast.DumpHelper(n, src, level, map[string]string{"Raw": n.Raw}, nil)
}

type mathInlineCache struct {
	lineNum int
	// noClose records the earliest column offset on lineNum where scanning for
	// each delimiter (0="$", 1="$$", 2="\\)") already failed to find a closer,
	// preventing quadratic rescans on lines with many unmatched openers.
	noClose [3]int
}

func mathDelimiterSlot(delimiter string) int {
	switch delimiter {
	case "$":
		return 0
	case "$$":
		return 1
	default:
		return 2
	}
}

func mathCacheForLine(pc parser.Context, lineNum int) *mathInlineCache {
	if pc == nil {
		return nil
	}
	c, _ := pc.Get(mathCacheKey).(*mathInlineCache)
	if c == nil {
		c = &mathInlineCache{lineNum: -1}
		pc.Set(mathCacheKey, c)
	}
	if c.lineNum != lineNum {
		c.lineNum = lineNum
		c.noClose = [3]int{-1, -1, -1}
	}
	return c
}

// skipCodeSpan checks whether line[i:] starts a complete CommonMark inline code
// span (matching backtick run length) and returns the index immediately after
// its closing backticks, or -1 if unclosed.
func skipCodeSpan(line []byte, i int) int {
	n := 0
	for i+n < len(line) && line[i+n] == '`' {
		n++
	}
	if n == 0 {
		return -1
	}
	for j := i + n; j < len(line); {
		if line[j] != '`' {
			j++
			continue
		}
		m := 0
		for j+m < len(line) && line[j+m] == '`' {
			m++
		}
		if m == n {
			return j + m
		}
		j += m
	}
	return -1
}

func mathClose(line []byte, delimiter string) int {
	skipTicks := delimiter == "$"
	for i := 0; i < len(line); i++ {
		if skipTicks && line[i] == '`' {
			if next := skipCodeSpan(line, i); next > i {
				i = next - 1
				continue
			}
		}
		if bytes.HasPrefix(line[i:], []byte(delimiter)) {
			return i
		}
		if line[i] == '\\' {
			i++
		}
	}
	return -1
}

type mathInlineParser struct{}

func (mathInlineParser) Trigger() []byte { return []byte{'$', '\\'} }
func (mathInlineParser) Parse(_ ast.Node, r text.Reader, pc parser.Context) ast.Node {
	line, seg := r.PeekLine()
	line = bytes.TrimRight(line, "\r\n")
	closeWith := "$"
	width := 1
	switch {
	case bytes.HasPrefix(line, []byte(`\(`)):
		closeWith = `\)`
		width = 2
	case line[0] == '$':
		prev := r.PrecendingCharacter()
		if prev == '$' || prev >= '0' && prev <= '9' {
			return nil
		}
		if bytes.HasPrefix(line, []byte("$$")) {
			width = 2
			closeWith = "$$"
		}
	default:
		return nil
	}
	lineNum, _ := r.Position()
	slot := mathDelimiterSlot(closeWith)
	cache := mathCacheForLine(pc, lineNum)
	if cache != nil && cache.noClose[slot] >= 0 && seg.Start >= cache.noClose[slot] {
		return nil
	}
	body := line[width:]
	first, _ := utf8.DecodeRune(body)
	if width == 1 && unicode.IsSpace(first) {
		return nil
	}
	end := mathClose(body, closeWith)
	if end < 0 {
		if cache != nil {
			cache.noClose[slot] = seg.Start
		}
		return nil
	}
	if end == 0 {
		return nil
	}
	after := body[end+width:]
	if width == 1 {
		last, _ := utf8.DecodeLastRune(body[:end])
		if unicode.IsSpace(last) || len(after) > 0 && (after[0] == '$' || after[0] == '_' || asciiLetter(after[0]) || after[0] >= '0' && after[0] <= '9') {
			return nil
		}
	}
	r.Advance(width + end + width)
	return &MathInlineNode{Raw: string(body[:end])}
}

type mathBlockParser struct{}

func (mathBlockParser) Trigger() []byte             { return []byte{'$', '\\'} }
func (mathBlockParser) CanInterruptParagraph() bool { return true }
func (mathBlockParser) CanAcceptIndentedLine() bool { return false }
func (mathBlockParser) Open(_ ast.Node, r text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, _ := r.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 {
		return nil, parser.NoChildren
	}
	trimmed := bytes.TrimSpace(line[pos:])
	closeWith := "$$"
	switch {
	case bytes.HasPrefix(trimmed, []byte("$$")):
	case bytes.HasPrefix(trimmed, []byte(`\[`)):
		closeWith = `\]`
	default:
		return nil, parser.NoChildren
	}
	body := trimmed[2:]
	if len(body) == 0 {
		r.AdvanceToEOL()
		return &MathBlockNode{closeWith: closeWith}, parser.NoChildren
	}
	end := mathClose(body, closeWith)
	if end >= 0 {
		if len(bytes.TrimSpace(body[end+2:])) != 0 || end == 0 {
			return nil, parser.NoChildren
		}
		r.AdvanceToEOL()
		return &MathBlockNode{Raw: string(bytes.TrimSpace(body[:end]))}, parser.NoChildren
	}
	r.AdvanceToEOL()
	return &MathBlockNode{closeWith: closeWith, parts: []string{string(body)}}, parser.NoChildren
}
func (mathBlockParser) Continue(n ast.Node, r text.Reader, _ parser.Context) parser.State {
	node := n.(*MathBlockNode)
	if node.closeWith == "" {
		return parser.Close
	}
	line, _ := r.PeekLine()
	r.AdvanceToEOL()
	clean := bytes.TrimRight(line, "\r\n")
	noTrail := bytes.TrimRight(clean, " \t")
	if end := mathClose(noTrail, node.closeWith); end >= 0 && end+len(node.closeWith) == len(noTrail) {
		if before := bytes.TrimRight(noTrail[:end], " \t"); len(before) > 0 {
			node.parts = append(node.parts, string(before))
		}
		node.closeWith = ""
		return parser.Close
	}
	node.parts = append(node.parts, string(clean))
	return parser.Continue | parser.NoChildren
}
func (mathBlockParser) Close(n ast.Node, _ text.Reader, _ parser.Context) {
	node := n.(*MathBlockNode)
	if len(node.parts) > 0 {
		node.Raw = strings.TrimRight(strings.Join(node.parts, "\n"), "\r\n")
	}
}
