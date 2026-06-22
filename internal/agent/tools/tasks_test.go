package tools

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/loom/message"
)

type mockFileStorage struct {
	saved map[string]string
}

func (m *mockFileStorage) Save(ctx context.Context, relativePath string, r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	m.saved[relativePath] = string(data)
	return "/mock/storage/" + relativePath, nil
}

func (m *mockFileStorage) Get(ctx context.Context, relativePath string) (io.ReadCloser, error) {
	content, ok := m.saved[relativePath]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func TestToolHandlers_BashAndTasks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasks-tool-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	taskMgr := NewTaskManager(tmpDir, nil)
	handlers := NewHandlers(nil, tmpDir).WithTaskManager(taskMgr, "session_123")

	// 1. Run sync command
	syncStream, err := handlers.Bash(context.Background(), BashArgs{
		Command:     "echo 'hello from tool'",
		Description: "hello print",
		WaitMs:      2000,
	})
	if err != nil {
		t.Fatalf("sync Bash tool call failed: %v", err)
	}
	var syncOut BashOutput
	syncStream(func(chunk message.ToolChunk, err error) bool {
		if err != nil {
			t.Fatalf("sync stream yielded error: %v", err)
		}
		if chunk.StructuredContent != nil {
			if bo, ok := chunk.StructuredContent.(BashOutput); ok {
				syncOut = bo
			}
		}
		return true
	})

	if syncOut.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", syncOut.ExitCode)
	}
	if strings.TrimSpace(syncOut.Stdout) != "hello from tool" {
		t.Errorf("expected stdout 'hello from tool', got %q", syncOut.Stdout)
	}
	if syncOut.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", syncOut.Status)
	}

	// 2. Run background command
	asyncStream, err := handlers.Bash(context.Background(), BashArgs{
		Command:     "sleep 1 && echo 'background tool done'",
		Description: "background echo",
		WaitMs:      10, // Go background almost immediately
	})
	if err != nil {
		t.Fatalf("async Bash tool call failed: %v", err)
	}
	var asyncOut BashOutput
	asyncStream(func(chunk message.ToolChunk, err error) bool {
		if err != nil {
			t.Fatalf("async stream yielded error: %v", err)
		}
		if chunk.StructuredContent != nil {
			if bo, ok := chunk.StructuredContent.(BashOutput); ok {
				asyncOut = bo
			}
		}
		return true
	})

	if asyncOut.TaskId == "" {
		t.Fatal("expected task ID to be returned for background command")
	}
	if asyncOut.Status != "running" {
		t.Errorf("expected status 'running', got %q", asyncOut.Status)
	}

	// 3. List tasks
	listOut, err := handlers.Tasks(context.Background(), TasksArgs{
		Action: "list",
	})
	if err != nil {
		t.Fatalf("Tasks list failed: %v", err)
	}
	// We ran 2 tasks so far (one sync, one async)
	if len(listOut.Tasks) != 2 {
		t.Errorf("expected 2 tasks in list, got %d", len(listOut.Tasks))
	}

	// 4. Check status of running task
	statusOut, err := handlers.Tasks(context.Background(), TasksArgs{
		Action: "status",
		TaskId: asyncOut.TaskId,
	})
	if err != nil {
		t.Fatalf("Tasks status failed: %v", err)
	}
	if statusOut.Status != "running" {
		t.Errorf("expected task status to be 'running', got %q", statusOut.Status)
	}

	// 5. Kill running task
	killOut, err := handlers.Tasks(context.Background(), TasksArgs{
		Action: "kill",
		TaskId: asyncOut.TaskId,
	})
	if err != nil {
		t.Fatalf("Tasks kill failed: %v", err)
	}
	if killOut.Status != "killed" {
		t.Errorf("expected killed status, got %q", killOut.Status)
	}

	// 6. Verify status updated to killed
	statusOut2, _ := handlers.Tasks(context.Background(), TasksArgs{
		Action: "status",
		TaskId: asyncOut.TaskId,
	})
	if statusOut2.Status != "killed" {
		t.Errorf("expected killed status after killing, got %q", statusOut2.Status)
	}
}

