package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func samplePlanReviewRequest() api.PlanReviewRequest {
	return api.PlanReviewRequest{
		ID:         "pr-1",
		ToolCallID: "tc-exit-1",
		Path:       "plans/feature.md",
		Content:    "# Feature Plan\n\n- Step 1: Implement\n- Step 2: Test\n",
	}
}

func TestPlanReviewPromptRenderAndApprove(t *testing.T) {
	var got api.PlanReviewDecision
	app := testutils.NewApp(t, PlanReviewPrompt(PlanReviewPromptProps{
		Request: samplePlanReviewRequest(),
		OnSubmit: func(_ api.PlanReviewRequest, dec api.PlanReviewDecision, _ func(error)) {
			got = dec
		},
	}), 100, 35)

	out := app.Text()
	for _, want := range []string{
		"Plan review · plans/feature.md",
		"(4 lines)",
		"Approve implementation plan?",
		"press p to view full plan",
		"1. Approve & auto-accept edits (accept_edits)",
		"2. Approve with standard prompts (default)",
		"3. Approve autonomous (auto)",
		"4. Keep planning",
		"View plan",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in rendered plan review:\n%s", want, out)
		}
	}
	for _, hidden := range []string{"Feature Plan", "Step 1: Implement"} {
		if strings.Contains(out, hidden) {
			t.Fatalf("unexpected inline markdown %q in compact asking box:\n%s", hidden, out)
		}
	}

	// Arrow down to option 2 ("default") and press Enter
	app.Press(key.Key{Code: key.KeyDown})
	app.Press(key.Key{Code: key.KeyEnter})

	if got.Canceled || !got.Approved || got.Mode != api.ModeDefault {
		t.Fatalf("unexpected approval decision: %+v", got)
	}
}

func TestPlanReviewPromptViewerToggleScrollAndReturn(t *testing.T) {
	var (
		submitted bool
		got       api.PlanReviewDecision
	)
	req := samplePlanReviewRequest()
	var lines []string
	lines = append(lines, "# Feature Plan", "", "- Step 1: Implement", "- Step 2: Test")
	for i := 3; i <= 30; i++ {
		lines = append(lines, "- Step detail line")
	}
	req.Content = strings.Join(lines, "\n") + "\n"

	app := testutils.NewApp(t, PlanReviewPrompt(PlanReviewPromptProps{
		Request: req,
		OnSubmit: func(_ api.PlanReviewRequest, dec api.PlanReviewDecision, _ func(error)) {
			submitted = true
			got = dec
		},
	}), 100, 35)

	// Toggle into Plan Viewer with 'p'.
	out := app.Type("p")
	for _, want := range []string{"Feature Plan", "Step 1: Implement", "Back to review"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in plan viewer:\n%s", want, out)
		}
	}

	// Scroll with j, Down, and PgDn without submitting or moving option cursor.
	out = app.Type("j")
	if !strings.Contains(out, "line +1") {
		t.Fatalf("expected scroll position line +1 after j, got:\n%s", out)
	}
	out = app.Press(key.Key{Code: key.KeyDown})
	if !strings.Contains(out, "line +2") {
		t.Fatalf("expected scroll position line +2 after Down, got:\n%s", out)
	}
	out = app.Press(key.Key{Code: key.KeyPgDown})
	if !strings.Contains(out, "line +10") {
		t.Fatalf("expected scroll position line +10 after PgDn, got:\n%s", out)
	}
	if submitted {
		t.Fatalf("unexpected submission while scrolling plan viewer: %+v", got)
	}

	// Esc exits the Plan Viewer back to the Asking Box without canceling.
	out = app.Press(key.Key{Code: key.KeyEscape})
	if submitted || got.Canceled {
		t.Fatalf("expected Esc in plan viewer to return to review without canceling, got submitted=%v dec=%+v", submitted, got)
	}
	if !strings.Contains(out, "1. Approve & auto-accept edits (accept_edits)") || strings.Contains(out, "Back to review") {
		t.Fatalf("expected Asking Box after Esc from plan viewer, got:\n%s", out)
	}

	// Toggle back in with 'p' and exit again with 'p' without canceling.
	out = app.Type("p")
	if !strings.Contains(out, "Back to review") || !strings.Contains(out, "line +10") {
		t.Fatalf("expected Plan Viewer with preserved scroll position after re-opening with 'p', got:\n%s", out)
	}
	app.Type("p")
	if submitted || got.Canceled {
		t.Fatalf("expected 'p' in plan viewer to return to review without canceling, got submitted=%v dec=%+v", submitted, got)
	}

	// Option cursor should still be on option 1 (accept_edits); pressing '1' approves it.
	app.Type("1")
	if !submitted || got.Canceled || !got.Approved || got.Mode != api.ModeAcceptEdits {
		t.Fatalf("expected option 1 approval after returning from viewer, got submitted=%v dec=%+v", submitted, got)
	}
}

func TestPlanReviewPromptNumberShortcutsAndFeedback(t *testing.T) {
	var got api.PlanReviewDecision
	app := testutils.NewApp(t, PlanReviewPrompt(PlanReviewPromptProps{
		Request: samplePlanReviewRequest(),
		OnSubmit: func(_ api.PlanReviewRequest, dec api.PlanReviewDecision, _ func(error)) {
			got = dec
		},
	}), 100, 35)

	// Press 'r' to enter inline feedback for Keep planning
	out := app.Type("r")
	if !strings.Contains(out, "Feedback (optional):") {
		t.Fatalf("expected feedback input after 'r', got:\n%s", out)
	}
	app.Type("Add migration step")
	app.Press(key.Key{Code: key.KeyEnter})

	if got.Canceled || got.Approved || got.Feedback != "Add migration step" {
		t.Fatalf("unexpected keep-planning decision: %+v", got)
	}
}

func TestPlanReviewPromptEscCancels(t *testing.T) {
	var got api.PlanReviewDecision
	app := testutils.NewApp(t, PlanReviewPrompt(PlanReviewPromptProps{
		Request: samplePlanReviewRequest(),
		OnSubmit: func(_ api.PlanReviewRequest, dec api.PlanReviewDecision, _ func(error)) {
			got = dec
		},
	}), 100, 35)

	app.Press(key.Key{Code: key.KeyEscape})
	if !got.Canceled || got.Approved {
		t.Fatalf("expected Esc to cancel plan review, got %+v", got)
	}
}
