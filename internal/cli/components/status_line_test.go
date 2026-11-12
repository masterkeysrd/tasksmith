package components

import (
	"slices"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func renderNode(t *testing.T, node trail.Node) string {
	t.Helper()
	return testutils.Render(t, node, 80, 24)
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

	// 4. When a slot is active, HintSegment is hidden (nil) unless CtrlCPending is true
	store.SetCtrlCPending(false)
	store.ClearSlot()
	store.EnqueueSlot(store.SlotItem{ID: "test-slot", Kind: store.SlotKindPicker})
	defer store.ClearSlot()

	out = renderNode(t, HintSegment(SegmentProps{}))
	if strings.Contains(out, "custom picker hint") || strings.Contains(out, store.DefaultHint) {
		t.Fatalf("expected HintSegment to be hidden when slot is active, got:\n%s", out)
	}

	// 5. CtrlCPending overrides active slot hiding
	store.SetCtrlCPending(true)
	out = renderNode(t, HintSegment(SegmentProps{}))
	if !strings.Contains(out, store.CtrlCHint) {
		t.Fatalf("expected CtrlCHint %q when CtrlCPending is true even with active slot, got:\n%s", store.CtrlCHint, out)
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

	// 3. Ready with empty values defaults to "default · provider default"
	store.SetModelInfo("", "")
	out = renderNode(t, ModelSegment(SegmentProps{}))
	if !strings.Contains(out, "default · provider default") {
		t.Fatalf("expected 'default · provider default', got:\n%s", out)
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

func TestDefaultStatusLineWithActiveSlot(t *testing.T) {
	store.ResetApp()
	store.ResetSession()
	defer store.ResetApp()
	defer store.ResetSession()

	store.SetMode(api.ModePlan)
	store.SetModelInfo("TestModel", "medium")

	// Activate a slot
	store.EnqueueSlot(store.SlotItem{ID: "slot-1", Kind: store.SlotKindPicker})
	defer store.ClearSlot()

	out := renderNode(t, DefaultStatusLine())
	if strings.Contains(out, store.DefaultHint) {
		t.Fatalf("hint must not render when slot is active, got:\n%s", out)
	}
	if !strings.Contains(out, "plan · TestModel · medium") {
		t.Fatalf("expected right segments with separator when slot is active, got:\n%s", out)
	}
}

func TestPlanStatusLine(t *testing.T) {
	all := []store.StatusLineSegmentID{store.StatusLineHint, store.StatusLineMode, store.StatusLineContext, store.StatusLineUsage, store.StatusLineModel}
	const (
		hint    = "shift+tab cycle mode - /help commands"
		mode    = "plan"
		ctxFull = "109.5k/400.0k (27%)"
		ctxPct  = "27%"
		usage   = "$0.42 - up128.4k down8.3k"
		mFull   = "Qwen 3.8 DFlash2 NVFP4 - high"
		mComp   = "Qwen 3.8 DFlash2 NVFP4"
	)

	t.Run("width zero keeps everything", func(t *testing.T) {
		plan := planStatusLine(0, false, all, hint, mode, ctxFull, ctxPct, usage, mFull, mComp)
		if plan.Texts[store.StatusLineUsage] != usage || plan.Texts[store.StatusLineModel] != mFull {
			t.Fatalf("expected full texts at width 0, got %v", plan.Texts)
		}
	})

	t.Run("wide fits unchanged", func(t *testing.T) {
		plan := planStatusLine(200, false, all, hint, mode, ctxFull, ctxPct, usage, mFull, mComp)
		if plan.Texts[store.StatusLineModel] != mFull || plan.Texts[store.StatusLineUsage] != usage {
			t.Fatalf("expected no changes at width 200, got %v", plan.Texts)
		}
	})

	t.Run("ladder compacts model before dropping usage", func(t *testing.T) {
		// Wide enough for everything except the model suffix.
		need := len([]rune(hint)) + len([]rune(mode)) + len([]rune(ctxFull)) + len([]rune(usage)) + len([]rune(mComp)) + 4*len([]rune(" · "))
		plan := planStatusLine(need, false, all, hint, mode, ctxFull, ctxPct, usage, mFull, mComp)
		if plan.Texts[store.StatusLineModel] != mComp {
			t.Fatalf("expected compact model, got %q", plan.Texts[store.StatusLineModel])
		}
		if plan.Texts[store.StatusLineUsage] != usage {
			t.Fatalf("usage should survive model compaction, got %q", plan.Texts[store.StatusLineUsage])
		}
	})

	t.Run("usage dropped before hint", func(t *testing.T) {
		need := len([]rune(hint)) + len([]rune(mode)) + len([]rune(ctxFull)) + len([]rune(mComp)) + 3*len([]rune(" · "))
		plan := planStatusLine(need, false, all, hint, mode, ctxFull, ctxPct, usage, mFull, mComp)
		if plan.Texts[store.StatusLineUsage] != "" {
			t.Fatalf("expected usage hidden, got %q", plan.Texts[store.StatusLineUsage])
		}
		if plan.Texts[store.StatusLineHint] != hint {
			t.Fatalf("expected hint kept, got %q", plan.Texts[store.StatusLineHint])
		}
	})

	t.Run("mode never hidden", func(t *testing.T) {
		plan := planStatusLine(4, false, all, hint, mode, ctxFull, ctxPct, usage, mFull, mComp)
		if plan.Texts[store.StatusLineMode] != mode {
			t.Fatalf("expected mode kept at width 4, got %q", plan.Texts[store.StatusLineMode])
		}
		if plan.Texts[store.StatusLineHint] != "" || plan.Texts[store.StatusLineUsage] != "" ||
			plan.Texts[store.StatusLineContext] != "" || plan.Texts[store.StatusLineModel] != "" {
			t.Fatalf("expected expendable segments hidden at width 4, got %v", plan.Texts)
		}
	})

	t.Run("unconfigured segments excluded", func(t *testing.T) {
		plan := planStatusLine(20, false, []store.StatusLineSegmentID{store.StatusLineMode}, hint, mode, ctxFull, ctxPct, usage, mFull, mComp)
		if _, ok := plan.Texts[store.StatusLineHint]; ok {
			t.Fatalf("hint must not appear when unconfigured: %v", plan.Texts)
		}
		if plan.Texts[store.StatusLineMode] != mode {
			t.Fatalf("expected mode, got %v", plan.Texts)
		}
	})

	t.Run("compact starts from compact variants", func(t *testing.T) {
		plan := planStatusLine(200, true, all, hint, mode, ctxFull, ctxPct, usage, mFull, mComp)
		if plan.Texts[store.StatusLineContext] != ctxPct || plan.Texts[store.StatusLineModel] != mComp {
			t.Fatalf("expected compact variants, got %v", plan.Texts)
		}
	})
}

func TestDefaultStatusLineResponsive(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith")
	defer xdg.ClearCache()
	store.ResetApp()
	store.ResetSession()
	defer store.ResetApp()
	defer store.ResetSession()

	store.SetSession(&api.Session{
		ID:      "sess-status",
		Context: api.ContextState{UsedTokens: 109500, MaxTokens: 400000, TriggerTokens: 356000},
	})
	store.SetModelInfo("Qwen 3.8 DFlash2 NVFP4", "high")
	store.SetMode(api.ModePlan)
	_, _ = store.SaveStatusLine(store.StatusLineConfig{Segments: slices.Clone(store.DefaultStatusLineSegments)})

	// Wide terminal: model with thinking effort visible.
	out := testutils.Render(t, DefaultStatusLine(), 120, 4)
	if !strings.Contains(out, "Qwen 3.8 DFlash2 NVFP4") || !strings.Contains(out, "plan") {
		t.Fatalf("expected full status line at width 120, got:\n%s", out)
	}

	// Narrow terminal: segments must not collide; expendable ones disappear.
	out = testutils.Render(t, DefaultStatusLine(), 24, 4)
	for line := range strings.SplitSeq(out, "\n") {
		if len([]rune(strings.TrimRight(line, " "))) > 24 {
			t.Fatalf("line exceeds terminal width at 24 cols:\n%q", line)
		}
	}
	if !strings.Contains(out, "plan") {
		t.Fatalf("expected mode badge to survive at width 24, got:\n%s", out)
	}

	// Config without usage/model drops them even on a wide terminal.
	_, _ = store.SaveStatusLine(store.StatusLineConfig{Segments: []store.StatusLineSegmentID{store.StatusLineMode, store.StatusLineContext}})
	defer func() {
		_, _ = store.SaveStatusLine(store.StatusLineConfig{Segments: slices.Clone(store.DefaultStatusLineSegments)})
	}()
	out = testutils.Render(t, DefaultStatusLine(), 120, 4)
	if !strings.Contains(out, "plan") || !strings.Contains(out, "109.5k/400.0k") {
		t.Fatalf("expected configured mode+context, got:\n%s", out)
	}
	if strings.Contains(out, "Qwen") || strings.Contains(out, store.DefaultHint) {
		t.Fatalf("unconfigured segments must not render, got:\n%s", out)
	}
}

func TestContextSegment(t *testing.T) {
	store.ResetSession()
	defer store.ResetSession()

	// 1. Hidden when MaxTokens == 0
	out := renderNode(t, ContextSegment(SegmentProps{Separator: " · "}))
	if strings.TrimSpace(out) != "" {
		t.Fatalf("expected ContextSegment to be hidden when MaxTokens == 0, got:\n%s", out)
	}

	// 2. Visible when MaxTokens > 0
	store.SetSession(&api.Session{
		ID: "sess-ctx-test",
		Context: api.ContextState{
			UsedTokens:    109500,
			MaxTokens:     400000,
			TriggerTokens: 356000,
		},
	})
	out = renderNode(t, ContextSegment(SegmentProps{Separator: " · "}))
	if !strings.Contains(out, "109.5k/400.0k (27%)") {
		t.Fatalf("expected '109.5k/400.0k (27%%)' in ContextSegment, got:\n%s", out)
	}
}
