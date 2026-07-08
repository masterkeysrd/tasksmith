package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
)

const (
	defaultAgentWaitMs = 10000
)

// InvokeAgent executes a subagent and returns a ToolStream.
func (h *ToolHandlers) InvokeAgent(ctx context.Context, in InvokeAgentArgs) (tool.ToolStream, error) {
	toolCallID, _ := ctx.Value("tool_call_id").(string)

	return func(yield func(message.ToolChunk, error) bool) {
		if h.TaskManager == nil {
			outObj := InvokeAgentOutput{
				Status: "failed",
				Error:  "Task manager is not configured.",
			}
			yield(message.ToolChunk{
				BaseChunk:         message.BaseChunk{ID: toolCallID},
				IsError:           true,
				StructuredContent: outObj,
			}, nil)
			return
		}

		waitMs := in.WaitMs
		if waitMs <= 0 {
			waitMs = defaultAgentWaitMs
		}

		mode := in.Mode
		if mode == "" {
			mode = "transient"
		}

		runner := &AgentRunner{
			AgentRef: in.AgentRef,
			Task:     in.Task,
			Mode:     mode,
			Handlers: h,
		}

		task, err := h.TaskManager.Submit(ctx, SubmitOptions{
			SessionID: h.SessionID,
			TaskType:  "agent",
			Name:      fmt.Sprintf("agent:%s", in.AgentRef),
			Runner:    runner,
			WaitMs:    waitMs,
		})
		if err != nil {
			outObj := InvokeAgentOutput{
				Status: "failed",
				Error:  err.Error(),
			}
			yield(message.ToolChunk{
				BaseChunk:         message.BaseChunk{ID: toolCallID},
				IsError:           true,
				StructuredContent: outObj,
			}, nil)
			return
		}

		if toolCallID == "" {
			toolCallID = task.ID
		}
		runner.TaskID = task.ID

		ticker := time.NewTicker(logStreamPollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.TaskManager.mu.RLock()
				status := task.Status
				isBg := task.IsBackground
				h.TaskManager.mu.RUnlock()

				if status != StatusRunning {
					// Retrieve the subagent's final output from the runner
					// We'll set this field on the runner during execution.
					finalOutput := runner.Result()

					var errStr string
					if status == StatusFailed {
						errStr = task.Error
						if stdoutLog, err := h.TaskManager.ReadLog(task.ID, false, 100); err == nil && stdoutLog != "" {
							errStr += fmt.Sprintf("\n\n[Subagent Stdout Log]\n%s", stdoutLog)
						}
						if stderrLog, err := h.TaskManager.ReadLog(task.ID, true, 100); err == nil && stderrLog != "" {
							errStr += fmt.Sprintf("\n\n[Subagent Stderr Log]\n%s", stderrLog)
						}
					}

					outObj := InvokeAgentOutput{
						Status: string(status),
						Output: finalOutput,
						Error:  errStr,
					}
					yield(message.ToolChunk{
						BaseChunk:         message.BaseChunk{ID: toolCallID},
						StructuredContent: outObj,
					}, nil)
					return
				}

				if isBg {
					hintText := fmt.Sprintf("\n<background_task task_id=\"%s\" status=\"running\">\nYou do not need to poll this task's status. The system will automatically notify you when it finishes. You can continue with other work, or stop calling tools to wait.\n</background_task>\n", task.ID)
					outObj := InvokeAgentOutput{
						TaskId: task.ID,
						Status: "running",
					}
					yield(message.ToolChunk{
						BaseChunk: message.BaseChunk{ID: toolCallID},
						Content: message.Content{
							&message.TextBlock{Text: hintText},
						},
						StructuredContent: outObj,
					}, nil)
					return
				}
			}
		}
	}, nil
}

// TextContent formats the InvokeAgentOutput for LLM presentation.
func (o InvokeAgentOutput) TextContent() string {
	var sb strings.Builder
	if o.Status == "running" {
		fmt.Fprintf(&sb, "<background_task task_id=\"%s\" status=\"running\">\n", o.TaskId)
		sb.WriteString("You do not need to poll this task's status. The system will automatically notify you when it finishes. You can continue with other work, or stop calling tools to wait.\n")
		sb.WriteString("</background_task>\n")
		return sb.String()
	}

	if o.Status == "completed" {
		sb.WriteString("Subagent completed successfully.\n")
	} else if o.Status == "failed" {
		sb.WriteString("Subagent failed.\n")
	}

	if o.Error != "" {
		fmt.Fprintf(&sb, "Error: %s\n", o.Error)
	}
	if o.Output != "" {
		sb.WriteString("\n[Response]\n")
		sb.WriteString(o.Output)
	}
	return sb.String()
}
