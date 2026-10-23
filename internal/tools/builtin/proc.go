package builtin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/guard"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/std"
	"github.com/masterkeysrd/tasksmith/internal/tools"
)

type bashTool struct {
	std     *std.Std
	sandbox sandbox.Sandbox
	guard   *shellguard.Guard
}

func (t *bashTool) Prepare(_ context.Context, in api.BashInput) (*tools.Plan, error) {
	cmdStr := strings.TrimSpace(in.Command)
	if cmdStr == "" {
		return nil, fmt.Errorf("%w: command is required", std.ErrInvalidArgument)
	}

	analysisCmd := cmdStr
	if strings.TrimSpace(in.Dir) != "" {
		relDir, err := t.sandbox.FS().ResolvePath(in.Dir)
		if err != nil {
			return nil, err
		}
		analysisCmd = fmt.Sprintf("cd %q && %s", relDir, cmdStr)
	}

	ops, analyzeErr := t.guard.Analyze(analysisCmd)
	if analyzeErr != nil {
		return &tools.Plan{
			ToolName: ToolBash,
			Action:   tools.ActionExec,
			Safety:   tools.SafetyUnsafe,
			Target:   cmdStr,
			Summary:  fmt.Sprintf("Run shell command: %s", cmdStr),
			ReadOnly: false,
		}, nil
	}

	worstAction := guard.ActionRead
	worstSafety := guard.SafetySafe
	for _, op := range ops {
		if op.Action.Severity() > worstAction.Severity() {
			worstAction = op.Action
		}
		if op.Safety.Severity() > worstSafety.Severity() {
			worstSafety = op.Safety
		}
	}
	if worstAction == guard.ActionUnknown {
		worstAction = guard.ActionExec
	}

	readOnly := worstAction == tools.ActionRead && worstSafety == tools.SafetySafe

	return &tools.Plan{
		ToolName: ToolBash,
		Action:   worstAction,
		Safety:   worstSafety,
		Target:   cmdStr,
		Summary:  fmt.Sprintf("Run shell command: %s", cmdStr),
		ReadOnly: readOnly,
	}, nil
}

func (t *bashTool) Execute(ctx context.Context, in api.BashInput) (api.BashOutput, error) {
	var timeout time.Duration
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	res, err := t.std.Bash(ctx, std.BashRequest{
		Command: in.Command,
		Dir:     in.Dir,
		Env:     in.Env,
		Stdin:   in.Stdin,
		Timeout: timeout,
	})
	if err != nil {
		return api.BashOutput{}, err
	}
	return api.BashOutput{
		Stdout:     res.Stdout,
		Stderr:     res.Stderr,
		ExitCode:   res.ExitCode,
		DurationMs: res.Duration.Milliseconds(),
	}, nil
}
