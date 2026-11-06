package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/kite/testenv"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// renderOptionList renders a node in a fixed-size terminal and returns its text so
// assertions can read what a user would see.
func renderOptionList(t *testing.T, node trail.Node) string {
	t.Helper()
	env := testenv.Default(80, 24)
	t.Cleanup(env.Close)
	container := element.NewBox(env.Document())
	env.Mount(container)
	kitex.Render(node, container)
	env.Flush()
	return strings.TrimSpace(env.DumpText())
}

// trimmedLines splits rendered text into lines with their trailing padding
// removed, so assertions can reason about indentation and blank lines without
// carrying the terminal's right padding into every expectation.
func trimmedLines(out string) []string {
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return lines
}

func TestOptionListGroupsAndIcons(t *testing.T) {
	scheme := theme.Get()
	icon := func(text string) trail.Node {
		return trail.Span(trail.SpanProps{
			Style: style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Success),
		}, trail.Text(text))
	}
	options := []Option{
		{Value: "a", Label: "alpha", Cells: []string{"alpha", "2 tools"}, Icon: icon("✔")},
		{Value: "b", Label: "beta", Cells: []string{"beta", "1 tool"}, Icon: icon("✔")},
		{Value: "c", Label: "gamma", Cells: []string{"gamma", "idle"}},
	}
	groups := map[string]string{"a": "Workspace MCPs", "b": "Workspace MCPs", "c": "User MCPs"}

	out := renderOptionList(t, OptionList(OptionListProps{
		Options:  options,
		Selected: 0,
		Columns:  []Column{{Width: 20}, {Width: 0}},
		GroupLabel: func(opt Option) string {
			return groups[opt.Value]
		},
	}))

	for _, want := range []string{"Workspace MCPs", "User MCPs", "✔ alpha", "✔ beta", "gamma"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing missing %q:\n%s", want, out)
		}
	}
	// A group after the first is separated by a blank line so the headings read
	// as blocks; the first heading keeps its line free because the caller already
	// printed a title or count above the listing. Headings share the column a
	// row's status glyph sits in rather than the cursor gutter.
	lines := trimmedLines(out)
	if len(lines) < 2 {
		t.Fatalf("expected a multi-line listing, got %q", out)
	}
	if lines[0] != "Workspace MCPs" {
		t.Errorf("first group heading should sit on the listing's first line, got %q:\n%s", lines[0], out)
	}
	userHeading := -1
	for i, line := range lines {
		if line == "  User MCPs" {
			userHeading = i
			break
		}
	}
	switch {
	case userHeading == -1:
		t.Errorf("second group heading missing its glyph-column indent:\n%s", out)
	case lines[userHeading-1] != "":
		t.Errorf("second group heading should be preceded by a blank line, got %q:\n%s", lines[userHeading-1], out)
	}
	// One heading per group, not one per row.
	if n := strings.Count(out, "Workspace MCPs"); n != 1 {
		t.Errorf("Workspace heading rendered %d times, want 1:\n%s", n, out)
	}
	// The cursor still marks the selected row, sharing its gutter with the icon.
	if !strings.Contains(out, "❯ ✔ alpha") {
		t.Errorf("selected row should carry the cursor and the icon:\n%s", out)
	}
	if strings.Contains(out, "❯ ✔ beta") {
		t.Errorf("only one row should carry the cursor:\n%s", out)
	}
}

func TestOptionListWithoutGroupsOrIcons(t *testing.T) {
	options := []Option{
		{Value: "a", Label: "alpha", Cells: []string{"alpha", "ready"}},
		{Value: "b", Label: "beta", Cells: []string{"beta", "ready"}},
	}
	out := renderOptionList(t, OptionList(OptionListProps{
		Options: options,
		Columns: []Column{{Width: 20}, {Width: 0}},
	}))
	for _, unwanted := range []string{"MCPs", "✔"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("an ungrouped listing should not render %q:\n%s", unwanted, out)
		}
	}
	for _, want := range []string{"alpha", "beta", "ready"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing missing %q:\n%s", want, out)
		}
	}
}

// TestOptionListIconInLabelOnlyRows covers the two-column fallback path, where a
// row has no explicit cells and renders its label and description.
func TestOptionListIconInLabelOnlyRows(t *testing.T) {
	scheme := theme.Get()
	out := renderOptionList(t, OptionList(OptionListProps{
		Options: []Option{
			{Value: "a", Label: "alpha", Description: "ready", Icon: trail.Span(trail.SpanProps{
				Style: style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Warning),
			}, trail.Text("⚠"))},
		},
	}))
	if !strings.Contains(out, "⚠ alpha") {
		t.Errorf("label-only row missing its icon:\n%s", out)
	}
	if !strings.Contains(out, "ready") {
		t.Errorf("label-only row missing its description:\n%s", out)
	}
}
