package tools

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskManager_Submit_Sync(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskmgr-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tm := NewTaskManager(tmpDir, nil)

	runner := &BashRunner{
		Command: "echo 'hello world'",
		CWD:     tmpDir,
	}

	task, err := tm.Submit(context.Background(), SubmitOptions{
		SessionID: "sess_test",
		TaskType:  "bash",
		Name:      "test_sync",
		Runner:    runner,
		WaitMs:    2000,
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	if task.Status != StatusCompleted {
		t.Errorf("expected status %s, got %s", StatusCompleted, task.Status)
	}
	if task.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", task.ExitCode)
	}

	stdout, err := tm.ReadLog(task.ID, false, 0)
	if err != nil {
		t.Fatalf("ReadLog failed: %v", err)
	}
	if strings.TrimSpace(stdout) != "hello world" {
		t.Errorf("expected stdout 'hello world', got %q", stdout)
	}
}

func TestTaskManager_Submit_Async(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskmgr-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	notifyChan := make(chan string, 1)
	tm := NewTaskManager(tmpDir, func(sessionID, taskID string, task *Task) {
		notifyChan <- taskID
	})

	runner := &BashRunner{
		Command: "sleep 0.5 && echo 'background done'",
		CWD:     tmpDir,
	}

	task, err := tm.Submit(context.Background(), SubmitOptions{
		SessionID: "sess_test",
		TaskType:  "bash",
		Name:      "test_async",
		Runner:    runner,
		WaitMs:    100,
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	// Should transition to background since it takes 500ms > 100ms
	if task.Status != StatusRunning {
		t.Errorf("expected status %s, got %s", StatusRunning, task.Status)
	}

	select {
	case notifiedTaskID := <-notifyChan:
		if notifiedTaskID != task.ID {
			t.Errorf("expected notified task ID %s, got %s", task.ID, notifiedTaskID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for task completion notification")
	}

	// Re-fetch task
	t2, ok := tm.GetTask(task.ID)
	if !ok {
		t.Fatalf("task %s not found in registry", task.ID)
	}

	if t2.Status != StatusCompleted {
		t.Errorf("expected post-notify status %s, got %s", StatusCompleted, t2.Status)
	}

	stdout, err := tm.ReadLog(task.ID, false, 0)
	if err != nil {
		t.Fatalf("ReadLog failed: %v", err)
	}
	if strings.TrimSpace(stdout) != "background done" {
		t.Errorf("expected stdout 'background done', got %q", stdout)
	}
}

func TestTaskManager_KillTask(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskmgr-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tm := NewTaskManager(tmpDir, nil)

	runner := &BashRunner{
		Command: "sleep 10",
		CWD:     tmpDir,
	}

	task, err := tm.Submit(context.Background(), SubmitOptions{
		SessionID: "sess_test",
		TaskType:  "bash",
		Name:      "test_kill",
		Runner:    runner,
		WaitMs:    100,
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	if task.Status != StatusRunning {
		t.Fatalf("expected task to be running, got %s", task.Status)
	}

	if err := tm.KillTask(task.ID); err != nil {
		t.Fatalf("KillTask failed: %v", err)
	}

	// Verify status is killed
	if task.Status != StatusKilled {
		t.Errorf("expected status %s, got %s", StatusKilled, task.Status)
	}
}

func TestTaskManager_StdoutStderrSeparation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskmgr-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tm := NewTaskManager(tmpDir, nil)

	runner := &BashRunner{
		Command: "echo 'out message' && echo 'err message' >&2",
		CWD:     tmpDir,
	}

	task, err := tm.Submit(context.Background(), SubmitOptions{
		SessionID: "sess_test",
		TaskType:  "bash",
		Name:      "test_sep",
		Runner:    runner,
		WaitMs:    2000,
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	stdout, _ := tm.ReadLog(task.ID, false, 0)
	stderr, _ := tm.ReadLog(task.ID, true, 0)

	if strings.TrimSpace(stdout) != "out message" {
		t.Errorf("expected stdout 'out message', got %q", stdout)
	}
	if strings.TrimSpace(stderr) != "err message" {
		t.Errorf("expected stderr 'err message', got %q", stderr)
	}
}

type DummyRunner struct {
	startFunc func(ctx context.Context, stdout, stderr io.Writer) error
	stopFunc  func() error
}

func (d *DummyRunner) Start(ctx context.Context, stdout, stderr io.Writer) error {
	if d.startFunc != nil {
		return d.startFunc(ctx, stdout, stderr)
	}
	return nil
}

func (d *DummyRunner) Stop() error {
	if d.stopFunc != nil {
		return d.stopFunc()
	}
	return nil
}

func (d *DummyRunner) WriteStdin(data string) error {
	return nil
}

func TestTaskManager_Submit_Timeout(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskmgr-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tm := NewTaskManager(tmpDir, nil)

	runner := &DummyRunner{
		startFunc: func(ctx context.Context, stdout, stderr io.Writer) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}

	task, err := tm.Submit(context.Background(), SubmitOptions{
		SessionID:  "sess_test",
		TaskType:   "dummy",
		Name:       "test_timeout",
		Runner:     runner,
		WaitMs:     100,
		TimeoutSec: 1, // timeout in 1 second
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	// Should go async
	if task.Status != StatusRunning {
		t.Fatalf("expected running, got %s", task.Status)
	}

	// Wait for timeout to fire (should be within 1.5 seconds)
	time.Sleep(1500 * time.Millisecond)

	t2, _ := tm.GetTask(task.ID)
	if t2.Status != StatusFailed {
		t.Errorf("expected failed status due to timeout, got %s", t2.Status)
	}
	if !strings.Contains(t2.Error, "context deadline exceeded") {
		t.Errorf("expected timeout error, got %q", t2.Error)
	}
}

func TestTaskManager_Submit_SyncCallback(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskmgr-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	called := false
	tm := NewTaskManager(tmpDir, func(sessionID, taskID string, task *Task) {
		called = true
	})

	runner := &BashRunner{
		Command: "echo 'hello sync'",
		CWD:     tmpDir,
	}

	_, err = tm.Submit(context.Background(), SubmitOptions{
		SessionID: "sess_test",
		TaskType:  "bash",
		Name:      "test_sync_callback",
		Runner:    runner,
		WaitMs:    1000,
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	// Wait 150ms to ensure the runner goroutine has fully completed and checked IsBackground
	time.Sleep(150 * time.Millisecond)

	if called {
		t.Error("expected notification callback NOT to be called for synchronous task")
	}
}

func TestTaskManager_KillTaskGroup(t *testing.T) {
	// Only run on unix/macOS where shell and ps commands are fully supported
	if os.PathSeparator == '\\' {
		t.Skip("skipping process group kill test on Windows")
	}

	tmpDir, err := os.MkdirTemp("", "taskmgr-test-group-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tm := NewTaskManager(tmpDir, nil)

	// Command: sleep in background, save its PID to child.pid, then wait for it
	cmdStr := `sleep 30 & echo $! > child.pid; wait`

	runner := &BashRunner{
		Command: cmdStr,
		CWD:     tmpDir,
	}

	task, err := tm.Submit(context.Background(), SubmitOptions{
		SessionID: "sess_test",
		TaskType:  "bash",
		Name:      "test_kill_group",
		Runner:    runner,
		WaitMs:    200,
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	// Verify task goes async
	if task.Status != StatusRunning {
		t.Fatalf("expected running, got %s", task.Status)
	}

	// Wait for child.pid to be written
	var childPid string
	pidPath := filepath.Join(tmpDir, "child.pid")
	for range 20 {
		data, err := os.ReadFile(pidPath)
		if err == nil && len(data) > 0 {
			childPid = strings.TrimSpace(string(data))
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if childPid == "" {
		t.Fatal("failed to read child process PID")
	}

	// Kill the main task (which should kill the process group)
	if err := tm.KillTask(task.ID); err != nil {
		t.Fatalf("KillTask failed: %v", err)
	}

	// Verify task is registered as killed
	if task.Status != StatusKilled {
		t.Errorf("expected status %s, got %s", StatusKilled, task.Status)
	}

	// Wait a moment for processes to exit
	time.Sleep(200 * time.Millisecond)

	// Verify the child process is terminated.
	// ps -p <PID> exits with non-zero if process does not exist.
	checkCmd := exec.Command("ps", "-p", childPid)
	err = checkCmd.Run()
	if err == nil {
		t.Errorf("child process %s is still running after task kill", childPid)
		// Clean up the orphan just in case
		exec.Command("kill", "-9", childPid).Run()
	}
}

func TestTaskManager_ReadLog_Budgeting(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taskmgr-readlog-budget-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tm := NewTaskManager(tmpDir, nil)
	taskID := "task_test_budget"

	sessDir := filepath.Join(tmpDir, "sessions", "sess_test", "tasks")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	stdoutPath := filepath.Join(sessDir, taskID+"_stdout.log")

	longLine := strings.Repeat("A", 2897)
	logContent := "short line 1\n" + longLine + "\nshort line 2\n"
	if err := os.WriteFile(stdoutPath, []byte(logContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	tm.mu.Lock()
	tm.tasks[taskID] = &Task{
		ID:         taskID,
		SessionID:  "sess_test",
		StdoutPath: stdoutPath,
		Status:     StatusCompleted,
	}
	tm.mu.Unlock()

	got, err := tm.ReadLog(taskID, false, 2)
	if err != nil {
		t.Fatalf("ReadLog failed: %v", err)
	}

	if !strings.Contains(got, "[Line truncated: 2397 characters omitted]") {
		t.Errorf("expected long line to be truncated, got: %s", got)
	}
	if !strings.Contains(got, "short line 2") {
		t.Errorf("expected last line to be present, got: %s", got)
	}
	if strings.Contains(got, "short line 1") {
		t.Errorf("expected first line to be excluded by limitLines, got: %s", got)
	}
}
