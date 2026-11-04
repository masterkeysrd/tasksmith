package components

import (
	"strings"
	"testing"
)

func TestLabelFromValueRule(t *testing.T) {
	label, symKind := labelFromValue("rule", "go-error-handling", nil)
	if label != "go-error-handling" {
		t.Errorf("label = %q, want %q", label, "go-error-handling")
	}
	if symKind != "" {
		t.Errorf("symKind = %q, want empty for rule refs", symKind)
	}
}

func TestKnownPrefixesIncludesRule(t *testing.T) {
	if got := knownPrefixes["@rule:"]; got != "rule" {
		t.Errorf("knownPrefixes[@rule:] = %q, want %q", got, "rule")
	}
}

func TestMarkdownRender_RulePill(t *testing.T) {
	const ruleName = "go-error-handling"

	env := renderMarkdownEnv(t, 80, 6, "Refactor the handler following @rule:"+ruleName)
	defer env.Close()

	lines := nonEmptyLines(env.DumpText())
	if len(lines) == 0 {
		t.Fatal("expected rendered markdown output")
	}

	idx := firstCellIndex(lines[0], ruleName)
	if idx < 0 {
		t.Fatalf("expected rule pill label %q in rendered line %q", ruleName, lines[0])
	}
	if strings.Contains(lines[0], "@rule:") {
		t.Errorf("raw @rule: prefix should be replaced by a pill, got %q", lines[0])
	}

	frame := env.Backend.LastFrame()
	if frame.Surface == nil {
		t.Fatal("expected a rendered framebuffer")
	}
	for offset := range len(ruleName) {
		cell := frame.Surface.CellAt(idx+offset, 0)
		if cell.Bg == nil {
			t.Fatalf("rule pill cell %d missing background styling", offset)
		}
	}
}
