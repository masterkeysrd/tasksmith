package components

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
)

func renderNode(t *testing.T, node trail.Node) string {
	t.Helper()
	b := mock.New(80, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = trail.Run(ctx, node, trail.WithBackend(b))
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.LastFrame().Surface != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return surfaceText(b)
}

func TestHintSegment(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	// 1. Default hint
	out := renderNode(t, HintSegment(SegmentProps{}))
	if !strings.Contains(out, store.DefaultHint) {
		t.Fatalf("expected default hint %q, got:\n%s", store.DefaultHint, out)
	}

	// 2. Custom hint via SetHint
	store.SetHint("custom picker hint")
	out = renderNode(t, HintSegment(SegmentProps{}))
	if !strings.Contains(out, "custom picker hint") {
		t.Fatalf("expected custom hint, got:\n%s", out)
	}

	// 3. CtrlCPending priority over custom hint
	store.SetCtrlCPending(true)
	out = renderNode(t, HintSegment(SegmentProps{}))
	if !strings.Contains(out, store.CtrlCHint) {
		t.Fatalf("expected CtrlCHint %q, got:\n%s", store.CtrlCHint, out)
	}
	if strings.Contains(out, "custom picker hint") {
		t.Fatalf("custom hint should be overridden by CtrlCHint, got:\n%s", out)
	}
}

func TestModeSegment(t *testing.T) {
	store.ResetSession()
	defer store.ResetSession()

	// 1. Default mode: hidden (nil)
	store.SetMode(api.ModeDefault)
	out := renderNode(t, ModeSegment(SegmentProps{Separator: " · "}))
	if strings.Contains(out, "default") {
		t.Fatalf("expected default mode to be hidden, got:\n%s", out)
	}

	// 2. Non-default modes
	modes := []api.PermissionMode{
		api.ModePlan,
		api.ModeAcceptEdits,
		api.ModeAsk,
		api.ModeAuto,
	}

	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			store.SetMode(mode)
			out := renderNode(t, ModeSegment(SegmentProps{Separator: " · "}))
			if !strings.Contains(out, string(mode)) {
				t.Fatalf("expected mode %q in output, got:\n%s", mode, out)
			}
			if !strings.Contains(out, "·") {
				t.Fatalf("expected separator '·' in output, got:\n%s", out)
			}
		})
	}
}

func TestModelSegment(t *testing.T) {
	store.ResetSession()
	defer store.ResetSession()

	// 1. Not ready: hidden (nil)
	out := renderNode(t, ModelSegment(SegmentProps{}))
	if strings.TrimSpace(out) != "" {
		t.Fatalf("expected ModelSegment to be hidden when !Ready, got:\n%s", out)
	}

	// 2. Ready with model info
	store.SetModelInfo("Claude 3.7 Sonnet", "high")
	out = renderNode(t, ModelSegment(SegmentProps{}))
	if !strings.Contains(out, "Claude 3.7 Sonnet · high") {
		t.Fatalf("expected 'Claude 3.7 Sonnet · high', got:\n%s", out)
	}

	// 3. Ready with empty values defaults to "default · auto"
	store.SetModelInfo("", "")
	out = renderNode(t, ModelSegment(SegmentProps{}))
	if !strings.Contains(out, "default · auto") {
		t.Fatalf("expected 'default · auto', got:\n%s", out)
	}
}

func TestDefaultStatusLine(t *testing.T) {
	store.ResetApp()
	store.ResetSession()
	defer store.ResetApp()
	defer store.ResetSession()

	store.SetMode(api.ModePlan)
	store.SetModelInfo("TestModel", "medium")

	out := renderNode(t, DefaultStatusLine())
	if !strings.Contains(out, store.DefaultHint) {
		t.Fatalf("expected hint in status line, got:\n%s", out)
	}
	if !strings.Contains(out, "plan · TestModel · medium") {
		t.Fatalf("expected 'plan · TestModel · medium' in status line, got:\n%s", out)
	}
}
