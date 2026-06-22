package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownload_Sync_Success(t *testing.T) {
	// Start local mock HTTP server
	expectedContent := "Hello, this is a mock downloaded file!"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(expectedContent)))
		_, _ = w.Write([]byte(expectedContent))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	taskMgr := NewTaskManager(tmpDir, nil)
	handlers := NewHandlers(nil, tmpDir).WithTaskManager(taskMgr, "session_123")

	destName := "test_download_file.txt"
	out, err := handlers.Download(context.Background(), DownloadArgs{
		Url:         server.URL,
		Destination: destName,
		WaitMs:      2000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !out.Success {
		t.Errorf("expected download success, got false with message: %s", out.Message)
	}
	if out.TaskId != "" {
		t.Errorf("expected taskId to be empty for sync completion, got %q", out.TaskId)
	}
	if out.SizeBytes != len(expectedContent) {
		t.Errorf("expected size_bytes to be %d, got %d", len(expectedContent), out.SizeBytes)
	}

	// Verify file content on disk
	filePath := filepath.Join(tmpDir, destName)
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(content) != expectedContent {
		t.Errorf("expected downloaded content %q, got %q", expectedContent, string(content))
	}
}

func TestDownload_Sync_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	taskMgr := NewTaskManager(tmpDir, nil)
	handlers := NewHandlers(nil, tmpDir).WithTaskManager(taskMgr, "session_123")

	out, err := handlers.Download(context.Background(), DownloadArgs{
		Url:    server.URL,
		WaitMs: 2000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Success {
		t.Errorf("expected success to be false, got true")
	}
	if !strings.Contains(out.Message, "server returned status") && !strings.Contains(out.Message, "HTTP error") {
		t.Errorf("expected error message indicating status code error, got %q", out.Message)
	}
}

func TestDownload_Background(t *testing.T) {
	expectedContent := "This download transitions to the background."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep to force transition to background task
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(expectedContent))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	taskMgr := NewTaskManager(tmpDir, nil)
	handlers := NewHandlers(nil, tmpDir).WithTaskManager(taskMgr, "session_123")

	destName := "bg_download.bin"
	out, err := handlers.Download(context.Background(), DownloadArgs{
		Url:         server.URL,
		Destination: destName,
		WaitMs:      20, // Transition immediately
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !out.Success {
		t.Fatalf("expected success response starting background download, got false: %s", out.Message)
	}
	if out.TaskId == "" {
		t.Fatalf("expected background taskId, got empty string")
	}

	// Verify task exists in manager and is running
	task, exists := taskMgr.GetTask(out.TaskId)
	if !exists {
		t.Fatalf("background task not found in TaskManager")
	}

	// Wait for task to finish
	for i := 0; i < 20; i++ {
		taskMgr.mu.RLock()
		status := task.Status
		taskMgr.mu.RUnlock()
		if status != StatusRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	taskMgr.mu.RLock()
	finalStatus := task.Status
	taskMgr.mu.RUnlock()

	if finalStatus != StatusCompleted {
		t.Errorf("expected final task status to be completed, got %q (error: %s)", finalStatus, task.Error)
	}

	// Verify file content on disk
	filePath := filepath.Join(tmpDir, destName)
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read bg downloaded file: %v", err)
	}
	if string(content) != expectedContent {
		t.Errorf("expected downloaded content %q, got %q", expectedContent, string(content))
	}
}

func TestDownload_StuckWatchdog(t *testing.T) {
	// Temporarily override the default idle timeout
	oldIdleTimeout := defaultIdleTimeout
	defaultIdleTimeout = 50 * time.Millisecond
	defer func() { defaultIdleTimeout = oldIdleTimeout }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write initial chunk
		_, _ = w.Write([]byte("hello"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		// Stall the response indefinitely to trigger the watchdog
		select {
		case <-r.Context().Done():
			// Client cancelled
		case <-time.After(1 * time.Second):
			// Keep-alive stall
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	taskMgr := NewTaskManager(tmpDir, nil)
	handlers := NewHandlers(nil, tmpDir).WithTaskManager(taskMgr, "session_123")

	out, err := handlers.Download(context.Background(), DownloadArgs{
		Url:    server.URL,
		WaitMs: 10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for the background task to complete (or fail) due to the watchdog
	var finalTask *Task
	if out.TaskId != "" {
		task, _ := taskMgr.GetTask(out.TaskId)
		for i := 0; i < 20; i++ {
			taskMgr.mu.RLock()
			status := task.Status
			taskMgr.mu.RUnlock()
			if status != StatusRunning {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		finalTask = task
	}

	if finalTask == nil {
		t.Fatalf("expected task to transition to background or fail, got nil task")
	}

	taskMgr.mu.RLock()
	status := finalTask.Status
	taskErr := finalTask.Error
	taskMgr.mu.RUnlock()

	if status != StatusFailed {
		t.Errorf("expected task status failed, got %q", status)
	}

	if !strings.Contains(taskErr, "context canceled") && !strings.Contains(taskErr, "idle timeout") {
		t.Errorf("expected task error to indicate cancellation/stalled watchdog, got %q", taskErr)
	}
}
