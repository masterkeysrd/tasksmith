package builtin

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/guard"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

type safeWriter struct {
	mu     sync.Mutex
	closed bool
	w      io.Writer
}

func (sw *safeWriter) Write(p []byte) (int, error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.closed || sw.w == nil {
		return len(p), nil
	}
	return sw.w.Write(p)
}

func (sw *safeWriter) Close() {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.closed = true
}

type bashTool struct {
	std     *std.Std
	sandbox sandbox.Sandbox
	guard   *shellguard.Guard
	tasks   tasks.Manager
}

func (t *bashTool) Prepare(_ context.Context, in api.BashInput) (*tools.Plan, error) {
	cmdStr := strings.TrimSpace(in.Command)
	if cmdStr == "" {
		return nil, fmt.Errorf("%w: command is required", std.ErrInvalidArgument)
	}

	summary := fmt.Sprintf("Run shell command: %s", cmdStr)
	if desc := strings.TrimSpace(in.Description); desc != "" {
		summary = desc
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
			Summary:  summary,
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
		Summary:  summary,
		ReadOnly: readOnly,
	}, nil
}

func (t *bashTool) Execute(ctx context.Context, in api.BashInput) (api.BashOutput, error) {
	wait := 60 * time.Second
	if in.RunInBackground {
		wait = 500 * time.Millisecond
	}
	if in.WaitMs > 0 {
		wait = time.Duration(in.WaitMs) * time.Millisecond
	}

	w := tools.NewSpillWriter(ctx)
	defer w.Close()

	sw := &safeWriter{w: w}
	defer sw.Close()

	title := strings.TrimSpace(in.Description)
	if title == "" {
		title = strings.TrimSpace(in.Command)
	}

	task := &tasks.Task{
		Kind:    "bash",
		Title:   title,
		Command: in.Command,
		Wait:    wait,
		Fn: func(taskCtx context.Context, out io.Writer, stdin io.Reader) (int, error) {
			mw := io.MultiWriter(out, sw)
			res, err := t.std.Bash(taskCtx, std.BashRequest{
				Command:     in.Command,
				Dir:         in.Dir,
				StdinReader: stdin,
				Output:      mw,
			})
			if err != nil {
				return -1, err
			}
			return res.ExitCode, nil
		},
	}

	err := t.tasks.Run(ctx, task)
	sw.Close()

	if err != nil && !task.Backgrounded() {
		if task.Status() == tasks.StatusFailed && task.ExitCode() >= 0 {
			return api.BashOutput{
				Output:     w.String(),
				ExitCode:   task.ExitCode(),
				DurationMs: task.Duration().Milliseconds(),
				Status:     string(task.Status()),
			}, nil
		}
		return api.BashOutput{}, err
	}

	if task.Backgrounded() {
		initial := strings.TrimSpace(w.String())
		bgNotice := fmt.Sprintf("<background_task task_id=%q status=%q log_path=%q>\nCommand is running in the background. You do not need to poll this task's status — the system will automatically notify and wake you up when it finishes execution. You can continue with other work, or stop calling tools to wait. Use the `tasks` tool to check intermediate logs, send input, or kill the task.\n</background_task>", task.ID, task.Status(), task.LogPath())
		outStr := bgNotice
		if initial != "" {
			outStr = initial + "\n\n" + bgNotice
		}
		return api.BashOutput{
			Output:     outStr,
			ExitCode:   0,
			DurationMs: task.Duration().Milliseconds(),
			TaskID:     task.ID,
			Status:     string(task.Status()),
		}, nil
	}

	return api.BashOutput{
		Output:     w.String(),
		ExitCode:   task.ExitCode(),
		DurationMs: task.Duration().Milliseconds(),
		Status:     string(task.Status()),
	}, nil
}

type tasksTool struct {
	tasks tasks.Manager
}