func TestToolHandlers_LargeOutputTruncation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasks-tool-large-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage := &mockFileStorage{saved: make(map[string]string)}
	taskMgr := NewTaskManager(tmpDir, nil)
	handlers := NewHandlers(storage, tmpDir).WithTaskManager(taskMgr, "session_large")

	// Inject tool call ID in context
	ctx := context.WithValue(context.Background(), "tool_call_id", "call_large_123")

	// 1. Run sync command with large output (approx 120KB, threshold is 100KB)
	// 'yes' prints 120000 characters
	largeCmd := "yes 'a' | head -n 60000" // Each 'a\n' is 2 bytes -> 120,000 bytes
	resStream, err := handlers.Bash(ctx, BashArgs{
		Command:     largeCmd,
		Description: "generate large output",
		WaitMs:      3000,
	})
	if err != nil {
		t.Fatalf("Bash execution failed: %v", err)
	}

	var res BashOutput
	resStream(func(chunk message.ToolChunk, err error) bool {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		if chunk.StructuredContent != nil {
			if bo, ok := chunk.StructuredContent.(BashOutput); ok {
				res = bo
			}
		}
		return true
	})

	if res.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", res.Status)
	}

	// Verify truncation: stdout should contain the warning note pointing to saved file path
	if !strings.Contains(res.Stdout, "[SYSTEM NOTE: The stdout output was too long and was truncated. The complete output is saved at: /mock/storage/outputs/call_large_123_stdout.txt.") {
		t.Errorf("expected stdout to contain truncation warning, got:\n%s", res.Stdout)
	}

	// Verify preview limit of 30,000 bytes
	if len(res.Stdout) > 35000 {
		t.Errorf("expected stdout preview to be limited, got length %d", len(res.Stdout))
	}

	// Verify storage saved the complete log
	savedContent, ok := storage.saved["outputs/call_large_123_stdout.txt"]
	if !ok {
		t.Fatal("expected stdout log to be saved under outputs/call_large_123_stdout.txt in storage")
	}

	// The log should be approximately 120,000 bytes (60,000 lines of 'a\n')
	if len(savedContent) < 100000 {
		t.Errorf("expected full log content to be saved in storage, got length %d", len(savedContent))
	}

	// 2. Test status checking of a completed large task
	// Start background command that outputs a lot and finishes
	asyncResStream, err := handlers.Bash(ctx, BashArgs{
		Command:     "sleep 0.5 && yes 'b' | head -n 60000", // 120,000 bytes
		Description: "generate large background output",
		WaitMs:      10, // Go async
	})
	if err != nil {
		t.Fatalf("Bash background run failed: %v", err)
	}

	var asyncRes BashOutput
	asyncResStream(func(chunk message.ToolChunk, err error) bool {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		if chunk.StructuredContent != nil {
			if bo, ok := chunk.StructuredContent.(BashOutput); ok {
				asyncRes = bo
			}
		}
		return true
	})

	// Wait for completion (within 2 seconds)
	time.Sleep(1 * time.Second)

	// Retrieve status. This should trigger saveAndTruncate
	ctxStatus := context.WithValue(context.Background(), "tool_call_id", "call_status_456")
	statusRes, err := handlers.Tasks(ctxStatus, TasksArgs{
		Action: "status",
		TaskId: asyncRes.TaskId,
	})
	if err != nil {
		t.Fatalf("Tasks status check failed: %v", err)
	}

	if statusRes.Status != "completed" {
		t.Errorf("expected completed task status, got %q (message: %q)", statusRes.Status, statusRes.Message)
	}

	// Verify status stdout is truncated and contains warning
	if !strings.Contains(statusRes.StdoutTail, "[SYSTEM NOTE: The stdout output was too long and was truncated. The complete output is saved at: /mock/storage/outputs/call_status_456_stdout.txt.") {
		t.Errorf("expected stdoutTail to contain truncation warning, got:\n%s", statusRes.StdoutTail)
	}

	// Verify storage saved the complete log of background task
	savedContent2, ok := storage.saved["outputs/call_status_456_stdout.txt"]
	if !ok {
		t.Fatal("expected stdout log to be saved under outputs/call_status_456_stdout.txt in storage")
	}
	if len(savedContent2) < 25000 {
		t.Errorf("expected full log content for background task to be saved, got length %d", len(savedContent2))
	}
}

