package tools

import (
	"context"
	"fmt"
	"os"
	"time"
)

// Tasks manages background tasks (listing, checking status/logs, terminating).
func (h *ToolHandlers) Tasks(ctx context.Context, in TasksArgs) (TasksOutput, error) {
	if h.TaskManager == nil {
		return TasksOutput{Message: "Task manager is not configured."}, nil
	}

	switch in.Action {
	case "list":
		tasks := h.TaskManager.ListTasks(h.SessionID)
		var items []TasksOutputTasksItem
		for _, t := range tasks {
			var finishedAtStr string
			if !t.FinishedAt.IsZero() {
				finishedAtStr = t.FinishedAt.Format(time.RFC3339)
			}
			items = append(items, TasksOutputTasksItem{
				TaskId:     t.ID,
				Name:       t.Name,
				Type:       t.Type,
				Status:     string(t.Status),
				ExitCode:   t.ExitCode,
				StartedAt:  t.StartedAt.Format(time.RFC3339),
				FinishedAt: finishedAtStr,
				Error:      t.Error,
			})
		}
		return TasksOutput{
			Tasks:   items,
			Message: fmt.Sprintf("Successfully retrieved %d task(s).", len(items)),
		}, nil

	case "status":
		if in.TaskId == "" {
			return TasksOutput{Message: "Task ID is required for status action."}, nil
		}
		t, ok := h.TaskManager.GetTask(in.TaskId)
		if !ok || t.SessionID != h.SessionID {
			return TasksOutput{Message: fmt.Sprintf("Task %q not found in this session.", in.TaskId)}, nil
		}

		limit := 100
		if in.Limit > 0 {
			limit = in.Limit
		}

		var stdoutTail string
		var err error
		stdoutInfo, statErr := os.Stat(t.StdoutPath)
		if statErr == nil && stdoutInfo.Size() > 100000 {
			toolCallID, _ := ctx.Value("tool_call_id").(string)
			if toolCallID == "" {
				toolCallID = in.TaskId
			}
			stdoutTail, err = h.saveAndTruncate(ctx, t.StdoutPath, "stdout", toolCallID)
			if err != nil {
				stdoutTail = fmt.Sprintf("Failed to save and truncate stdout log: %v", err)
			}
		} else {
			stdoutTail, err = h.TaskManager.ReadLog(in.TaskId, false, limit)
			if err != nil {
				stdoutTail = fmt.Sprintf("Failed to read stdout log: %v", err)
			}
		}

		var stderrTail string
		stderrInfo, statErr := os.Stat(t.StderrPath)
		if statErr == nil && stderrInfo.Size() > 100000 {
			toolCallID, _ := ctx.Value("tool_call_id").(string)
			if toolCallID == "" {
				toolCallID = in.TaskId
			}
			stderrTail, err = h.saveAndTruncate(ctx, t.StderrPath, "stderr", toolCallID)
			if err != nil {
				stderrTail = fmt.Sprintf("Failed to save and truncate stderr log: %v", err)
			}
		} else {
			stderrTail, err = h.TaskManager.ReadLog(in.TaskId, true, limit)
			if err != nil {
				stderrTail = fmt.Sprintf("Failed to read stderr log: %v", err)
			}
		}

		return TasksOutput{
			Status:     string(t.Status),
			ExitCode:   t.ExitCode,
			StdoutTail: stdoutTail,
			StderrTail: stderrTail,
			Message:    fmt.Sprintf("Task %s is %s.", in.TaskId, t.Status),
		}, nil

	case "kill":
		if in.TaskId == "" {
			return TasksOutput{Message: "Task ID is required for kill action."}, nil
		}
		t, ok := h.TaskManager.GetTask(in.TaskId)
		if !ok || t.SessionID != h.SessionID {
			return TasksOutput{Message: fmt.Sprintf("Task %q not found in this session.", in.TaskId)}, nil
		}

		if err := h.TaskManager.KillTask(in.TaskId); err != nil {
			return TasksOutput{Message: fmt.Sprintf("Failed to kill task: %v", err)}, nil
		}

		return TasksOutput{
			Status:  "killed",
			Message: fmt.Sprintf("Successfully sent kill signal to task %s.", in.TaskId),
		}, nil

	default:
		return TasksOutput{Message: fmt.Sprintf("Unsupported action %q.", in.Action)}, nil
	}
}
