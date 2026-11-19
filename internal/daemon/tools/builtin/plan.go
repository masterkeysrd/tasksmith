package builtin

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

const maxPlanPreviewBytes = 128 * 1024

// PlanController coordinates session-level plan mode transitions for the
// enter_plan_mode and exit_plan_mode built-in tools.
type PlanController interface {
	PlansDir() string
	CurrentMode() api.PermissionMode
	EnterPlanMode(ctx context.Context) (alreadyActive bool, plansDir string, err error)
	ExitPlanMode(ctx context.Context, planPath string, nextMode api.PermissionMode) (api.PermissionMode, error)
}

// PlanModeInstructions returns the behavioral contract guidance for plan mode,
// parameterized by the session's safe plans directory.
func PlanModeInstructions(plansDir string) string {
	var sb strings.Builder
	sb.WriteString("Plan mode is active. Investigate the codebase using read-only tools and clarify ambiguities with `ask_question` without modifying project code or running mutating commands.")
	if strings.TrimSpace(plansDir) != "" {
		fmt.Fprintf(&sb, " Ask the user via `ask_question` whether to save the plan document in the session's safe plans directory (%s, where writes, edits, and removals are auto-allowed) or in the current workspace directory (which uses normal permission prompts).", plansDir)
	}
	sb.WriteString(" Once the plan document is written, call `exit_plan_mode` with its `path` for user review and approval.")
	return sb.String()
}

type enterPlanModeTool struct {
	plans PlanController
}

var _ tools.Handler[api.EnterPlanModeInput, api.EnterPlanModeOutput] = (*enterPlanModeTool)(nil)

func (t *enterPlanModeTool) Prepare(_ context.Context, _ api.EnterPlanModeInput) (*tools.Plan, error) {
	return &tools.Plan{
		ToolName: ToolEnterPlanMode,
		Action:   api.ActionRead,
		Safety:   api.SafetySafe,
		Target:   string(api.ModePlan),
		Summary:  "Enter plan mode",
		ReadOnly: true,
	}, nil
}

func (t *enterPlanModeTool) Execute(ctx context.Context, _ api.EnterPlanModeInput) (api.EnterPlanModeOutput, error) {
	alreadyActive, plansDir, err := t.plans.EnterPlanMode(ctx)
	if err != nil {
		return api.EnterPlanModeOutput{}, err
	}
	return api.EnterPlanModeOutput{
		Mode:          string(api.ModePlan),
		AlreadyActive: alreadyActive,
		PlansDir:      plansDir,
		Instructions:  PlanModeInstructions(plansDir),
	}, nil
}

type exitPlanModeTool struct {
	sandbox sandbox.Sandbox
	guard   *shellguard.Guard
	plans   PlanController
}

var _ tools.Handler[api.ExitPlanModeInput, api.ExitPlanModeOutput] = (*exitPlanModeTool)(nil)

func (t *exitPlanModeTool) Prepare(ctx context.Context, in api.ExitPlanModeInput) (*tools.Plan, error) {
	if strings.TrimSpace(in.Path) == "" {
		return nil, fmt.Errorf("%w: path is required", std.ErrInvalidArgument)
	}
	target, safety, err := resolveReadTarget(t.sandbox, t.guard, in.Path)
	if err != nil {
		return nil, err
	}
	if safety != api.SafetySafe {
		return nil, fmt.Errorf("%w: plan path %q must resolve inside the workspace or session plans directory", std.ErrInvalidArgument, in.Path)
	}
	info, err := t.sandbox.FS().Stat(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("%w: plan file %q does not exist: %v", std.ErrInvalidArgument, target, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%w: plan path %q must be a file, not a directory", std.ErrInvalidArgument, target)
	}
	data, err := t.sandbox.FS().ReadFile(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("read plan file %q: %w", target, err)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%w: plan file %q must be UTF-8 text", std.ErrInvalidArgument, target)
	}
	content := string(data)
	if len(content) > maxPlanPreviewBytes {
		content = content[:maxPlanPreviewBytes] + "\n\n... [plan preview truncated]"
	}
	return &tools.Plan{
		ToolName: ToolExitPlanMode,
		Action:   api.ActionRead,
		Safety:   api.SafetySafe,
		Target:   target,
		Paths:    []string{target},
		Summary:  fmt.Sprintf("Submit plan: %s", target),
		ReadOnly: true,
		PlanReview: &api.PlanReviewRequest{
			Path:    target,
			Content: content,
		},
	}, nil
}

func (t *exitPlanModeTool) Execute(ctx context.Context, in api.ExitPlanModeInput) (api.ExitPlanModeOutput, error) {
	target, _, err := resolveReadTarget(t.sandbox, t.guard, in.Path)
	if err != nil || target == "" {
		target = strings.TrimSpace(in.Path)
	}
	outcome, ok := tools.PlanReviewOutcomeFromContext(ctx)
	if !ok {
		return api.ExitPlanModeOutput{
			Approved: false,
			Mode:     string(t.plans.CurrentMode()),
			PlanPath: target,
			Feedback: "plan review requires user approval",
		}, nil
	}
	if !outcome.Approved {
		return api.ExitPlanModeOutput{
			Approved: false,
			Mode:     string(t.plans.CurrentMode()),
			PlanPath: target,
			Feedback: outcome.Feedback,
		}, nil
	}
	nextMode, err := t.plans.ExitPlanMode(ctx, target, outcome.Mode)
	if err != nil {
		return api.ExitPlanModeOutput{}, err
	}
	return api.ExitPlanModeOutput{
		Approved: true,
		Mode:     string(nextMode),
		PlanPath: target,
	}, nil
}
