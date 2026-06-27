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
	"github.com/masterkeysrd/tasksmith/internal/core/fs"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/process"
	"github.com/masterkeysrd/tasksmith/internal/core/vcs"
	"github.com/masterkeysrd/tasksmith/internal/session/filetrack"
)

// BashRunner implements TaskRunner for OS shell commands.
type BashRunner struct {
	Command string
	CWD     string
	cmd     *exec.Cmd
	mu      sync.Mutex
}

func (br *BashRunner) Start(ctx context.Context, stdout io.Writer, stderr io.Writer) error {
	br.mu.Lock()
	cmd := exec.CommandContext(ctx, "bash", "-c", br.Command)
	cmd.Dir = br.CWD
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	// Set process group so we can terminate all subprocesses
	process.Prepare(cmd)

	// Configure graceful termination before SIGKILL
	cmd.Cancel = func() error {
		return process.Kill(cmd)
	}
	cmd.WaitDelay = 5 * time.Second
	br.cmd = cmd
	br.mu.Unlock()

	return cmd.Run()
}

func (br *BashRunner) Stop() error {
	br.mu.Lock()
	defer br.mu.Unlock()
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
	bashLogSizeThreshold = 100000 // 100KB threshold
	bashLogPreviewLimit  = 30000  // 30KB preview limit
	bashBgPreviewLimit   = 5000   // 5KB preview limit for background task logs
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

	if info.Size() <= bashBgPreviewLimit {
		data, err := os.ReadFile(logPath)
		if err != nil {
			return ""
		}
		return string(data)
	}

	// Read last bashBgPreviewLimit bytes
	offset := info.Size() - bashBgPreviewLimit
	_, err = f.Seek(offset, io.SeekStart)
	if err != nil {
		return ""
	}

	buf := make([]byte, bashBgPreviewLimit)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return ""
	}

	return "[... truncated to protect context window. Use the 'tasks' tool with action 'status' to view more/latest logs ...]\n" + string(buf[:n])
}

// saveAndTruncate checks the size of the log file at logPath. If it exceeds a threshold (20,000 bytes),
// it saves the full output in h.Storage (if available) under a name containing toolCallID and suffix,
// reads a truncated preview (5,000 bytes) of the output, and appends a warning note.
// Otherwise, it returns the full file content.
func (h *ToolHandlers) saveAndTruncate(ctx context.Context, logPath string, suffix string, toolCallID string) (string, error) {
	fileInfo, err := os.Stat(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // Log file does not exist yet (e.g. no output produced)
		}
		return "", err
	}

	if fileInfo.Size() <= bashLogSizeThreshold {
		data, err := os.ReadFile(logPath)
		if err != nil {
			return "", err
		}
		return string(data), nil
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

	// Read first preview limit characters from logPath
	_, _ = logFile.Seek(0, io.SeekStart)
	reader := io.LimitReader(logFile, bashLogPreviewLimit)
	buf := new(strings.Builder)
	_, _ = io.Copy(buf, reader)

	truncated := buf.String()
	note := fmt.Sprintf("\n\n[SYSTEM NOTE: The %s output was too long and was truncated. The complete output is saved at: %s. You can view the full file using 'view' or search it using 'grep'.]", suffix, savedPath)
	return truncated + note, nil
}

