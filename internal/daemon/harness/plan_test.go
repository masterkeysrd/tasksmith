package harness

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

func TestModeTransitionRemindersAndCachePreservation(t *testing.T) {
	ctx := context.Background()
	_, reg, perm := newTestToolsRegistry(t)
	plansDir := filepath.Join(t.TempDir(), "plans")

	var capturedPrompts []string
	var capturedUserMsgs [][]string
	prov := &fakeProvider{
		stepHandler: func(_ context.Context, req *llm.Request, _ int) ([]message.AssistantChunk, error) {
			var sys string
			var users []string
			for _, m := range req.Messages {
				if s, ok := m.(*message.System); ok {
					sys = s.Content.Text()
				}
				if u, ok := m.(*message.User); ok {
					users = append(users, u.Content.Text())
				}
			}
			capturedPrompts = append(capturedPrompts, sys)
			capturedUserMsgs = append(capturedUserMsgs, users)
			return []message.AssistantChunk{{
				BaseChunk: message.BaseChunk{ID: "asst"},
				Content:   []message.Block{&message.TextBlock{Text: "ok"}},
				Done:      true,
			}}, nil
		},
	}

	h, err := New(Config{
		ID:          "thread-mode-reminders",
		DB:          openTestSQLite(t),
		Bash:        &fakeBashRunner{},
		Spiller:     newTestSpiller(t),
		Permissions: perm,
		Queue:       &fakeQueue{},
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Configure(NodeOptions{
		Model:        newTestModel(t, prov),
		Tools:        reg,
		SystemPrompt: "STATIC_SYSTEM_PROMPT",
		PlansDir:     plansDir,
	}); err != nil {
		t.Fatal(err)
	}

	runTurn := func(text string) {
		for _, err := range h.Stream(ctx, Input{Message: message.NewUserText(text)}) {
			if err != nil {
				t.Fatalf("Stream(%q): %v", text, err)
			}
		}
	}

	// 1. Turn 1 in ModeDefault: silent (zero reminder injected)
	perm.SetMode(api.ModeDefault)
	runTurn("hello default")

	// 2. Switch to ModeAcceptEdits then ModeAuto: zero reminder injected
	perm.SetMode(api.ModeAcceptEdits)
	runTurn("hello accept_edits")
	perm.SetMode(api.ModeAuto)
	runTurn("hello auto")

	st, err := h.LoadState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode != api.ModeAuto {
		t.Fatalf("state.Mode = %q, want %q", st.Mode, api.ModeAuto)
	}
	for _, m := range st.Messages {
		if SystemKindOf(m) == SystemKindReminder {
			t.Fatalf("unexpected reminder during act-mode transitions: %q", msgText(m))
		}
	}

	// 3. Enter ModePlan: reminder injected with plansDir
	perm.SetMode(api.ModePlan)
	runTurn("plan this feature")
	st, _ = h.LoadState(ctx)
	if st.Mode != api.ModePlan {
		t.Fatalf("state.Mode = %q, want %q", st.Mode, api.ModePlan)
	}
	lastUsers := capturedUserMsgs[len(capturedUserMsgs)-1]
	lastReminder := lastUsers[len(lastUsers)-1]
	if !strings.Contains(lastReminder, "<system_reminders>") || !strings.Contains(lastReminder, plansDir) {
		t.Fatalf("expected plan reminder with plansDir, got %q", lastReminder)
	}

	// 4. Switch ModePlan -> ModeAsk: ask reminder injected
	perm.SetMode(api.ModeAsk)
	runTurn("why does this work?")
	lastUsers = capturedUserMsgs[len(capturedUserMsgs)-1]
	lastReminder = lastUsers[len(lastUsers)-1]
	if !strings.Contains(lastReminder, "**ask** mode") {
		t.Fatalf("expected ask reminder, got %q", lastReminder)
	}

	// 5. Exit ModeAsk -> ModeDefault: exit reminder injected
	perm.SetMode(api.ModeDefault)
	runTurn("now implement it")
	lastUsers = capturedUserMsgs[len(capturedUserMsgs)-1]
	lastReminder = lastUsers[len(lastUsers)-1]
	if !strings.Contains(lastReminder, "exited **ask** mode") || !strings.Contains(lastReminder, "**default** mode") {
		t.Fatalf("expected exit reminder, got %q", lastReminder)
	}

	// Static system prompt remained completely unchanged across all mode switches.
	for i, p := range capturedPrompts {
		if p != "STATIC_SYSTEM_PROMPT" {
			t.Fatalf("turn %d system prompt mutated: %q", i, p)
		}
	}

	// ApplyCompaction resets Mode (so plan/ask re-reminds after compaction) while keeping PlanPath.
	st.Mode = api.ModePlan
	st.PlanPath = "PLAN.md"
	compacted := st.ApplyCompaction(StaticContext{}, models.Limits{Context: 100000})
	if compacted.Mode != "" || compacted.PlanPath != "PLAN.md" {
		t.Fatalf("ApplyCompaction Mode=%q PlanPath=%q", compacted.Mode, compacted.PlanPath)
	}
}

func TestExitPlanModeInterruptAndResume(t *testing.T) {
	approved := planReviewOutcome(api.PlanReviewDecision{
		Approved: true,
		Mode:     api.ModeAcceptEdits,
	})
	if !approved.Approved || approved.Mode != api.ModeAcceptEdits {
		t.Fatalf("planReviewOutcome(accept_edits) = %+v", approved)
	}
	keep := planReviewOutcome(api.PlanReviewDecision{
		Approved: false,
		Feedback: "  Needs tests  ",
	})
	if keep.Approved || keep.Feedback != "Needs tests" {
		t.Fatalf("planReviewOutcome(keep_planning) = %+v", keep)
	}
	canceled := planReviewOutcome(api.PlanReviewDecision{Approved: true, Canceled: true})
	if canceled.Approved {
		t.Fatalf("planReviewOutcome(canceled) = %+v", canceled)
	}

	req, err := newPlanReviewRequest(&message.ToolCall{ID: "tc-exit-1", Name: "exit_plan_mode"}, &tools.Plan{
		PlanReview: &api.PlanReviewRequest{
			Path:    "plans/feature.md",
			Content: "# Plan\n",
		},
	})
	if err != nil {
		t.Fatalf("newPlanReviewRequest: %v", err)
	}
	if req.ID == "" || req.ToolCallID != "tc-exit-1" || req.Path != "plans/feature.md" || req.Content != "# Plan\n" {
		t.Fatalf("unexpected PlanReviewRequest: %+v", req)
	}

	s := applyPlanToolStateUpdates(State{Mode: api.ModePlan}, []*message.Tool{
		{
			Name: "exit_plan_mode",
			StructuredContent: api.ExitPlanModeOutput{
				Approved: true,
				Mode:     string(api.ModeAuto),
				PlanPath: "PLAN.md",
			},
		},
	})
	if s.Mode != api.ModeAuto || s.PlanPath != "PLAN.md" {
		t.Fatalf("applyPlanToolStateUpdates = Mode:%q PlanPath:%q", s.Mode, s.PlanPath)
	}
	_ = tools.PlanReviewOutcome{}
}
