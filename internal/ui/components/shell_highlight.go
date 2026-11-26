package components

import (
	"image/color"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
	"mvdan.cc/sh/v3/syntax"
)

func HighlightShellSpans(cmd string, scheme *theme.Scheme, base color.Color) []trail.Node {
	span := func(text string, kind chroma.TokenType) trail.Node {
		return trail.Span(trail.SpanProps{Style: resolveTokenStyle(scheme, kind, base)}, trail.Text(text))
	}
	parser := syntax.NewParser(syntax.KeepComments(true), syntax.Variant(syntax.LangBash))
	file, err := parser.Parse(strings.NewReader(cmd), "")
	var spans []trail.Node
	if err != nil {
		iter, err := chroma.Coalesce(lexers.Get("bash")).Tokenise(&chroma.TokeniseOptions{State: "root"}, cmd)
		if err != nil {
			return []trail.Node{span(cmd, chroma.Text)}
		}
		for tok := iter(); tok != chroma.EOF; tok = iter() {
			spans = append(spans, span(tok.Value, tok.Type))
		}
		return spans
	}
	h := shellHighlight{cmd, make([]chroma.TokenType, len(cmd))}
	syntax.Walk(file, h.visit)
	for i := 0; i < len(cmd); {
		j := i + 1
		for j < len(cmd) && h.kinds[j] == h.kinds[i] {
			j++
		}
		spans = append(spans, span(cmd[i:j], h.kinds[i]))
		i = j
	}
	return spans
}

type shellHighlight struct {
	cmd   string
	kinds []chroma.TokenType
}

func (h shellHighlight) paint(start, end int, kind chroma.TokenType) {
	for i := start; i < min(end, len(h.kinds)); i++ {
		h.kinds[i] = kind
	}
}
func (h shellHighlight) node(n syntax.Node, k chroma.TokenType) {
	h.paint(int(n.Pos().Offset()), int(n.End().Offset()), k)
}
func (h shellHighlight) token(p syntax.Pos, k chroma.TokenType, choices ...string) {
	if !p.IsValid() {
		return
	}
	i := int(p.Offset())
	for _, s := range choices {
		if strings.HasPrefix(h.cmd[i:], s) {
			h.paint(i, i+len(s), k)
			return
		}
	}
}
func (h shellHighlight) visit(n syntax.Node) bool {
	const op = chroma.Operator
	const kw = chroma.KeywordReserved
	switch v := n.(type) {
	case *syntax.CallExpr:
		h.call(v)
	case *syntax.Assign:
		if v.Name != nil {
			h.node(v.Name, chroma.NameVariable)
		}
	case *syntax.ParamExp:
		h.node(v, chroma.NameVariable)
	case *syntax.SglQuoted, *syntax.DblQuoted:
		h.node(n, chroma.LiteralString)
	case *syntax.Comment:
		h.node(v, chroma.Comment)
	case *syntax.Stmt:
		if v.Negated {
			h.token(v.Position, kw, "!")
		}
		h.token(v.Semicolon, op, ";", "&")
	case *syntax.BinaryCmd:
		h.token(v.OpPos, op, v.Op.String())
	case *syntax.Redirect:
		h.token(v.OpPos, op, v.Op.String())
		if v.N != nil {
			h.node(v.N, op)
		}
		if v.Op == syntax.DplOut || v.Op == syntax.DplIn {
			h.node(v.Word, op)
		}
		if v.Hdoc != nil {
			h.node(v.Hdoc, chroma.LiteralString)
		}
	case *syntax.CmdSubst:
		h.node(v, chroma.Text)
		h.token(v.Left, op, "$(", "`", "${")
		h.token(v.Right, op, ")", "`", "}")
	case *syntax.Subshell:
		h.token(v.Lparen, op, "(")
		h.token(v.Rparen, op, ")")
	case *syntax.IfClause:
		h.token(v.Position, kw, "if", "elif", "else")
		h.token(v.ThenPos, kw, "then")
		h.token(v.FiPos, kw, "fi")
	case *syntax.ForClause:
		h.token(v.ForPos, kw, "for", "select")
		h.token(v.DoPos, kw, "do")
		h.token(v.DonePos, kw, "done")
	case *syntax.WordIter:
		h.token(v.InPos, kw, "in")
	case *syntax.WhileClause:
		h.token(v.WhilePos, kw, "while", "until")
		h.token(v.DoPos, kw, "do")
		h.token(v.DonePos, kw, "done")
	case *syntax.CaseClause:
		h.token(v.Case, kw, "case")
		h.token(v.In, kw, "in")
		h.token(v.Esac, kw, "esac")
	case *syntax.CaseItem:
		h.token(v.OpPos, op, v.Op.String())
	case *syntax.FuncDecl:
		if v.RsrvWord {
			h.token(v.Position, kw, "function")
		}
	case *syntax.TimeClause:
		h.token(v.Time, kw, "time")
	case *syntax.TestClause:
		h.token(v.Left, kw, "[[")
		h.token(v.Right, kw, "]]")
	}
	return true
}

func (h shellHighlight) call(call *syntax.CallExpr) {
	command, wrapper, optionArg := true, "", false
	for _, arg := range call.Args {
		word := arg.Lit()
		first, _ := arg.Parts[0].(*syntax.Lit)
		flag := first != nil && strings.HasPrefix(first.Value, "-")
		switch {
		case optionArg:
			optionArg = false
		case flag && (!command || wrapper != ""):
			h.node(arg, chroma.KeywordType)
			if command {
				switch wrapper + ":" + word {
				case "sudo:-u", "sudo:--user", "sudo:-g", "sudo:--group", "env:-u", "env:--unset", "env:-C", "env:--chdir":
					optionArg = true
				}
			}
		case command:
			if wrapper != "" && first != nil {
				if name, _, ok := strings.Cut(first.Value, "="); ok && syntax.ValidName(name) {
					i := int(first.Pos().Offset())
					h.paint(i, i+len(name), chroma.NameVariable)
					continue
				}
			}
			h.node(arg, chroma.NameFunction)
			wrapper = word
			command = word == "sudo" || word == "env"
		}
	}
}
