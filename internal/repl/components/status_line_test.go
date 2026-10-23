package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
)

func renderStatus(t *testing.T, status StatusContext, node func() trail.Node) (string, int) {
	t.Helper()
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return StatusProvider(status, node())
	})
	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()
	return surfaceText(b), b.LastFrame().Surface.Height
}

func renderDefaultStatusLine(t *testing.T, status StatusContext) (string, int) {
	t.Helper()
	return renderStatus(t, status, func() trail.Node { return DefaultStatusLine(struct{}{}) })
}

func TestStatusLine(t *testing.T) {
	// 1. Default mode: shows "<Model> · <Thinking>" without "default · "
	out1, h := renderDefaultStatusLine(t, StatusContext{
		State: ui.StateIdle, Mode: api.ModeDefault, Model: "Claude 3.7 Sonnet", Thinking: "high",
	})
	if h != 1 {
		t.Errorf("expected StatusLine height 1, got %d:\n%s", h, out1)
	}
	if !strings.Contains(out1, "Claude 3.7 Sonnet · high") {
		t.Errorf("expected model · thinking in status line:\n%s", out1)
	}
	if strings.Contains(out1, "default · ") {
		t.Errorf("default mode should not be prefixed with 'default · ':\n%s", out1)
	}
	if !strings.Contains(out1, "shift+tab cycle mode") {
		t.Errorf("idle state hint missing on left:\n%s", out1)
	}

	// 2. Non-default mode: shows "<mode> · <Model> · <Thinking>"
	out2, h := renderDefaultStatusLine(t, StatusContext{
		State: ui.StateStreaming, Mode: api.ModeAcceptEdits, Model: "Claude 3.7 Sonnet", Thinking: "high",
	})
	if h != 1 {
		t.Errorf("expected StatusLine height 1 in non-default mode, got %d:\n%s", h, out2)
	}
	if !strings.Contains(out2, "accept_edits · Claude 3.7 Sonnet · high") {
		t.Errorf("expected mode · model · thinking:\n%s", out2)
	}
	if !strings.Contains(out2, "streaming • esc to cancel") {
		t.Errorf("streaming hint missing:\n%s", out2)
	}

	// 3. Custom hint override
	out3, _ := renderDefaultStatusLine(t, StatusContext{State: ui.StatePicker, Hint: "custom hint override"})
	if !strings.Contains(out3, "custom hint override") {
		t.Errorf("expected custom hint:\n%s", out3)
	}

	// 4. Picker with an empty hint: nothing on the left, model/thinking on the right
	out4, _ := renderDefaultStatusLine(t, StatusContext{State: ui.StatePicker, Model: "Claude 3.7 Sonnet", Thinking: "high"})
	if !strings.Contains(out4, "Claude 3.7 Sonnet · high") {
		t.Errorf("expected model · thinking on right:\n%s", out4)
	}
	if strings.Contains(out4, "navigate") || strings.Contains(out4, "select") {
		t.Errorf("expected no hint on left in StatePicker, got:\n%s", out4)
	}
}

func TestStatusSegmentsReadFromProviderAndComposeFreely(t *testing.T) {
	// Segments are self-sufficient: composing them in another order needs no props.
	out, _ := renderStatus(t, StatusContext{Mode: api.ModePlan, Model: "m", Thinking: "high"}, func() trail.Node {
		return StatusLine(StatusLineProps{},
			ModelSegment(SegmentProps{Separator: " | "}),
			ModeSegment(SegmentProps{}),
		)
	})
	if !strings.Contains(out, "m · high | plan") {
		t.Errorf("composed segments = %q", out)
	}
}

func TestStatusSegmentHidesSeparatorWithItself(t *testing.T) {
	// The mode segment is hidden in default mode, and so is its separator.
	out, _ := renderDefaultStatusLine(t, StatusContext{Mode: api.ModeDefault, Model: "m", Thinking: "high"})
	if strings.Contains(out, "·  m") || strings.Contains(out, " · m · high") {
		t.Errorf("hidden mode segment left a separator: %q", out)
	}
	if !strings.Contains(out, "m · high") {
		t.Errorf("model segment missing: %q", out)
	}
}

func TestStatusSegmentDefaults(t *testing.T) {
	out, _ := renderStatus(t, StatusContext{State: ui.StatePicker}, func() trail.Node {
		return StatusLine(StatusLineProps{}, HintSegment(SegmentProps{}), ModelSegment(SegmentProps{}))
	})
	if !strings.Contains(out, "default · auto") {
		t.Errorf("blank model/thinking defaults missing: %q", out)
	}
}
