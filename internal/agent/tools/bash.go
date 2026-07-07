package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/process"
)

// BashRunner implements TaskRunner for OS shell commands.
type BashRunner struct {
	Command string
	CWD     string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	mu      sync.Mutex
}

func (br *BashRunner) Start(ctx context.Context, stdout io.Writer, stderr io.Writer) error {
	br.mu.Lock()
	cmd := exec.CommandContext(ctx, "bash", "-c", br.Command)
	cmd.Dir = br.CWD
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		br.mu.Unlock()
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	br.stdin = stdin

	// Set process group so we can terminate all subprocesses
	process.Prepare(cmd)

	// Configure graceful termination before SIGKILL
	cmd.Cancel = func() error {
		return process.Kill(cmd)
	}
	cmd.WaitDelay = defaultWaitDelay
	br.cmd = cmd
	br.mu.Unlock()

	return cmd.Run()
}

func (br *BashRunner) WriteStdin(data string) error {
	br.mu.Lock()
	defer br.mu.Unlock()
	if br.stdin == nil {
		return fmt.Errorf("stdin pipe not available")
	}
	_, err := io.WriteString(br.stdin, data)
	return err
}

func (br *BashRunner) Stop() error {
	br.mu.Lock()
	defer br.mu.Unlock()
	if br.stdin != nil {
		br.stdin.Close()
	}
	if br.cmd != nil {
		return process.Kill(br.cmd)
	}
	return nil
}

// State implements the StateReporter interface to report dynamic task details.
func (br *BashRunner) State() string {
	br.mu.Lock()
	cmd := br.cmd
	br.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return ""
	}

	ports, _ := process.FindPorts(cmd.Process.Pid)
	if len(ports) > 0 {
		var pstrs []string
		for _, p := range ports {
			pstrs = append(pstrs, fmt.Sprintf(":%d", p))
		}
		return strings.Join(pstrs, ", ")
	}
	return ""
}

const (
	logSizeThresholdBytes  = 100000 // 100KB threshold
	logPreviewLimitBytes   = 8000   // 8KB preview limit
	bgLogPreviewLimitBytes = 5000   // 5KB preview limit for background task logs

	defaultWaitDelay      = 5 * time.Second
	defaultBashWaitMs     = 10000
	logStreamPollInterval = 50 * time.Millisecond
)

func readAndTruncateBgLog(logPath string) string {
	if logPath == "" {
		return ""
	}
	f, err := os.Open(logPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ""
	}
	if info.Size() == 0 {
		return ""
	}

	if info.Size() <= bgLogPreviewLimitBytes {
		data, err := os.ReadFile(logPath)
		if err != nil {
			return ""
		}
		return applyLogBudget(string(data), 0, bgLogPreviewLimitBytes)
	}

	// Read last bgLogPreviewLimitBytes bytes
	offset := info.Size() - bgLogPreviewLimitBytes
	_, err = f.Seek(offset, io.SeekStart)
	if err != nil {
		return ""
	}

	buf := make([]byte, bgLogPreviewLimitBytes)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return ""
	}

	return applyLogBudget(string(buf[:n]), 0, bgLogPreviewLimitBytes)
}

// saveAndTruncate checks the size of the log file at logPath. If it exceeds logSizeThresholdBytes,
// it saves the full output in h.Storage (if available) under a name containing toolCallID and suffix,
// reads a truncated tail preview of logPreviewLimitBytes bytes, and appends a warning note.
// Otherwise, it returns the full file content.
func (h *ToolHandlers) saveAndTruncate(ctx context.Context, logPath string, suffix string, toolCallID string) (string, error) {
	fileInfo, err := os.Stat(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // Log file does not exist yet (e.g. no output produced)
		}
		return "", err
	}

	if fileInfo.Size() <= logSizeThresholdBytes {
		data, err := os.ReadFile(logPath)
		if err != nil {
			return "", err
		}
		return applyLogBudget(string(data), 0, logPreviewLimitBytes), nil
	}

	// Exceeds threshold. Save full file to FileStorage
	logFile, err := os.Open(logPath)
	if err != nil {
		return "", err
	}
	defer logFile.Close()

	var savedPath string
	if h.Storage != nil {
		relativePath := filepath.Join("outputs", fmt.Sprintf("%s_%s.txt", toolCallID, suffix))
		var err error
		savedPath, err = h.Storage.Save(ctx, relativePath, logFile)
		if err != nil {
			log.Error("Failed to save large output to storage", log.Err(err))
		}
	}

	// Read last preview limit characters from logPath
	offset := fileInfo.Size() - logPreviewLimitBytes
	_, _ = logFile.Seek(offset, io.SeekStart)
	buf := make([]byte, logPreviewLimitBytes)
	n, err := logFile.Read(buf)
	if err != nil && err != io.EOF {
		return "", err
	}

	truncated := applyLogBudget(string(buf[:n]), 0, logPreviewLimitBytes)
	note := fmt.Sprintf("\n\n[SYSTEM NOTE: The %s output was too long and was truncated. The complete output is saved at: %s. You can view the full file using 'view' or search it using 'grep'.]", suffix, savedPath)
	return truncated + note, nil
}

