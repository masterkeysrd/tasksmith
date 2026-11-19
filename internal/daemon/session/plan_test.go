package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

func TestRestrictedWorkspaceStartsDefaultAndAllowsSessionPlansDir(t *testing.T) {
	ctx := context.Background()
	mgr := newSandboxPolicyCoordinator(t, "", true)
	sess, _, err := mgr.Create(ctx, api.CreateSessionRequest{Title: "restricted default"})
	if err != nil {
		t.Fatalf("Create restricted: %v", err)
	}
	if sess.Config.Mode != api.ModeDefault {
		t.Fatalf("restricted workspace initial Mode = %q, want %q", sess.Config.Mode, api.ModeDefault)
	}

	planPath := filepath.Join(sess.PlansDir(), "draft.md")
	planCall := &message.ToolCall{
		ID:   "tc-write-plan",
		Name: "write",
		Args: map[string]any{"path": planPath, "content": "# Restricted Plan\n"},
	}
	planPrep, err := sess.Tools.Prepare(ctx, planCall)
	if err != nil {
		t.Fatalf("Prepare write into plansDir: %v", err)
	}
	if dec, _, err := sess.PermManager.Evaluate(planPrep); err != nil || dec != tools.AccessAllow {
		t.Fatalf("Evaluate write into plansDir = (%v, %v), want AccessAllow", dec, err)
	}
	writePlanRes, err := sess.Tools.Execute(ctx, planCall)
	if err != nil || writePlanRes.IsError {
		t.Fatalf("write into plansDir in restricted workspace failed: res=%+v err=%v", writePlanRes, err)
	}
	data, err := os.ReadFile(planPath)
	if err != nil || string(data) != "# Restricted Plan\n" {
		t.Fatalf("plan file contents = %q, %v", data, err)
	}

	repoPrep, err := sess.Tools.Prepare(ctx, &message.ToolCall{
		ID:   "tc-write-repo",
		Name: "write",
		Args: map[string]any{"path": "blocked.txt", "content": "nope\n"},
	})
	if err != nil {
		t.Fatalf("Prepare repo write: %v", err)
	}
	if dec, _, err := sess.PermManager.Evaluate(repoPrep); err != nil || dec != tools.AccessDeny {
		t.Fatalf("Evaluate repo write in restricted workspace = (%v, %v), want AccessDeny", dec, err)
	}
}

func TestSessionEnterAndExitPlanModeTurnFlow(t *testing.T) {
	ctx := context.Background()
	step := 0
	var planFilePath string

	provider := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) ([]message.AssistantChunk, error) {
			step++
			switch step {
			case 1:
				return []message.AssistantChunk{{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content: []message.Block{
						&message.ToolCall{ID: "tc-enter", Name: "enter_plan_mode", Args: map[string]any{}},
					},
					Done: true,
				}}, nil
			case 2:
				return []message.AssistantChunk{{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content: []message.Block{
						&message.ToolCall{
							ID:   "tc-write-plan",
							Name: "write",
							Args: map[string]any{
								"path":    planFilePath,
								"content": "# Implementation Plan\n\n- Step 1\n",
							},
						},
					},
					Done: true,
				}}, nil
			case 3:
				return []message.AssistantChunk{{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content: []message.Block{
						&message.ToolCall{
							ID:   "tc-exit",
							Name: "exit_plan_mode",
							Args: map[string]any{"path": planFilePath},
						},
					},
					Done: true,
				}}, nil
			default:
				return []message.AssistantChunk{{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content:   []message.Block{&message.TextBlock{Text: "Plan approved and executed."}},
					Done:      true,
				}}, nil
			}
		},
	}

	_, _, _, pub, mgr := setupTestEnv(t, provider)
	sess, _, err := mgr.Create(ctx, api.CreateSessionRequest{Title: "Plan Flow"})
	if err != nil {
		t.Fatal(err)
	}
	planFilePath = filepath.Join(sess.PlansDir(), "feature.md")

	if err := sess.Send(ctx, api.SendRequest{Content: "Design a feature"}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatal(err)
	}

	if sess.Config.Mode != api.ModePlan {
		t.Fatalf("after enter_plan_mode: mode = %q, want %q", sess.Config.Mode, api.ModePlan)
	}
	if sess.Status != api.StatusWaitingPlanReview || len(sess.PendingPlanReviews) != 1 {
		t.Fatalf("expected waiting_plan_review for exit_plan_mode, got status=%s plan_reviews=%+v", sess.Status, sess.PendingPlanReviews)
	}
	if len(sess.PendingQuestions) != 0 {
		t.Fatalf("expected zero PendingQuestions for exit_plan_mode, got %+v", sess.PendingQuestions)
	}
	pr := sess.PendingPlanReviews[0]
	if pr.ID == "" || pr.ToolCallID == "" || pr.Path != filepath.ToSlash(planFilePath) || !strings.Contains(pr.Content, "# Implementation Plan") {
		t.Fatalf("unexpected PlanReviewRequest: %+v", pr)
	}

	// Approve and switch to accept_edits
	if err := sess.Send(ctx, api.SendRequest{
		PlanReview: &api.PlanReviewDecision{
			RequestID: pr.ID,
			Approved:  true,
			Mode:      api.ModeAcceptEdits,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatal(err)
	}

	if sess.Status != api.StatusIdle {
		t.Fatalf("expected StatusIdle after approval, got %s", sess.Status)
	}
	if sess.Config.Mode != api.ModeAcceptEdits {
		t.Fatalf("after exit_plan_mode approval: mode = %q, want %q", sess.Config.Mode, api.ModeAcceptEdits)
	}

	st, err := sess.Harness.LoadState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode != api.ModeAcceptEdits || st.PlanPath != filepath.ToSlash(planFilePath) {
		t.Fatalf("harness state Mode=%q PlanPath=%q", st.Mode, st.PlanPath)
	}

	var modeChanges []api.ModeChange
	var planReviews []api.PlanReviewRecord
	tl, _ := sess.Timeline.List(ctx)
	for _, e := range tl {
		if e.Kind == api.TimelineKindMode && e.Mode != nil {
			modeChanges = append(modeChanges, *e.Mode)
		}
		if e.Kind == api.TimelineKindPlanReview && e.PlanReview != nil {
			planReviews = append(planReviews, *e.PlanReview)
		}
	}
	if len(modeChanges) != 2 || modeChanges[0].To != api.ModePlan || modeChanges[1].To != api.ModeAcceptEdits {
		t.Fatalf("unexpected mode timeline entries: %+v", modeChanges)
	}
	if len(planReviews) != 1 || planReviews[0].Request.ID != pr.ID || !planReviews[0].Decision.Approved || planReviews[0].Decision.Mode != api.ModeAcceptEdits {
		t.Fatalf("unexpected plan_review timeline entries: %+v", planReviews)
	}

	var planReviewEvCount int
	var questionEvCount int
	for _, ev := range pub.recorded() {
		if _, ok := ev.(api.PlanReviewRequestedEvent); ok {
			planReviewEvCount++
		}
		if _, ok := ev.(api.QuestionRequestedEvent); ok {
			questionEvCount++
		}
	}
	if planReviewEvCount == 0 {
		t.Fatal("expected PlanReviewRequestedEvent for exit_plan_mode")
	}
	if questionEvCount != 0 {
		t.Fatalf("expected zero QuestionRequestedEvent for exit_plan_mode, got %d", questionEvCount)
	}
}
