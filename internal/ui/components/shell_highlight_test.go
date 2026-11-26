package components

import (
	"image/color"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func TestShellHighlight(t *testing.T) {
	scheme := theme.Get()
	s := scheme.Color.Syntax
	base := scheme.Color.Text.Secondary
	for _, tc := range []struct {
		cmd    string
		tokens map[string]color.Color
	}{
		{`A=b  sudo -u root env B=c rm -rf --force café | cat 2>&1 && (whoami); pwd >> out # end`, map[string]color.Color{"A": s.Variable, "sudo": s.Function, "root": base, "env": s.Function, "B": s.Variable, "rm": s.Function, "-rf": s.Type, "--force": s.Type, "café": base, "|": s.Operator, "cat": s.Function, "2>&1": s.Operator, "&&": s.Operator, "(": s.Operator, "whoami": s.Function, ")": s.Operator, ";": s.Operator, "pwd": s.Function, ">>": s.Operator, "# end": s.Comment}},
		{`echo "pre $VAR ${HOME} $(ls path) post" || printf $'ok'`, map[string]color.Color{"\"pre": s.String, "$VAR": s.Variable, "${HOME}": s.Variable, "$(": s.Operator, "ls": s.Function, "path": base, ")": s.Operator, "post\"": s.String, "||": s.Operator, "printf": s.Function, "$'ok'": s.String}},
		{"cat <<'EOF'\nhello world\nEOF", map[string]color.Color{"<<": s.Operator, "hello world": s.String}},
		{`if ! [[ -e x ]]; then true; elif false; then :; else :; fi`, map[string]color.Color{"if": s.Keyword, "!": s.Keyword, "[[": s.Keyword, "]]": s.Keyword, "then": s.Keyword, "elif": s.Keyword, "else": s.Keyword, "fi": s.Keyword}},
		{`echo "unterminated && rm -rf build`, map[string]color.Color{"echo": s.Function, "unterminated": s.String}},
		{`git commit -m "$(cat <<'EOF'...`, map[string]color.Color{"'EOF'": s.String}},
	} {
		t.Run(tc.cmd, func(t *testing.T) {
			env := testutils.NewEnv(t, 120, 8)
			out := env.Render(trail.Span(trail.SpanProps{Style: style.S().WhiteSpace(style.WhiteSpacePreWrap)}, HighlightShellSpans(tc.cmd, scheme, base)...))
			lines := testutils.TrimmedLines(out)
			if strings.Join(lines, "\n") != tc.cmd {
				t.Fatalf("text changed: %q", out)
			}
			frame := env.Backend.LastFrame().Surface
			for token, want := range tc.tokens {
				prefix, _, found := strings.Cut(tc.cmd, token)
				if !found {
					t.Fatalf("fixture missing token %q", token)
				}
				y := strings.Count(prefix, "\n")
				x := len([]rune(prefix[strings.LastIndexByte(prefix, '\n')+1:]))
				for range token {
					if got := frame.CellAt(x, y).Fg; got != want {
						t.Fatalf("%q col %d: got %v want %v", token, x, got, want)
					}
					x++
				}
			}
		})
	}
}