func (t *tasksTool) Prepare(_ context.Context, in api.TasksInput) (*tools.Plan, error) {
	switch in.Action {
	case "list":
		return &tools.Plan{
			ToolName: ToolTasks,
			Action:   tools.ActionRead,
			Safety:   tools.SafetySafe,
			Target:   "list",
			Summary:  "List background tasks",
			ReadOnly: true,
		}, nil
	case "status":
		taskID := strings.TrimSpace(in.TaskID)
		if taskID == "" {
			return nil, fmt.Errorf("%w: task_id is required for status", std.ErrInvalidArgument)
		}
		return &tools.Plan{
			ToolName: ToolTasks,
			Action:   tools.ActionRead,
			Safety:   tools.SafetySafe,
			Target:   taskID,
			Summary:  fmt.Sprintf("Check status of task %s", taskID),
			ReadOnly: true,
		}, nil
	case "kill":
		taskID := strings.TrimSpace(in.TaskID)
		if taskID == "" {
			return nil, fmt.Errorf("%w: task_id is required for kill", std.ErrInvalidArgument)
		}
		return &tools.Plan{
			ToolName: ToolTasks,
			Action:   tools.ActionExec,
			Safety:   tools.SafetySafe,
			Target:   fmt.Sprintf("kill %s", taskID),
			Summary:  fmt.Sprintf("Kill background task %s", taskID),
			ReadOnly: false,
		}, nil
	case "send_input":
		taskID := strings.TrimSpace(in.TaskID)
		if taskID == "" {
			return nil, fmt.Errorf("%w: task_id is required for send_input", std.ErrInvalidArgument)
		}
		if in.Input == "" {
			return nil, fmt.Errorf("%w: input is required for send_input", std.ErrInvalidArgument)
		}
		return &tools.Plan{
			ToolName: ToolTasks,
			Action:   tools.ActionExec,
			Safety:   tools.SafetySafe,
			Target:   fmt.Sprintf("send_input %s", taskID),
			Summary:  fmt.Sprintf("Send input to background task %s", taskID),
			ReadOnly: false,
		}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported action %q", std.ErrInvalidArgument, in.Action)
	}
}

func (t *tasksTool) Execute(ctx context.Context, in api.TasksInput) (api.TasksOutput, error) {
	switch in.Action {
	case "list":
		snaps := t.tasks.List()
		items := make([]api.TasksTask, 0, len(snaps))
		for _, snap := range snaps {
			items = append(items, api.TasksTask{
				ID:         snap.ID,
				Kind:       snap.Kind,
				Title:      snap.Title,
				Command:    snap.Command,
				Status:     string(snap.Status),
				ExitCode:   snap.ExitCode,
				Error:      snap.Error,
				LogPath:    snap.LogPath,
				DurationMs: snap.DurationMs,
			})
		}
		return api.TasksOutput{
			Tasks:   items,
			Message: fmt.Sprintf("%d background task(s).", len(items)),
		}, nil
	case "status":
		taskID := strings.TrimSpace(in.TaskID)
		task, ok := t.tasks.Get(taskID)
		if !ok {
			return api.TasksOutput{}, tasks.ErrTaskNotFound
		}
		snap := task.Snapshot()
		tail, _ := task.Tail(in.Limit)
		tail = tools.Spill(ctx, tail)
		return api.TasksOutput{
			Status:   string(snap.Status),
			ExitCode: snap.ExitCode,
			LogPath:  snap.LogPath,
			Output:   tail,
			Message:  fmt.Sprintf("Task %s is %s.", snap.ID, snap.Status),
		}, nil
	case "kill":
		taskID := strings.TrimSpace(in.TaskID)
		if err := t.tasks.Kill(taskID); err != nil {
			return api.TasksOutput{}, err
		}
		return api.TasksOutput{
			Status:  string(tasks.StatusCanceled),
			Message: fmt.Sprintf("Killed task %s.", taskID),
		}, nil
	case "send_input":
		taskID := strings.TrimSpace(in.TaskID)
		if err := t.tasks.WriteInput(taskID, in.Input); err != nil {
			return api.TasksOutput{}, err
		}
		return api.TasksOutput{
			Status:  string(tasks.StatusRunning),
			Message: fmt.Sprintf("Sent input to task %s.", taskID),
		}, nil
	default:
		return api.TasksOutput{}, fmt.Errorf("%w: unsupported action %q", std.ErrInvalidArgument, in.Action)
	}
}