// Bash executes a bash command and returns a ToolStream.
func (h *ToolHandlers) Bash(ctx context.Context, in BashArgs) (tool.ToolStream, error) {
	toolCallID, _ := ctx.Value("tool_call_id").(string)

	return func(yield func(message.ToolChunk, error) bool) {
		// If task manager is nil, fallback to synchronous one-shot combined execution
		if h.TaskManager == nil {
			cmd := exec.CommandContext(ctx, "bash", "-c", in.Command)
			cmd.Dir = h.CWD
			process.Prepare(cmd)
			cmd.Cancel = func() error {
				return process.Kill(cmd)
			}
			cmd.WaitDelay = defaultWaitDelay
			out, err := cmd.CombinedOutput()
			var exitCode int
			var status = "completed"
			var stderrMsg string
			if err != nil {
				exitCode = 1
				status = "failed"
				stderrMsg = err.Error()
			}
			outObj := BashOutput{
				ExitCode: exitCode,
				Stdout:   string(out),
				Stderr:   stderrMsg,
				Status:   status,
			}
			chunk := message.ToolChunk{
				BaseChunk: message.BaseChunk{ID: toolCallID},
				Content: message.Content{
					&message.TextBlock{Text: string(out)},
				},
				StructuredContent: outObj,
			}
			if err != nil {
				chunk.IsError = true
			}
			yield(chunk, nil)
			return
		}

		cmdStr := strings.TrimSpace(in.Command)
		// If command ends with a single '&' (and not '&&'), strip it.
		// The TaskManager handles background execution automatically.
		if strings.HasSuffix(cmdStr, "&") && !strings.HasSuffix(cmdStr, "&&") {
			cmdStr = strings.TrimSuffix(cmdStr, "&")
			cmdStr = strings.TrimSpace(cmdStr)
		}

		runner := &BashRunner{
			Command: cmdStr,
			CWD:     h.CWD,
		}

		waitMs := in.WaitMs
		if waitMs <= 0 {
			waitMs = defaultBashWaitMs
		}

		task, err := h.TaskManager.Submit(ctx, SubmitOptions{
			SessionID:  h.SessionID,
			TaskType:   "bash",
			Name:       in.Description,
			Runner:     runner,
			WaitMs:     waitMs,
			TimeoutSec: in.Timeout,
		})
		if err != nil {
			outObj := BashOutput{Status: "failed", Message: err.Error()}
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

		// Open stdout and stderr logs for active streaming
		var stdoutOffset int64
		var stderrOffset int64

		readNewLogs := func() (string, string, error) {
			var newStdout, newStderr string

			// Read stdout
			if task.StdoutPath != "" {
				f, err := os.Open(task.StdoutPath)
				if err == nil {
					defer f.Close()
					info, err := f.Stat()
					if err == nil && info.Size() > stdoutOffset {
						_, _ = f.Seek(stdoutOffset, io.SeekStart)
						buf := make([]byte, info.Size()-stdoutOffset)
						n, err := f.Read(buf)
						if err == nil || err == io.EOF {
							stdoutOffset += int64(n)
							newStdout = string(buf[:n])
						}
					}
				}
			}

			// Read stderr
			if task.StderrPath != "" {
				f, err := os.Open(task.StderrPath)
				if err == nil {
					defer f.Close()
					info, err := f.Stat()
					if err == nil && info.Size() > stderrOffset {
						_, _ = f.Seek(stderrOffset, io.SeekStart)
						buf := make([]byte, info.Size()-stderrOffset)
						n, err := f.Read(buf)
						if err == nil || err == io.EOF {
							stderrOffset += int64(n)
							newStderr = string(buf[:n])
						}
					}
				}
			}

			return newStdout, newStderr, nil
		}

		// Stream logs during WaitMs window or until completion
		ticker := time.NewTicker(logStreamPollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Read any new logs and yield them as progress updates or content
				newOut, newErr, _ := readNewLogs()
				if newOut != "" || newErr != "" {
					var content message.Content
					if newOut != "" {
						content = append(content, &message.TextBlock{Text: newOut})
					}
					// If there is new stderr, we also add it to content.
					if newErr != "" {
						content = append(content, &message.TextBlock{Text: "\n[stderr]\n" + newErr})
					}

					chunk := message.ToolChunk{
						BaseChunk: message.BaseChunk{ID: toolCallID},
						Content:   content,
					}
					if !yield(chunk, nil) {
						return
					}
				}

				// Check status
				h.TaskManager.mu.RLock()
				status := task.Status
				isBg := task.IsBackground
				h.TaskManager.mu.RUnlock()

				if status != StatusRunning {
					// Read final logs
					newOut, newErr, _ := readNewLogs()
					if newOut != "" || newErr != "" {
						var content message.Content
						if newOut != "" {
							content = append(content, &message.TextBlock{Text: newOut})
						}
						if newErr != "" {
							content = append(content, &message.TextBlock{Text: "\n[stderr]\n" + newErr})
						}
						yield(message.ToolChunk{
							BaseChunk: message.BaseChunk{ID: toolCallID},
							Content:   content,
						}, nil)
					}

					// Clean up output files using saveAndTruncate
					stdoutFinal, err := h.saveAndTruncate(ctx, task.StdoutPath, "stdout", toolCallID)
					if err != nil {
						stdoutFinal = fmt.Sprintf("Failed to read stdout log: %v", err)
					}
					stderrFinal, err := h.saveAndTruncate(ctx, task.StderrPath, "stderr", toolCallID)
					if err != nil {
						stderrFinal = fmt.Sprintf("Failed to read stderr log: %v", err)
					}

					// Yield final aggregated structured content chunk
					outObj := BashOutput{
						ExitCode: task.ExitCode,
						Stdout:   stdoutFinal,
						Stderr:   stderrFinal,
						Status:   string(status),
						Message:  task.Error,
					}

					yield(message.ToolChunk{
						BaseChunk:         message.BaseChunk{ID: toolCallID},
						StructuredContent: outObj,
					}, nil)
					return
				}

				// If the TaskManager transitioned the task to background, yield running chunk and stop streaming
				if isBg {
					stdoutSoFar := readAndTruncateBgLog(task.StdoutPath)
					stderrSoFar := readAndTruncateBgLog(task.StderrPath)

					var content message.Content
					hintText := fmt.Sprintf("\n<background_task task_id=\"%s\" status=\"running\">\nYou do not need to poll this task's status. The system will automatically notify you when it finishes. You can continue with other work, or stop calling tools to wait.\n</background_task>\n", task.ID)
					content = append(content, &message.TextBlock{Text: hintText})

					// Yield background status chunk
					outObj := BashOutput{
						TaskId:  task.ID,
						Status:  "running",
						Message: "Command took longer than wait threshold; running in background.",
						Stdout:  stdoutSoFar,
						Stderr:  stderrSoFar,
					}
					yield(message.ToolChunk{
						BaseChunk:         message.BaseChunk{ID: toolCallID},
						Content:           content,
						StructuredContent: outObj,
					}, nil)
					return
				}
			}
		}
	}, nil
}

