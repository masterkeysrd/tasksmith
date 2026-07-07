package tools

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	defaultCompletedListLimit = 5
	defaultLogLimitLines      = 100
)

// Validate checks the TasksArgs based on the action specified.
func (in TasksArgs) Validate() error {
	switch in.Action {
	case "list":
		return nil
	case "send_input":
		if in.TaskId == "" {
			return fmt.Errorf("taskId is required for send_input action")
		}
		if in.Input == "" {
			return fmt.Errorf("input is required for send_input action")
		}
		return nil
	case "status":
		if in.TaskId == "" {
			return fmt.Errorf("taskId is required for status action")
		}
		return nil
	case "kill":
		if in.TaskId == "" {
			return fmt.Errorf("taskId is required for kill action")
		}
		return nil
	default:
		return fmt.Errorf("unsupported action %q: must be one of list, send_input, status, kill", in.Action)
	}
}

// Tasks manages background tasks (listing, checking status/logs, terminating).
func (h *ToolHandlers) Tasks(ctx context.Context, in TasksArgs) (TasksOutput, error) {
	if err := in.Validate(); err != nil {
		return TasksOutput{}, err
	}
	if h.TaskManager == nil {
		return TasksOutput{Message: "Task manager is not configured."}, nil
	}

	switch in.Action {
	case "list":
		tasks := h.TaskManager.ListTasks(h.SessionID)
		var items []TasksOutputTasksItem

		// Sort tasks by startedAt descending
		sort.Slice(tasks, func(i, j int) bool {
			return tasks[i].StartedAt.After(tasks[j].StartedAt)
		})

		completedCount := 0
		for _, t := range tasks {
			isRunning := t.Status == StatusRunning
			if !in.IncludeCompleted && !isRunning {
				if completedCount >= defaultCompletedListLimit {
					continue
				}
				completedCount++
			}

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
				Details:    t.Details,
			})
		}
		return TasksOutput{
			Tasks:   items,
			Message: fmt.Sprintf("Successfully retrieved %d task(s).", len(items)),
		}, nil

	case "send_input":
		if in.TaskId == "" {
			return TasksOutput{Message: "Task ID is required for send_input action."}, nil
		}
		if in.Input == "" {
			return TasksOutput{Message: "Input is required for send_input action."}, nil
		}
		t, ok := h.TaskManager.GetTask(in.TaskId)
		if !ok || t.SessionID != h.SessionID {
			return TasksOutput{Message: fmt.Sprintf("Task %q not found in this session.", in.TaskId)}, nil
		}

		if err := h.TaskManager.WriteStdin(in.TaskId, in.Input); err != nil {
			return TasksOutput{Message: fmt.Sprintf("Failed to send input to task: %v", err)}, nil
		}

		return TasksOutput{
			Status:  string(t.Status),
			Message: fmt.Sprintf("Successfully sent input to task %s.", in.TaskId),
		}, nil

	case "status":
		if in.TaskId == "" {
			return TasksOutput{Message: "Task ID is required for status action."}, nil
		}
		t, ok := h.TaskManager.GetTask(in.TaskId)
		if !ok || t.SessionID != h.SessionID {
			return TasksOutput{Message: fmt.Sprintf("Task %q not found in this session.", in.TaskId)}, nil
		}

		limit := defaultLogLimitLines
		if in.Limit > 0 {
			limit = in.Limit
		}

		var stdoutTail string
		var err error
		stdoutInfo, statErr := os.Stat(t.StdoutPath)
		if statErr == nil && stdoutInfo.Size() > logSizeThresholdBytes {
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
		if statErr == nil && stderrInfo.Size() > logSizeThresholdBytes {
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

// TextContent implements tool.TextContentProvider so loom renders a human-readable summary
// of background task lists or task status checks instead of raw JSON.
func (o TasksOutput) TextContent() string {
	var sb strings.Builder

	if o.Message != "" {
		sb.WriteString(o.Message)
		sb.WriteString("\n")
	}

	// Case 1: Status query result
	if o.Status != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "Task Status: %s\n", o.Status)
		if o.Status == "completed" || o.Status == "failed" {
			fmt.Fprintf(&sb, "Exit Code: %d\n", o.ExitCode)
		}
		if o.StdoutTail != "" {
			sb.WriteString("\n[stdout tail]\n")
			sb.WriteString(o.StdoutTail)
			sb.WriteString("\n")
		}
		if o.StderrTail != "" {
			sb.WriteString("\n[stderr tail]\n")
			sb.WriteString(o.StderrTail)
			sb.WriteString("\n")
		}
		return sb.String()
	}

	// Case 2: List query result
	if len(o.Tasks) > 0 {
		sb.WriteString("\nBackground Tasks:\n")
		for _, t := range o.Tasks {
			fmt.Fprintf(&sb, "- ID: %s | Name: %q | Type: %s | Status: %s", t.TaskId, t.Name, t.Type, t.Status)
			if t.Details != "" {
				fmt.Fprintf(&sb, " (%s)", t.Details)
			}
			if t.Status == "completed" || t.Status == "failed" {
				fmt.Fprintf(&sb, " | Exit Code: %d", t.ExitCode)
			}
			if t.FinishedAt != "" {
				fmt.Fprintf(&sb, " | Finished: %s", t.FinishedAt)
			} else {
				fmt.Fprintf(&sb, " | Started: %s", t.StartedAt)
			}
			if t.Error != "" {
				fmt.Fprintf(&sb, " | Error: %s", t.Error)
			}
			sb.WriteString("\n")
		}
		return sb.String()
	}

	return sb.String()
}