// Bash executes a bash command and returns a ToolStream.
func (h *ToolHandlers) Bash(ctx context.Context, in BashArgs) (tool.ToolStream, error) {
	toolCallID, _ := ctx.Value("tool_call_id").(string)

	return func(yield func(message.ToolChunk, error) bool) {
		// If task manager is nil, fallback to synchronous one-shot combined execution
		if h.TaskManager == nil {
			detector := newChangeDetector(h.CWD)
			cmd := exec.CommandContext(ctx, "bash", "-c", in.Command)
			cmd.Dir = h.CWD
			process.Prepare(cmd)
			cmd.Cancel = func() error {
				return process.Kill(cmd)
			}
			cmd.WaitDelay = 5 * time.Second
			out, err := cmd.CombinedOutput()
			var exitCode int
			var status = "completed"
			var stderrMsg string
			if err != nil {
				exitCode = 1
				status = "failed"
				stderrMsg = err.Error()
			}
			if err == nil {
				cdChanges := detector.DetectChanges()
				recordBashChanges(ctx, h.FileTracker, h.CWD, cdChanges)
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
			waitMs = 10000 // Default: 10 seconds
		}

		detector := newChangeDetector(h.CWD)
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
		ticker := time.NewTicker(50 * time.Millisecond)
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

					cdChanges := detector.DetectChanges()
					recordBashChanges(ctx, h.FileTracker, h.CWD, cdChanges)

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
					hintText := fmt.Sprintf("\nCommand is running in the background (Task ID: %s).\nTo manage or monitor this task, you must use the 'tasks' tool (e.g., action: 'status' or 'kill' with taskId: '%s').\n", task.ID, task.ID)
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
		fmt.Fprintf(&sb, "Command is running in the background (Task ID: %s).\n", o.TaskId)
		fmt.Fprintf(&sb, "To manage or monitor this task, you must use the 'tasks' tool (e.g., action: 'status' or 'kill' with taskId: '%s').\n", o.TaskId)
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

type bashChangeDetector struct {
	cwd       string
	isGit     bool
	ignorer   fs.Ignorer
	preStatus map[string]string
	preMtimes map[string]time.Time
}

func newChangeDetector(cwd string) *bashChangeDetector {
	ign, _ := fs.NewIgnorer(cwd)
	cd := &bashChangeDetector{
		cwd:       cwd,
		ignorer:   ign,
		preMtimes: make(map[string]time.Time),
	}
	if vcs.IsGitAvailable() && vcs.IsRepo(cwd) {
		cd.isGit = true
		status, err := vcs.GetStatus(cwd)
		if err == nil {
			cd.preStatus = parseGitStatusLines(status)
		}
	}
	cd.scanMtimes()
	return cd
}

func parseGitStatusLines(statusOutput string) map[string]string {
	m := make(map[string]string)
	lines := strings.SplitSeq(statusOutput, "\n")
	for l := range lines {
		if len(l) < 4 {
			continue
		}
		status := l[:2]
		path := strings.TrimSpace(l[2:])
		m[path] = status
	}
	return m
}

func (cd *bashChangeDetector) scanMtimes() {
	_ = filepath.Walk(cd.cwd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(cd.cwd, path)
		if err != nil {
			return nil
		}
		name := info.Name()
		isDir := info.IsDir()
		if cd.ignorer != nil && cd.ignorer.ShouldIgnore(name, path, isDir) {
			if isDir {
				return filepath.SkipDir
			}
			return nil
		}
		if isDir {
			if strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			return nil
		}
		cd.preMtimes[rel] = info.ModTime()
		return nil
	})
}

func (cd *bashChangeDetector) DetectChanges() []filetrack.Change {
	var changes []filetrack.Change

	postMtimes := make(map[string]time.Time)
	_ = filepath.Walk(cd.cwd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(cd.cwd, path)
		if err != nil {
			return nil
		}
		name := info.Name()
		isDir := info.IsDir()
		if cd.ignorer != nil && cd.ignorer.ShouldIgnore(name, path, isDir) {
			if isDir {
				return filepath.SkipDir
			}
			return nil
		}
		if isDir {
			if strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			return nil
		}
		postMtimes[rel] = info.ModTime()
		return nil
	})

	for rel, postTime := range postMtimes {
		preTime, existed := cd.preMtimes[rel]
		if !existed {
			changes = append(changes, filetrack.Change{
				ToolName: "bash",
				Path:     "./" + filepath.ToSlash(rel),
				Kind:     filetrack.Created,
			})
		} else if postTime.After(preTime) {
			changes = append(changes, filetrack.Change{
				ToolName: "bash",
				Path:     "./" + filepath.ToSlash(rel),
				Kind:     filetrack.Modified,
			})
		}
	}

	for rel := range cd.preMtimes {
		if _, existed := postMtimes[rel]; !existed {
			changes = append(changes, filetrack.Change{
				ToolName: "bash",
				Path:     "./" + filepath.ToSlash(rel),
				Kind:     filetrack.Deleted,
			})
		}
	}

	if cd.isGit {
		status, err := vcs.GetStatus(cd.cwd)
		if err == nil {
			postStatus := parseGitStatusLines(status)
			for rel, postStat := range postStatus {
				preStat, existed := cd.preStatus[rel]
				if !existed || preStat != postStat {
					relSlash := "./" + filepath.ToSlash(rel)
					found := false
					for _, ch := range changes {
						if ch.Path == relSlash {
							found = true
							break
						}
					}
					if !found {
						kind := filetrack.Modified
						if strings.Contains(postStat, "D") {
							kind = filetrack.Deleted
						} else if strings.Contains(postStat, "?") || strings.Contains(postStat, "A") {
							kind = filetrack.Created
						}
						changes = append(changes, filetrack.Change{
							ToolName: "bash",
							Path:     relSlash,
							Kind:     kind,
						})
					}
				}
			}
		}
	}

	return changes
}

func countLinesInFile(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "\n") + 1
}

func runGitCmd(cwd string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func recordBashChanges(ctx context.Context, ft filetrack.FileTracker, cwd string, changes []filetrack.Change) {
	if ft == nil {
		return
	}
	for _, change := range changes {
		rel := strings.TrimPrefix(change.Path, "./")
		absPath := filepath.Join(cwd, rel)

		isBinary := false
		if change.Kind != filetrack.Deleted {
			mimeType := fs.DetectMIMEType(absPath)
			isBinary = fs.IsBinaryMIME(mimeType)
		}

		var diffStr string
		var oldContent string
		var additions, deletions int

		if !isBinary {
			if change.Kind == filetrack.Created {
				additions = countLinesInFile(absPath)
			} else if change.Kind == filetrack.Deleted {
				// Deletions count is 0 as file content is gone and not tracked here
			} else if change.Kind == filetrack.Modified {
				newBytes, err := os.ReadFile(absPath)
				var newContent string
				if err == nil {
					newContent = string(newBytes)
					additions = strings.Count(newContent, "\n") + 1
				}

				if vcs.IsGitAvailable() && vcs.IsRepo(cwd) {
					diffStr = runGitCmd(cwd, "diff", "--", rel)
					showOut := runGitCmd(cwd, "show", ":"+rel)
					if showOut == "" {
						showOut = runGitCmd(cwd, "show", "HEAD:"+rel)
					}
					if showOut != "" {
						oldContent = showOut
						deletions = strings.Count(oldContent, "\n") + 1
					}
				}
			}
		}

		change.Additions = additions
		change.Deletions = deletions

		_ = ft.Record(ctx, change, diffStr, oldContent)
	}
}
