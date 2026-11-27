package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestPanel(t *testing.T) {
	out := testutils.Render(t, Panel(PanelProps{
		Title: "Daemon", Subtitle: "local", Notice: "checking", Error: "failed",
		Bindings: []KeyBinding{{Key: "esc", Label: "Close"}},
	}, trail.Text("body")), 40, 12)
	if got := strings.Fields(out); len(got) == 0 ||
		strings.Join(got[1:], " ") != "Daemon local body checking failed Keyboard: esc Close" {
		t.Fatalf("unexpected panel:\n%s", out)
	}

	plain := style.S().FlexDirection(style.FlexColumn)
	out = testutils.Render(t, Panel(PanelProps{
		Title: "hidden", Header: trail.Text("custom"), OuterStyle: &plain,
	}), 40, 8)
	if out != "custom" {
		t.Fatalf("unexpected custom panel: %q", out)
	}

	// Subtitle-only header must not shift right by a stray gap, and indented
	// multi-line notices must preserve leading spaces and newlines.
	out = testutils.Render(t, Panel(PanelProps{
		Subtitle:   "only-sub",
		Notice:     "  line 1\n  line 2",
		OuterStyle: &plain,
	}), 40, 8)
	lines := strings.Split(out, "\n")
	if len(lines) < 4 || strings.TrimRight(lines[0], " ") != "only-sub" {
		t.Fatalf("unexpected subtitle-only header: %q", out)
	}
	if strings.TrimRight(lines[2], " ") != "  line 1" || strings.TrimRight(lines[3], " ") != "  line 2" {
		t.Fatalf("expected notice indentation and newline preserved:\n%q", out)
	}
}
