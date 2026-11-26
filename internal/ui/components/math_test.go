package components

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"slices"
	"strings"
	"testing"
)

func TestMathParser(t *testing.T) {
	md := goldmark.New(goldmark.WithParserOptions(parser.WithInlineParsers(util.Prioritized(mathInlineParser{}, 150)), parser.WithBlockParsers(util.Prioritized(mathBlockParser{}, 850))))
	for _, tc := range []struct{ source, want string }{
		{`(action) $\rightarrow$ output`, `inline:\rightarrow`},
		{`\(\to\)`, `inline:\to`},
		{`prefix $$x_2$$ suffix`, `inline:x_2`},
		{`\\$x$`, `inline:x`},
		{`$x\$y$`, `inline:x\$y`},
		{`$$x$$`, "block:x"},
		{"$$\na\nb\n$$\nnext", "block:a\nb"},
		{"$$\nx^2$$\n\n## Explanation", "block:x^2;heading"},
		{"$$x^2\n+ y^2$$\n\n## Explanation", "block:x^2\n+ y^2;heading"},
		{"\\[\nx\n\\]", "block:x"},
		{"\\[\nx\\]\n\n## After", "block:x;heading"},
		{"$$\nunclosed", "block:unclosed"},
		{`Costs $0.01 to $0.05`, ""},
		{"Pay $5; use `$` for currency.", "code:$"},
		{"Pay $5; use `` $ `` for currency.", "code:$"},
		{`$5+$10`, ""},
		{`$5 and $10`, ""},
		{`Reads $HOME and $XDG_CONFIG_HOME`, ""},
		{`\$escaped$`, ""},
		{`$ x$`, ""},
		{`$x $`, ""},
		{`$x$_y`, ""},
		{`$x$abc`, ""},
		{`5$x$`, ""},
		{"$x\ny$", ""},
		{"`$\\rightarrow$`", "code:$\\rightarrow$"},
		{"```\n$$x$$\n```", ""},
		{`empty $$$$ here`, ""},
	} {
		t.Run(tc.source, func(t *testing.T) {
			doc := md.Parser().Parse(text.NewReader([]byte(tc.source)))
			var found []string
			err := ast.Walk(doc, func(n ast.Node, enter bool) (ast.WalkStatus, error) {
				if enter {
					switch v := n.(type) {
					case *MathInlineNode:
						found = append(found, "inline:"+v.Raw)
					case *MathBlockNode:
						found = append(found, "block:"+v.Raw)
					case *ast.Heading:
						found = append(found, "heading")
					case *ast.CodeSpan:
						var code strings.Builder
						for child := v.FirstChild(); child != nil; child = child.NextSibling() {
							if txt, ok := child.(*ast.Text); ok {
								code.Write(txt.Value([]byte(tc.source)))
							}
						}
						found = append(found, "code:"+code.String())
					}
				}
				return ast.WalkContinue, nil
			})
			if err != nil || strings.Join(found, ";") != tc.want {
				t.Fatalf("got %q, %v; want %q", found, err, tc.want)
			}
		})
	}
}

func TestMathParserLinearOnManyUnmatchedOpeners(t *testing.T) {
	md := goldmark.New(goldmark.WithParserOptions(parser.WithInlineParsers(util.Prioritized(mathInlineParser{}, 150))))
	line := strings.Repeat(`\( `, 20000)
	doc := md.Parser().Parse(text.NewReader([]byte(line)))
	var mathNodes int
	_ = ast.Walk(doc, func(n ast.Node, enter bool) (ast.WalkStatus, error) {
		if enter {
			if _, ok := n.(*MathInlineNode); ok {
				mathNodes++
			}
		}
		return ast.WalkContinue, nil
	})
	if mathNodes != 0 {
		t.Fatalf("expected 0 math nodes for unmatched openers, got %d", mathNodes)
	}
}

func TestTranslateMath(t *testing.T) {
	for source, want := range map[string]string{
		`x_i x_{10} 2^n 10^{-3}`: "xᵢ x₁₀ 2ⁿ 10⁻³",
		`x_Q x^{Q+1}`:            "x_Q x^(Q+1)",
		`\sqrt{x} \sqrt[n]{x+1} \frac{a}{b} \frac{x+y}{x-y}`: "√x ⁿ√(x+1) a/b (x+y)/(x-y)",
		`\frac{a} {b} \frac {x+y} {z} \sqrt {x} \text {hi}`:  "a/b (x+y)/z √x hi",
		`\alpha\omega\Gamma\Omega\varepsilon\varphi`:         "αωΓΩϵϕ",
		`\forall x\in S\implies x\notin\emptyset`:            "∀ x∈ S⇒ x∉∅",
		`\text{hé{ll}o 世界} \operatorname{rank} \det`:         "héllo 世界 rank det",
		`a\,b\!c\qquad\%\_\{\}\$\&\#`:                        "a bc  %_{}$&#",
		`\unknown \frac{a} \text{未`:                          `\unknown \fraca \text{未`,
	} {
		var got strings.Builder
		for _, seg := range translateMath(source) {
			got.WriteString(seg.text)
		}
		if got.String() != want {
			t.Errorf("%q translated to %q, want %q", source, got.String(), want)
		}
	}
	if got := translateMath(`a\mathbf{b\mathtt{c}}d`); !slices.Equal(got, []mathSegment{{"a", 0}, {"b", mathBold}, {"c", mathBold | mathMono}, {"d", 0}}) {
		t.Errorf("nested styles: %+v", got)
	}
}
