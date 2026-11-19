package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

type fakePlanController struct {
	plansDir string
	mode     api.PermissionMode
	exits    []struct {
		path string
		mode api.PermissionMode
	}
}

func (f *fakePlanController) PlansDir() string { return f.plansDir }
func (f *fakePlanController) CurrentMode() api.PermissionMode {
	if f.mode == "" {
		return api.ModeDefault
	}
	return f.mode
}
func (f *fakePlanController) EnterPlanMode(context.Context) (bool, string, error) {
	if f.mode == api.ModePlan {
		return true, f.plansDir, nil
	}
	f.mode = api.ModePlan
	return false, f.plansDir, nil
}
func (f *fakePlanController) ExitPlanMode(_ context.Context, planPath string, nextMode api.PermissionMode) (api.PermissionMode, error) {
	f.mode = nextMode
	f.exits = append(f.exits, struct {
		path string
		mode api.PermissionMode
	}{path: planPath, mode: nextMode})
	return nextMode, nil
}

func TestEnterAndExitPlanModeTools(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	plansDir := filepath.Join(t.TempDir(), "plans")
	outsideFile := filepath.Join(t.TempDir(), "outside-plan.md")
	if err := os.WriteFile(outsideFile, []byte("# Outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(plansDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPlan := filepath.Join(plansDir, "session-plan.md")
	if err := os.WriteFile(sessionPlan, []byte("# Session Plan\n\nStep 1.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wsPlan := filepath.Join(workDir, "PLAN.md")
	if err := os.WriteFile(wsPlan, []byte("# Workspace Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sbx, err := sandbox.New(sandbox.Config{WorkDir: workDir, Mode: sandbox.ModeLocal})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sbx.Close() }()

	guardInst := shellguard.New(shellguard.Config{
		WorkspaceDir: workDir,
		WriteDirs:    []string{plansDir},
	})
	ctrl := &fakePlanController{plansDir: plansDir, mode: api.ModeDefault}

	enter := &enterPlanModeTool{plans: ctrl}
	plan, err := enter.Prepare(ctx, api.EnterPlanModeInput{})
	if err != nil || !plan.ReadOnly || plan.Safety != api.SafetySafe {
		t.Fatalf("enter.Prepare = %+v, %v", plan, err)
	}
	out1, err := enter.Execute(ctx, api.EnterPlanModeInput{})
	if err != nil || out1.AlreadyActive || out1.Mode != string(api.ModePlan) || out1.PlansDir != plansDir {
		t.Fatalf("enter.Execute (first) = %+v, %v", out1, err)
	}
	out2, err := enter.Execute(ctx, api.EnterPlanModeInput{})
	if err != nil || !out2.AlreadyActive {
		t.Fatalf("enter.Execute (idempotent) = %+v, %v", out2, err)
	}

	exit := &exitPlanModeTool{sandbox: sbx, guard: guardInst, plans: ctrl}

	// Validation errors
	if _, err := exit.Prepare(ctx, api.ExitPlanModeInput{Path: ""}); err == nil {
		t.Fatal("expected error for empty path")
	}
	if _, err := exit.Prepare(ctx, api.ExitPlanModeInput{Path: "missing.md"}); err == nil {
		t.Fatal("expected error for missing file")
	}
	if _, err := exit.Prepare(ctx, api.ExitPlanModeInput{Path: "."}); err == nil {
		t.Fatal("expected error for directory path")
	}
	if _, err := exit.Prepare(ctx, api.ExitPlanModeInput{Path: outsideFile}); err == nil {
		t.Fatal("expected error for path outside workspace and plansDir")
	}

	// Valid workspace plan path
	wsPrep, err := exit.Prepare(ctx, api.ExitPlanModeInput{Path: "PLAN.md"})
	if err != nil || wsPrep.PlanReview == nil || wsPrep.PlanReview.Path != "PLAN.md" || !strings.Contains(wsPrep.PlanReview.Content, "# Workspace Plan") {
		t.Fatalf("wsPrep = %+v, %v", wsPrep, err)
	}

	// Valid session plansDir path
	sessPrep, err := exit.Prepare(ctx, api.ExitPlanModeInput{Path: sessionPlan})
	if err != nil || sessPrep.PlanReview == nil || sessPrep.PlanReview.Path != filepath.ToSlash(sessionPlan) {
		t.Fatalf("sessPrep = %+v, %v", sessPrep, err)
	}

	// Keep planning without approval
	keepOut, err := exit.Execute(tools.WithPlanReviewOutcome(ctx, tools.PlanReviewOutcome{
		Approved: false,
		Feedback: "Add rollback plan",
	}), api.ExitPlanModeInput{Path: sessionPlan})
	if err != nil || keepOut.Approved || keepOut.Mode != string(api.ModePlan) || keepOut.Feedback != "Add rollback plan" {
		t.Fatalf("keepOut = %+v, %v", keepOut, err)
	}

	// Approval switches mode
	appOut, err := exit.Execute(tools.WithPlanReviewOutcome(ctx, tools.PlanReviewOutcome{
		Approved: true,
		Mode:     api.ModeAcceptEdits,
	}), api.ExitPlanModeInput{Path: sessionPlan})
	if err != nil || !appOut.Approved || appOut.Mode != string(api.ModeAcceptEdits) || ctrl.mode != api.ModeAcceptEdits {
		t.Fatalf("appOut = %+v, %v", appOut, err)
	}
}