func TestToolHandlers_BashAmpersandStripping(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tasks-amp-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	taskMgr := NewTaskManager(tmpDir, nil)
	handlers := NewHandlers(nil, tmpDir).WithTaskManager(taskMgr, "session_amp")

	// We pass a command ending in ` &` that would normally return immediately,
	// but we expect the trailing `&` to be stripped, meaning it runs for 500ms and transitions to background.
	asyncStream, err := handlers.Bash(context.Background(), BashArgs{
		Command:     "sleep 2 &",
		Description: "test ampersand stripping",
		WaitMs:      100, // Go background after 100ms
	})
	if err != nil {
		t.Fatalf("Bash tool call failed: %v", err)
	}

	var res BashOutput
	asyncStream(func(chunk message.ToolChunk, err error) bool {
		if err != nil {
			t.Fatalf("stream yielded error: %v", err)
		}
		if chunk.StructuredContent != nil {
			if bo, ok := chunk.StructuredContent.(BashOutput); ok {
				res = bo
			}
		}
		return true
	})

	// If the ampersand was NOT stripped, the bash process `bash -c 'sleep 2 &'` would return immediately (exit code 0),
	// and the status would be "completed".
	// Since the ampersand IS stripped, it executes `sleep 2` in foreground, does not complete in 100ms,
	// transitions to background, and status is "running" with a valid TaskId.
	if res.Status != "running" {
		t.Errorf("expected task to be running in background, got status %q (exitCode %d)", res.Status, res.ExitCode)
	}
	if res.TaskId == "" {
		t.Error("expected task ID to be returned for backgrounded task")
	}

	// Clean up task
	if res.TaskId != "" {
		taskMgr.KillTask(res.TaskId)
	}
}

func TestTextContentProviders(t *testing.T) {
	// Test BashOutput TextContent
	boRunning := BashOutput{
		TaskId: "task_123",
		Status: "running",
		Stdout: "server starting...",
		Stderr: "port check...",
	}
	tcRunning := boRunning.TextContent()
	if !strings.Contains(tcRunning, "running in the background") {
		t.Errorf("expected 'running in the background' in text content, got %q", tcRunning)
	}
	if !strings.Contains(tcRunning, "must use the 'tasks' tool") {
		t.Errorf("expected tasks tool instructions, got %q", tcRunning)
	}
	if !strings.Contains(tcRunning, "[stdout]\nserver starting...") {
		t.Errorf("expected stdout in text content, got %q", tcRunning)
	}

	boCompleted := BashOutput{
		ExitCode: 0,
		Status:   "completed",
		Stdout:   "hello",
	}
	tcCompleted := boCompleted.TextContent()
	if !strings.Contains(tcCompleted, "completed successfully") {
		t.Errorf("expected 'completed successfully', got %q", tcCompleted)
	}

	boFailed := BashOutput{
		ExitCode: 1,
		Status:   "failed",
		Message:  "some error",
	}
	tcFailed := boFailed.TextContent()
	if !strings.Contains(tcFailed, "failed with exit code 1") || !strings.Contains(tcFailed, "some error") {
		t.Errorf("expected failed status and message, got %q", tcFailed)
	}

	// Test TasksOutput TextContent
	toStatus := TasksOutput{
		Status:     "running",
		StdoutTail: "log tail line",
		Message:    "checking status",
	}
	tcStatus := toStatus.TextContent()
	if !strings.Contains(tcStatus, "checking status") || !strings.Contains(tcStatus, "Task Status: running") || !strings.Contains(tcStatus, "[stdout tail]\nlog tail line") {
		t.Errorf("expected message, status, and stdout tail, got %q", tcStatus)
	}

	toTasks := TasksOutput{
		Message: "Success listing",
		Tasks: []TasksOutputTasksItem{
			{
				TaskId: "task_123",
				Name:   "my server",
				Type:   "bash",
				Status: "running",
			},
		},
	}
	tcTasks := toTasks.TextContent()
	if !strings.Contains(tcTasks, "Background Tasks:") || !strings.Contains(tcTasks, "ID: task_123 | Name: \"my server\"") {
		t.Errorf("expected background task list format, got %q", tcTasks)
	}
}
