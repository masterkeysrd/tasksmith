package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutil"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

// TaskRunner defines the interface for executing a process or command.
type TaskRunner interface {
	// Start begins execution of the runner, directing standard output and error to the provided writers.
	// It must block until completion or context cancellation.
	Start(ctx context.Context, stdout io.Writer, stderr io.Writer) error

	// Stop gracefully terminates the running process/operation.
	Stop() error

	// WriteStdin writes the provided data to the runner's standard input.
	WriteStdin(data string) error
}

// TaskStatus represents the runtime execution state of a background task.
type TaskStatus string

const (
	StatusRunning   TaskStatus = "running"
	StatusCompleted TaskStatus = "completed"
	StatusFailed    TaskStatus = "failed"
	StatusKilled    TaskStatus = "killed"

	statusPollInterval   = 10 * time.Millisecond
	portPollInterval     = 2 * time.Second
	defaultExitCodeError = 1
	exitCodeSuccess      = 0
)

// Task holds the runtime execution state and metadata of a task.
type Task struct {
	ID           string     `json:"id"`
	SessionID    string     `json:"sessionId"`
	Type         string     `json:"type"`     // e.g. "bash", "mcp"
	Name         string     `json:"name"`     // User-friendly label or command string
	Status       TaskStatus `json:"status"`   // running, completed, failed, killed
	ExitCode     int        `json:"exitCode"` // Exit status code
	StartedAt    time.Time  `json:"startedAt"`
	FinishedAt   time.Time  `json:"finishedAt,omitzero"`
	Error        string     `json:"error,omitempty"`
	StdoutPath   string     `json:"stdoutPath"`
	StderrPath   string     `json:"stderrPath"`
	IsBackground bool       `json:"isBackground"`
	Details      string     `json:"details,omitempty"` // Extra generic human-readable task details

	runner TaskRunner
	cancel context.CancelFunc
}

// SubmitOptions defines the arguments for submitting a task to the TaskManager.
type SubmitOptions struct {
	SessionID  string
	TaskType   string
	Name       string
	Runner     TaskRunner
	WaitMs     int
	TimeoutSec int
}

// TaskManager orchestrates thread-safe creation, execution, and termination of background processes.
type TaskManager struct {
	mu             sync.RWMutex
	tasks          map[string]*Task
	workspacePath  string
	notifyCallback func(sessionID string, taskID string, task *Task)
}

// NewTaskManager creates a new centralized TaskManager for the workspace.
func NewTaskManager(workspacePath string, notifyCallback func(sessionID, taskID string, task *Task)) *TaskManager {
	tm := &TaskManager{
		tasks:          make(map[string]*Task),
		workspacePath:  workspacePath,
		notifyCallback: notifyCallback,
	}
	go tm.startPortPoller()
	return tm
}

