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

func TestStatusLine(t *testing.T) {
	// 1. Default mode: shows "<Model> · <Thinking>" without "default · "
	b1 := mock.New(80, 24)
	app1 := trail.SimpleFC("TestApp", func() kitex.Node {
		return StatusLine(StatusLineProps{
			State:    ui.StateIdle,
			Mode:     api.ModeDefault,
			Model:    "Claude 3.7 Sonnet",
			Thinking: "high",
		})
	})
	inst1, _ := trail.New(app1(), trail.WithBackend(b1))
	inst1.Frame()
	if h := b1.LastFrame().Surface.Height; h != 1 {
		t.Errorf("expected StatusLine height 1, got %d:\n%s", h, surfaceText(b1))
	}
	out1 := surfaceText(b1)
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
	b2 := mock.New(80, 24)
	app2 := trail.SimpleFC("TestApp", func() kitex.Node {
		return StatusLine(StatusLineProps{
			State:    ui.StateStreaming,
			Mode:     api.ModeAcceptEdits,
			Model:    "Claude 3.7 Sonnet",
			Thinking: "high",
		})
	})
	inst2, _ := trail.New(app2(), trail.WithBackend(b2))
	inst2.Frame()
	if h := b2.LastFrame().Surface.Height; h != 1 {
		t.Errorf("expected StatusLine height 1 in non-default mode, got %d:\n%s", h, surfaceText(b2))
	}
	out2 := surfaceText(b2)
	if !strings.Contains(out2, "accept_edits · Claude 3.7 Sonnet · high") {
		t.Errorf("expected mode · model · thinking:\n%s", out2)
	}
	if !strings.Contains(out2, "streaming • esc to cancel") {
		t.Errorf("streaming hint missing:\n%s", out2)
	}

	// 3. Custom hint override
	b3 := mock.New(80, 24)
	app3 := trail.SimpleFC("TestApp", func() kitex.Node {
		return StatusLine(StatusLineProps{
			State: ui.StatePicker,
			Hint:  "custom hint override",
		})
	})
	inst3, _ := trail.New(app3(), trail.WithBackend(b3))
	inst3.Frame()
	out3 := surfaceText(b3)
	if !strings.Contains(out3, "custom hint override") {
		t.Errorf("expected custom hint:\n%s", out3)
	}

	// 4. ui.StatePicker with empty hint: leftText is empty, right side displays model/thinking
	b4 := mock.New(80, 24)
	app4 := trail.SimpleFC("TestApp", func() kitex.Node {
		return StatusLine(StatusLineProps{
			State:    ui.StatePicker,
			Model:    "Claude 3.7 Sonnet",
			Thinking: "high",
		})
	})
	inst4, _ := trail.New(app4(), trail.WithBackend(b4))
	inst4.Frame()
	out4 := surfaceText(b4)
	if !strings.Contains(out4, "Claude 3.7 Sonnet · high") {
		t.Errorf("expected model · thinking on right:\n%s", out4)
	}
	if strings.Contains(out4, "navigate") || strings.Contains(out4, "select") {
		t.Errorf("expected no hint on left in ui.StatePicker, got:\n%s", out4)
	}
}