// TextContent implements tool.TextContentProvider so loom renders the result
// as a human-readable message instead of a raw JSON blob.
func (o BashOutput) TextContent() string {
	var sb strings.Builder

	switch o.Status {
	case "running":
		fmt.Fprintf(&sb, "<background_task task_id=\"%s\" status=\"running\">\n", o.TaskId)
		sb.WriteString("You do not need to poll this task's status. The system will automatically notify you when it finishes. You can continue with other work, or stop calling tools to wait.\n")
		sb.WriteString("</background_task>\n")
	case "completed":
		fmt.Fprintf(&sb, "Command completed successfully (exit code %d).\n", o.ExitCode)
	case "failed":
		fmt.Fprintf(&sb, "Command failed with exit code %d.\n", o.ExitCode)
		if o.Message != "" {
			fmt.Fprintf(&sb, "Error: %s\n", o.Message)
		}
	case "killed":
		sb.WriteString("Command was terminated/killed.\n")
	default:
		fmt.Fprintf(&sb, "Command status: %s\n", o.Status)
	}

	if o.Stdout != "" {
		sb.WriteString("\n[stdout]\n")
		sb.WriteString(o.Stdout)
	}
	if o.Stderr != "" {
		sb.WriteString("\n[stderr]\n")
		sb.WriteString(o.Stderr)
	}

	return sb.String()
}
