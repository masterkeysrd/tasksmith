package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
)

func TestKeyboardHint(t *testing.T) {
	surfaceText := func(b *mock.Backend) string {
		frame := b.LastFrame().Surface
		if frame == nil {
			return ""
		}
		var sb strings.Builder
		for y := 0; y < frame.Height; y++ {
			var line strings.Builder
			for x := 0; x < frame.Width; x++ {
				c := frame.CellAt(x, y)
				if c.Content != "" {
					line.WriteString(c.Content)
				} else {
					line.WriteByte(' ')
				}
			}
			trimmed := strings.TrimRight(line.String(), " ")
			if trimmed != "" {
				sb.WriteString(trimmed)
				sb.WriteString("\n")
			}
		}
		return sb.String()
	}
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

	outEmpty := surfaceText(bEmpty)
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

	text := surfaceText(b)

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
