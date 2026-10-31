package tasks

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestFastSynchronousCompletion(t *testing.T) {
	var updateCalls []api.Task
	var mu sync.Mutex

	mgr := New(Config{
		OnUpdate: func(s api.Task) {
			mu.Lock()
			updateCalls = append(updateCalls, s)
			mu.Unlock()
		},
	})
	defer mgr.Close()

	task := &Task{
		Kind:  "test",
		Title: "fast task",
		Wait:  time.Second,
		Fn: func(ctx context.Context, output io.Writer, stdin io.Reader) (int, error) {
			time.Sleep(10 * time.Millisecond)
			_, _ = output.Write([]byte("quick result\n"))
			return 0, nil
		},
	}

	if err := mgr.Add(task); err != nil {
		t.Fatalf("expected nil error on Add, got %v", err)
	}
	err := task.Run(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if task.Backgrounded() {
		t.Fatalf("expected task not backgrounded")
	}
	if task.Status() != api.TaskStatusCompleted {
		t.Fatalf("expected status %v, got %v", api.TaskStatusCompleted, task.Status())
	}
	if task.ExitCode() != 0 {
		t.Fatalf("expected exit code 0, got %d", task.ExitCode())
	}

	mu.Lock()
	callCount := len(updateCalls)
	mu.Unlock()
	if callCount != 0 {
		t.Fatalf("expected OnUpdate not called for sync completion, got %d calls", callCount)
	}

	// List should only return backgrounded tasks
	list := mgr.List()
	if len(list) != 0 {
		t.Fatalf("expected List() to be empty for non-backgrounded task, got %d", len(list))
	}
}

func TestCallerContextCancellationDuringWait(t *testing.T) {
	mgr := New(Config{})
	defer mgr.Close()

	started := make(chan struct{})
	task := &Task{
		Kind:  "test",
		Title: "hang task",
		Wait:  5 * time.Second,
		Fn: func(ctx context.Context, output io.Writer, stdin io.Reader) (int, error) {
			close(started)
			<-ctx.Done()
			return 130, ctx.Err()
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	if err := mgr.Add(task); err != nil {
		t.Fatalf("expected nil error on Add, got %v", err)
	}
	err := task.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	if task.Backgrounded() {
		t.Fatalf("expected task not backgrounded")
	}
	if task.Status() != api.TaskStatusCanceled {
		t.Fatalf("expected status %v, got %v", api.TaskStatusCanceled, task.Status())
	}
}

func TestBackgroundTransitionWhenWaitExpires(t *testing.T) {
	var updates []api.Task
	var mu sync.Mutex

	mgr := New(Config{
		OnUpdate: func(s api.Task) {
			mu.Lock()
			updates = append(updates, s)
			mu.Unlock()
		},
	})
	defer mgr.Close()

	unblock := make(chan struct{})
	task := &Task{
		Kind:  "bash",
		Title: "backgrounded task",
		Wait:  25 * time.Millisecond,
		Fn: func(ctx context.Context, output io.Writer, stdin io.Reader) (int, error) {
			<-unblock
			_, _ = output.Write([]byte("done\n"))
			return 0, nil
		},
	}

	if err := mgr.Add(task); err != nil {
		t.Fatalf("expected nil error on Add, got %v", err)
	}
	err := task.Run(context.Background())
	if err != nil {
		t.Fatalf("expected nil error on background transition, got %v", err)
	}

	if !task.Backgrounded() {
		t.Fatalf("expected task to be backgrounded")
	}

	// Verify first update was StatusRunning
	mu.Lock()
	if len(updates) < 1 {
		mu.Unlock()
		t.Fatalf("expected at least 1 update, got %d", len(updates))
	}
	first := updates[0]
	mu.Unlock()

	if first.Status != api.TaskStatusRunning {
		t.Fatalf("expected first update status %v, got %v", api.TaskStatusRunning, first.Status)
	}
	if !first.Backgrounded {
		t.Fatalf("expected first update backgrounded true")
	}

	// Unblock task and let it complete
	close(unblock)
	<-task.Done()

	// Wait briefly for completion onUpdate callback
	var last api.Task
	for range 50 {
		mu.Lock()
		if len(updates) >= 2 {
			last = updates[len(updates)-1]
			mu.Unlock()
			break
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}

	if last.Status != api.TaskStatusCompleted {
		t.Fatalf("expected second update status %v, got %v", api.TaskStatusCompleted, last.Status)
	}

	list := mgr.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 task in List(), got %d", len(list))
	}
	if list[0].ID != task.ID {
		t.Fatalf("expected task ID %q, got %q", task.ID, list[0].ID)
	}
}

func TestWriteInputTailAndSubscribeOutput(t *testing.T) {
	tempDir := t.TempDir()

	mgr := New(Config{
		OutputsDir: tempDir,
	})
	defer mgr.Close()

	inputReceived := make(chan string, 1)
	task := &Task{
		Kind:  "custom",
		Title: "interactive task",
		Wait:  0, // immediate background
		Fn: func(ctx context.Context, output io.Writer, stdin io.Reader) (int, error) {
			_, _ = output.Write([]byte("line 1\n"))
			_, _ = output.Write([]byte("line 2\n"))

			buf := make([]byte, 64)
			n, err := stdin.Read(buf)
			if err == nil {
				inputReceived <- string(buf[:n])
				_, _ = output.Write([]byte("received: " + string(buf[:n])))
			}
			_, _ = output.Write([]byte("\nline 3\n"))
			return 0, nil
		},
	}

	if err := mgr.Add(task); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	err := task.Run(context.Background())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Subscribe to output
	chunks, _, unsub := task.SubscribeOutput()
	defer unsub()

	// Feed input directly on task
	if err := task.WriteInput("hello stdin"); err != nil {
		t.Fatalf("WriteInput failed: %v", err)
	}

	select {
	case in := <-inputReceived:
		if in != "hello stdin" {
			t.Fatalf("got stdin %q, want 'hello stdin'", in)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stdin")
	}

	<-task.Done()

	// Drain chunks or read tail
	tail, err := task.Tail(2)
	if err != nil {
		t.Fatalf("Tail failed: %v", err)
	}
	if !strings.Contains(tail, "line 3") {
		t.Fatalf("expected tail to contain 'line 3', got %q", tail)
	}

	// Verify file was written on disk
	logPath := task.LogPath()
	if logPath == "" {
		t.Fatal("expected non-empty logPath")
	}
	fileBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file failed: %v", err)
	}
	if !strings.Contains(string(fileBytes), "received: hello stdin") {
		t.Fatalf("expected log file to contain input, got %q", string(fileBytes))
	}

	// Verify subscriber channel closes on task completion
	select {
	case <-chunks:
	case <-time.After(time.Second):
		t.Fatal("expected chunks channel to close")
	}
}

func TestKillRunningBackgroundTask(t *testing.T) {
	mgr := New(Config{})
	defer mgr.Close()

	task := &Task{
		Kind:  "bash",
		Title: "infinite task",
		Wait:  10 * time.Millisecond,
		Fn: func(ctx context.Context, output io.Writer, stdin io.Reader) (int, error) {
			<-ctx.Done()
			return 1, ctx.Err()
		},
	}

	if err := mgr.Add(task); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if err := task.Kill(); err != nil {
		t.Fatalf("Kill failed: %v", err)
	}

	<-task.Done()

	if task.Status() != api.TaskStatusCanceled {
		t.Fatalf("expected status %v, got %v", api.TaskStatusCanceled, task.Status())
	}
}

func TestListSorting(t *testing.T) {
	mgr := New(Config{})
	defer mgr.Close()

	t1 := &Task{
		ID:    "task-b",
		Title: "task B",
		Wait:  0,
		Fn: func(ctx context.Context, output io.Writer, stdin io.Reader) (int, error) {
			time.Sleep(50 * time.Millisecond)
			return 0, nil
		},
	}
	t2 := &Task{
		ID:    "task-a",
		Title: "task A",
		Wait:  0,
		Fn: func(ctx context.Context, output io.Writer, stdin io.Reader) (int, error) {
			time.Sleep(50 * time.Millisecond)
			return 0, nil
		},
	}

	if err := mgr.Add(t1); err != nil {
		t.Fatalf("Add t1: %v", err)
	}
	_ = t1.Run(context.Background())
	time.Sleep(5 * time.Millisecond)
	if err := mgr.Add(t2); err != nil {
		t.Fatalf("Add t2: %v", err)
	}
	_ = t2.Run(context.Background())

	list := mgr.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(list))
	}
	if list[0].ID != "task-b" || list[1].ID != "task-a" {
		t.Fatalf("expected order [task-b, task-a], got [%s, %s]", list[0].ID, list[1].ID)
	}
}

func TestBoundedMemoryAndDiskWrites(t *testing.T) {
	tempDir := t.TempDir()
	mgr := New(Config{
		OutputsDir: tempDir,
	})
	defer mgr.Close()

	task := &Task{
		Kind:  "test",
		Title: "heavy output",
		Wait:  time.Second,
		Fn: func(ctx context.Context, output io.Writer, stdin io.Reader) (int, error) {
			// Write 100 KB total in small chunks
			chunk := strings.Repeat("A", 1024) + "\n"
			for range 100 {
				_, _ = output.Write([]byte(chunk))
			}
			_, _ = output.Write([]byte("FINAL_LINE\n"))
			return 0, nil
		},
	}

	if err := mgr.Add(task); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	task.mu.Lock()
	bufLen := task.buf.Len()
	task.mu.Unlock()

	if bufLen > MaxLogMemBytes {
		t.Fatalf("task.buf len %d exceeds MaxLogMemBytes %d", bufLen, MaxLogMemBytes)
	}

	tail, err := task.Tail(1)
	if err != nil {
		t.Fatalf("Tail failed: %v", err)
	}
	if !strings.Contains(tail, "FINAL_LINE") {
		t.Fatalf("Tail expected to contain FINAL_LINE, got %q", tail)
	}

	// Verify disk file has all 100 KB since 100 KB < 16 MB
	info, err := os.Stat(task.LogPath())
	if err != nil {
		t.Fatalf("os.Stat: %v", err)
	}
	if info.Size() < 100*1024 {
		t.Fatalf("disk file size %d smaller than expected 100KB", info.Size())
	}
}

func TestKillTaskLifecycle(t *testing.T) {
	mgr := New(Config{})
	defer mgr.Close()

	// 1. Lookup unknown task -> not found
	if _, ok := mgr.Get("missing"); ok {
		t.Fatalf("expected missing task to not be found")
	}

	task := &Task{
		ID:    "t-kill",
		Title: "killable",
		Wait:  0,
		Fn: func(ctx context.Context, output io.Writer, stdin io.Reader) (int, error) {
			<-ctx.Done()
			return 130, ctx.Err()
		},
	}

	if err := mgr.Add(task); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 2. Kill running task -> nil
	kTask, ok := mgr.Get("t-kill")
	if !ok {
		t.Fatalf("expected task to be found")
	}
	if err := kTask.Kill(); err != nil {
		t.Fatalf("Kill running task: %v", err)
	}

	<-task.Done()
	if task.Status() != api.TaskStatusCanceled {
		t.Fatalf("expected status %v, got %v", api.TaskStatusCanceled, task.Status())
	}

	// 3. Kill already canceled task -> ErrNotRunning
	if err := kTask.Kill(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("expected ErrNotRunning on finished task, got %v", err)
	}
}

func TestStandaloneTaskExecution(t *testing.T) {
	// A Task should be runnable directly without Store
	task := &Task{
		Title: "standalone",
		Wait:  time.Second,
		Fn: func(ctx context.Context, output io.Writer, stdin io.Reader) (int, error) {
			_, err := io.WriteString(output, "standalone output\n")
			return 0, err
		},
	}

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("standalone Run failed: %v", err)
	}
	if task.ID == "" {
		t.Fatal("expected UUID to be assigned")
	}
	if task.Status() != api.TaskStatusCompleted {
		t.Fatalf("expected status completed, got %v", task.Status())
	}
	tail, err := task.Tail(1)
	if err != nil {
		t.Fatalf("Tail failed: %v", err)
	}
	if !strings.Contains(tail, "standalone output") {
		t.Fatalf("expected output, got %q", tail)
	}
}
