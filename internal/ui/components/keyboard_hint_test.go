package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestKeyboardHint(t *testing.T) {
	// Empty bindings should render nil
	bEmpty := mock.New(80, 5)
	appEmpty := trail.SimpleFC("TestEmpty", func() kitex.Node {
		return KeyboardHint(KeyboardHintProps{Bindings: nil})
	})
	instEmpty, err := trail.New(appEmpty(), trail.WithBackend(bEmpty))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer instEmpty.Unmount()
	instEmpty.Frame()

	outEmpty := testutils.SurfaceText(bEmpty)
	if strings.TrimSpace(outEmpty) != "" {
		t.Fatalf("expected empty render for nil bindings, got %q", outEmpty)
	}

	bindings := []KeyBinding{
		{Key: "↑/↓", Label: "Navigate"},
		{Key: "tab", Label: "Scope"},
		{Key: "enter", Label: "Confirm"},
	}

	b := mock.New(80, 5)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return KeyboardHint(KeyboardHintProps{Bindings: bindings})
	})
	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()

	text := testutils.SurfaceText(b)

	if !strings.Contains(text, "Keyboard:") {
		t.Errorf("expected text to contain 'Keyboard:', got %q", text)
	}
	if !strings.Contains(text, "↑/↓ Navigate") {
		t.Errorf("expected text to contain '↑/↓ Navigate', got %q", text)
	}
	if !strings.Contains(text, "tab Scope") {
		t.Errorf("expected text to contain 'tab Scope', got %q", text)
	}
	if !strings.Contains(text, "enter Confirm") {
		t.Errorf("expected text to contain 'enter Confirm', got %q", text)
	}
}

func TestKeyboardHintWrapsWholePairsOnNarrowWidth(t *testing.T) {
	bindings := []KeyBinding{
		{Key: "←/→", Label: "Tab"},
		{Key: "s", Label: "Scope"},
		{Key: "↑/↓", Label: "Move"},
		{Key: "space", Label: "Toggle"},
		{Key: "x", Label: "Reset"},
		{Key: "esc", Label: "Close"},
	}

	// Wide enough for a single line (80 cols): renders on one row.
	wide := testutils.Render(t, KeyboardHint(KeyboardHintProps{Bindings: bindings}), 80, 6)
	wideLines := nonEmptyLines(wide)
	if len(wideLines) != 1 {
		t.Fatalf("expected 1 line at width 80, got %d:\n%s", len(wideLines), wide)
	}
	wantWide := "Keyboard: ←/→ Tab  s Scope  ↑/↓ Move  space Toggle  x Reset  esc Close"
	if wideLines[0] != wantWide {
		t.Fatalf("unexpected wide line:\ngot:  %q\nwant: %q", wideLines[0], wantWide)
	}

	// Narrow split pane (42 cols): wraps whole key/label pairs without splitting
	// a key from its label, indenting continuation lines under the first key.
	narrow := testutils.Render(t, KeyboardHint(KeyboardHintProps{Bindings: bindings}), 42, 6)
	narrowLines := nonEmptyLines(narrow)
	if len(narrowLines) < 2 {
		t.Fatalf("expected wrapped lines at width 42, got:\n%s", narrow)
	}
	for _, b := range bindings {
		pair := b.Key + " " + b.Label
		if !strings.Contains(narrow, pair) {
			t.Errorf("expected intact pair %q in narrow render:\n%s", pair, narrow)
		}
	}
	const prefix = "Keyboard: "
	indent := strings.Repeat(" ", len(prefix))
	for i, line := range narrowLines[1:] {
		if !strings.HasPrefix(line, indent) || strings.HasPrefix(line, indent+" ") {
			t.Errorf("continuation line %d not aligned under first key (%d spaces): %q\n%s",
				i+2, len(prefix), line, narrow)
		}
	}
}