// Submit registers and starts a task. If it completes within waitMs, it returns the finished task state.
// Otherwise, it transitions the task to background execution and returns a running task state.
func (tm *TaskManager) Submit(ctx context.Context, opts SubmitOptions) (*Task, error) {
	// Generate unique task ID
	u, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("failed to generate task UUID: %w", err)
	}
	taskID := fmt.Sprintf("task_%s", u.String())

	// Resolve directories and create stdout/stderr log files
	wsDir, err := xdg.WorkspaceDir(tm.workspacePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace data directory: %w", err)
	}

	tasksDir := filepath.Join(wsDir, "sessions", opts.SessionID, "tasks")
	if err := fsutil.EnsureDir(tasksDir); err != nil {
		return nil, fmt.Errorf("failed to create tasks directory: %w", err)
	}

	stdoutPath := filepath.Join(tasksDir, fmt.Sprintf("%s_stdout.log", taskID))
	stderrPath := filepath.Join(tasksDir, fmt.Sprintf("%s_stderr.log", taskID))

	stdoutFile, err := os.Create(stdoutPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout log file: %w", err)
	}

	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		stdoutFile.Close()
		return nil, fmt.Errorf("failed to create stderr log file: %w", err)
	}

	// Create sub-context for execution
	taskCtx, cancel := context.WithCancel(context.Background())
	if opts.TimeoutSec > 0 {
		taskCtx, cancel = context.WithTimeout(taskCtx, time.Duration(opts.TimeoutSec)*time.Second)
	}

	task := &Task{
		ID:         taskID,
		SessionID:  opts.SessionID,
		Type:       opts.TaskType,
		Name:       opts.Name,
		Status:     StatusRunning,
		StartedAt:  time.Now().UTC(),
		StdoutPath: stdoutPath,
		StderrPath: stderrPath,
		runner:     opts.Runner,
		cancel:     cancel,
	}

	tm.mu.Lock()
	tm.tasks[taskID] = task
	tm.mu.Unlock()

	doneChan := make(chan error, 1)

	// Run the TaskRunner asynchronously
	go func() {
		defer stdoutFile.Close()
		defer stderrFile.Close()

		err := opts.Runner.Start(taskCtx, stdoutFile, stderrFile)
		cancel() // Ensure cancellation context cleanup

		tm.finalizeTask(taskID, err)

		// Execute notification callback only if task actually transitioned to background
		tm.mu.RLock()
		isBg := task.IsBackground
		tm.mu.RUnlock()

		if isBg && tm.notifyCallback != nil {
			tm.notifyCallback(opts.SessionID, taskID, task)
		}
	}()

	// Watch for completion within the wait period
	go func() {
		// Wait on runner completion
		// To safely check if the process completed, we poll task status
		ticker := time.NewTicker(statusPollInterval)
		defer ticker.Stop()

		timeout := time.After(time.Duration(opts.WaitMs) * time.Millisecond)
		for {
			select {
			case <-ticker.C:
				tm.mu.RLock()
				status := task.Status
				tm.mu.RUnlock()
				if status != StatusRunning {
					doneChan <- nil
					return
				}
			case <-timeout:
				tm.mu.RLock()
				status := task.Status
				tm.mu.RUnlock()
				if status != StatusRunning {
					doneChan <- nil
				} else {
					doneChan <- context.DeadlineExceeded
				}
				return
			}
		}
	}()

	// Race wait period
	raceErr := <-doneChan
	if raceErr == nil {
		// Completed synchronously
		return task, nil
	}

	// Transitioned to background
	tm.mu.Lock()
	if task.Status == StatusRunning {
		task.IsBackground = true
	}
	tm.mu.Unlock()
	return task, nil
}

// GetTask retrieves a task by ID.
func (tm *TaskManager) GetTask(taskID string) (*Task, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	t, ok := tm.tasks[taskID]
	return t, ok
}

// ListTasks returns a list of tasks filtered by session ID.
func (tm *TaskManager) ListTasks(sessionID string) []*Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var result []*Task
	for _, t := range tm.tasks {
		if t.SessionID == sessionID {
			result = append(result, t)
		}
	}
	return result
}

// KillTask gracefully stops a running task.
func (tm *TaskManager) KillTask(taskID string) error {
	tm.mu.Lock()
	t, ok := tm.tasks[taskID]
	tm.mu.Unlock()

	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}

	tm.mu.RLock()
	status := t.Status
	tm.mu.RUnlock()

	if status != StatusRunning {
		return nil
	}

	// Trigger runner stop sequence
	if err := t.runner.Stop(); err != nil {
		return fmt.Errorf("failed to stop runner: %w", err)
	}

	t.cancel()

	tm.mu.Lock()
	t.Status = StatusKilled
	t.FinishedAt = time.Now().UTC()
	tm.mu.Unlock()

	return nil
}

// ReadLog returns the tail of the log file for stdout or stderr, applying safety budgets.
func (tm *TaskManager) ReadLog(taskID string, isStderr bool, limitLines int) (string, error) {
	tm.mu.RLock()
	t, ok := tm.tasks[taskID]
	tm.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("task %q not found", taskID)
	}

	path := t.StdoutPath
	if isStderr {
		path = t.StderrPath
	}

	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", err
	}

	if info.Size() == 0 {
		return "", nil
	}

	const maxTotalChars = 32000

	if limitLines <= 0 {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return applyLogBudget(string(data), 0, maxTotalChars), nil
	}

	const chunkSize = 4096
	var contentBytes []byte
	fileSize := info.Size()
	offset := fileSize
	newlinesFound := 0

	for offset > 0 && newlinesFound <= limitLines {
		currentReadSize := min(offset, chunkSize)
		offset -= currentReadSize

		_, err = file.Seek(offset, io.SeekStart)
		if err != nil {
			return "", err
		}

		buf := make([]byte, currentReadSize)
		n, err := file.Read(buf)
		if err != nil && err != io.EOF {
			return "", err
		}
		buf = buf[:n]

		// Count newlines in this chunk from right to left
		for i := len(buf) - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				newlinesFound++
				if newlinesFound > limitLines {
					// Found enough lines, trim start of buf to after this newline
					buf = buf[i+1:]
					break
				}
			}
		}

		contentBytes = append(buf, contentBytes...)
	}

	content := string(contentBytes)
	return applyLogBudget(content, limitLines, maxTotalChars), nil
}

// applyLogBudget trims the suffix newlines, cuts individual lines exceeding MaxLogLineChars,
// and ensures the overall returned content stays under maxTotalChars by keeping newest lines first.
func applyLogBudget(content string, limitLines int, maxTotalChars int) string {
	content = strings.TrimSuffix(content, "\n")
	content = strings.TrimSuffix(content, "\r")

	lines := strings.Split(content, "\n")
	if limitLines > 0 && len(lines) > limitLines {
		lines = lines[len(lines)-limitLines:]
	}

	const maxLogLineChars = 500

	var formattedLines []string
	totalChars := 0
	truncated := false

	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		charCount := len(line)

		var formattedLine string
		if charCount > maxLogLineChars {
			formattedLine = line[:maxLogLineChars] + fmt.Sprintf(" ... [Line truncated: %d characters omitted]", charCount-maxLogLineChars)
		} else {
			formattedLine = line
		}

		lineLength := len(formattedLine)
		if len(formattedLines) > 0 {
			lineLength += 1 // account for "\n"
		}

		if totalChars+lineLength > maxTotalChars {
			truncated = true
			break
		}

		formattedLines = append([]string{formattedLine}, formattedLines...)
		totalChars += lineLength
	}

	result := strings.Join(formattedLines, "\n")
	if truncated {
		result = "[... logs truncated due to size limits ...]\n" + result
	}
	return result
}

// WriteStdin writes the provided data to the task's standard input.
func (tm *TaskManager) WriteStdin(taskID string, data string) error {
	tm.mu.RLock()
	t, ok := tm.tasks[taskID]
	tm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}

	if t.Status != StatusRunning {
		return fmt.Errorf("cannot write to task %q: status is %s", taskID, t.Status)
	}

	return t.runner.WriteStdin(data)
}

func (tm *TaskManager) finalizeTask(taskID string, err error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	t, ok := tm.tasks[taskID]
	if !ok {
		return
	}

	if t.Status == StatusKilled {
		return // Already handled by KillTask
	}

	t.FinishedAt = time.Now().UTC()
	if err != nil {
		t.Status = StatusFailed
		t.Error = err.Error()
		t.ExitCode = defaultExitCodeError

		// Extract exit code if possible
		type exitCoder interface {
			ExitCode() int
		}
		if ec, ok := err.(exitCoder); ok {
			t.ExitCode = ec.ExitCode()
		} else if strings.Contains(err.Error(), "exit status") {
			var code int
			if _, scanErr := fmt.Sscanf(err.Error(), "exit status %d", &code); scanErr == nil {
				t.ExitCode = code
			}
		}
	} else {
		t.Status = StatusCompleted
		t.ExitCode = exitCodeSuccess
	}
}

// StateReporter defines the optional interface for runners that support reporting generic runtime execution state details.
type StateReporter interface {
	State() string
}

func (tm *TaskManager) startPortPoller() {
	ticker := time.NewTicker(portPollInterval)
	for range ticker.C {
		tm.pollState()
	}
}

func (tm *TaskManager) pollState() {
	tm.mu.Lock()
	var runningTasks []*Task
	for _, t := range tm.tasks {
		if t.Status == StatusRunning {
			runningTasks = append(runningTasks, t)
		}
	}
	tm.mu.Unlock()

	for _, t := range runningTasks {
		if reporter, ok := t.runner.(StateReporter); ok {
			state := reporter.State()

			tm.mu.Lock()
			changed := t.Details != state
			if changed {
				t.Details = state
			}
			tm.mu.Unlock()

			if changed && tm.notifyCallback != nil {
				tm.notifyCallback(t.SessionID, t.ID, t)
			}
		}
	}
}
