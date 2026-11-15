package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
)

func toolDefinition(name string) tool.Definition {
	return tool.Definition{Name: name, Title: name}
}

// fakeRunner records Exec requests and blocks each call until released, so
// backgrounding and cancellation can be exercised deterministically without
// real sleeping processes.
type fakeRunner struct {
	requests chan std.ExecRequest
	release  chan struct{}
}

func (f *fakeRunner) Exec(ctx context.Context, req std.ExecRequest) (*std.ExecResult, error) {
	f.requests <- req
	select {
	case <-f.release:
		return &std.ExecResult{ExitCode: 0}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TestCustomToolBackgroundsWithFakeRunner verifies the task-promotion contract
// purely through the CommandRunner seam: the call returns at the wait window
// with a backgrounded task, and killing the task cancels the runner context.
func TestCustomToolBackgroundsWithFakeRunner(t *testing.T) {
	previous := customToolWait
	customToolWait = 25 * time.Millisecond
	t.Cleanup(func() { customToolWait = previous })

	runner := &fakeRunner{
		requests: make(chan std.ExecRequest, 4),
		release:  make(chan struct{}),
	}
	store := tasks.New(tasks.Config{Context: context.Background()})
	t.Cleanup(func() { _ = store.Close() })

	ct := &customTool{
		def:     toolDefinition("hang_tool"),
		command: []string{"never", "--runs"},
		runner:  runner,
		tasks:   store,
	}

	res, err := ct.Execute(context.Background(), &message.ToolCall{ID: "hang-1", Name: "hang_tool"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Content.Text(), "<background_task") {
		t.Fatalf("expected background_task notice, got %q", res.Content.Text())
	}
	if req := <-runner.requests; req.Name != "never" || len(req.Args) != 1 || req.Args[0] != "--runs" {
		t.Fatalf("runner received %+v, want argv [never --runs]", req)
	}

	tasksSnaps := store.List()
	if len(tasksSnaps) != 1 || !tasksSnaps[0].Backgrounded {
		t.Fatalf("expected one backgrounded task, got %+v", tasksSnaps)
	}
	task, ok := store.Get(tasksSnaps[0].ID)
	if !ok {
		t.Fatal("task not retrievable from store")
	}
	if err := task.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	select {
	case <-task.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("killed task did not finish")
	}
	if got := task.Status(); got != api.TaskStatusCanceled {
		t.Fatalf("status after kill = %v, want %v", got, api.TaskStatusCanceled)
	}
	close(runner.release)
}

// TestCustomToolAutoBackgroundsAtWaitWindow verifies that a custom tool still
// running when the wait window elapses returns a background-task notice with a
// registered, killable task instead of blocking the tool call until the
// process finishes.
func TestCustomToolAutoBackgroundsAtWaitWindow(t *testing.T) {
	previous := customToolWait
	customToolWait = 50 * time.Millisecond
	t.Cleanup(func() { customToolWait = previous })

	dir := t.TempDir()
	sb, err := sandbox.New(sandbox.Config{Mode: sandbox.ModeLocal, WorkDir: dir})
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	t.Cleanup(func() { _ = sb.Close() })

	stdLib, err := std.New(std.Config{Sandbox: sb, Tracker: std.NewMemoryTracker(), Logger: log.Default()})
	if err != nil {
		t.Fatalf("std.New: %v", err)
	}
	store := tasks.New(tasks.Config{Context: context.Background(), OutputsDir: filepath.Join(dir, "outputs")})
	t.Cleanup(func() { _ = store.Close() })

	ct := &customTool{
		def:     toolDefinition("slow_tool"),
		command: []string{"sh", "-c", "echo starting; sleep 30"},
		runner:  stdLib,
		tasks:   store,
	}

	start := time.Now()
	res, err := ct.Execute(context.Background(), &message.ToolCall{ID: "slow-1", Name: "slow_tool"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	elapsed := time.Since(start)

	text := res.Content.Text()
	if !strings.Contains(text, "<background_task") {
		t.Fatalf("expected background_task notice, got %q", text)
	}
	if !strings.Contains(text, "do not accept interactive input") {
		t.Fatalf("expected notice to state interactive input is unsupported, got %q", text)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Execute blocked for %v; expected return at the wait window", elapsed)
	}

	var snap api.Task
	found := false
	for _, task := range store.List() {
		if task.Kind == "custom_tool" {
			snap, found = task, true
			break
		}
	}
	if !found {
		t.Fatal("expected custom_tool task registered in store")
	}
	if snap.Status != api.TaskStatusRunning || !snap.Backgrounded {
		t.Fatalf("expected running backgrounded task, got %+v", snap)
	}
	if !strings.Contains(snap.Command, "sleep 30") {
		t.Errorf("task command = %q, want the rendered argv", snap.Command)
	}
	if snap.LogPath == "" {
		t.Errorf("expected task log path for backgrounded custom tool")
	}

	task, ok := store.Get(snap.ID)
	if !ok {
		t.Fatalf("task %s not retrievable", snap.ID)
	}
	if err := task.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	select {
	case <-task.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("killed task did not finish")
	}
	if got := task.Status(); got != api.TaskStatusCanceled {
		t.Fatalf("status after kill = %v, want %v", got, api.TaskStatusCanceled)
	}
}

// TestCustomToolTaskPathDeliversStdinPayload verifies the JSON argument payload
// still reaches the process when execution is promoted to a task, and that the
// exit status and stderr-free output surface like the direct path.
func TestCustomToolTaskPathDeliversStdinPayload(t *testing.T) {
	dir := t.TempDir()
	sb, err := sandbox.New(sandbox.Config{Mode: sandbox.ModeLocal, WorkDir: dir})
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	t.Cleanup(func() { _ = sb.Close() })

	stdLib, err := std.New(std.Config{Sandbox: sb, Tracker: std.NewMemoryTracker(), Logger: log.Default()})
	if err != nil {
		t.Fatalf("std.New: %v", err)
	}
	store := tasks.New(tasks.Config{Context: context.Background()})
	t.Cleanup(func() { _ = store.Close() })

	ct := &customTool{
		def:     toolDefinition("cat_tool"),
		command: []string{"cat"},
		runner:  stdLib,
		tasks:   store,
	}

	res, err := ct.Execute(context.Background(), &message.ToolCall{
		ID:   "cat-1",
		Name: "cat_tool",
		Args: map[string]any{"key": "value"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	text := res.Content.Text()
	if !strings.Contains(text, `"key":"value"`) {
		t.Fatalf("expected JSON payload on stdin, got %q", text)
	}
	if store.List() != nil {
		t.Fatalf("completed task must not appear in background list: %+v", store.List())
	}
}

// TestCustomToolTaskPathExitCodeError verifies a non-zero exit through the task
// path surfaces as an error carrying the process output, matching the direct
// path's contract.
func TestCustomToolTaskPathExitCodeError(t *testing.T) {
	dir := t.TempDir()
	sb, err := sandbox.New(sandbox.Config{Mode: sandbox.ModeLocal, WorkDir: dir})
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	t.Cleanup(func() { _ = sb.Close() })

	stdLib, err := std.New(std.Config{Sandbox: sb, Tracker: std.NewMemoryTracker(), Logger: log.Default()})
	if err != nil {
		t.Fatalf("std.New: %v", err)
	}
	store := tasks.New(tasks.Config{Context: context.Background()})
	t.Cleanup(func() { _ = store.Close() })

	ct := &customTool{
		def:     toolDefinition("fail_tool"),
		command: []string{"sh", "-c", "echo boom; exit 3"},
		runner:  stdLib,
		tasks:   store,
	}

	_, err = ct.Execute(context.Background(), &message.ToolCall{ID: "fail-1", Name: "fail_tool"})
	if err == nil {
		t.Fatal("expected error for exit code 3")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error should carry process output, got %v", err)
	}
}

func TestReadOnlyExecutionContextTag(t *testing.T) {
	ctx := context.Background()
	if ReadOnlyExecutionFromContext(ctx) {
		t.Error("plain context should not report read-only execution")
	}
	noOp := WithReadOnlyExecution(ctx, false)
	if ReadOnlyExecutionFromContext(noOp) {
		t.Error("WithReadOnlyExecution(false) must not mark context read-only")
	}
	ro := WithReadOnlyExecution(ctx, true)
	if !ReadOnlyExecutionFromContext(ro) {
		t.Error("WithReadOnlyExecution(true) must mark context read-only")
	}
	if ReadOnlyExecutionFromContext(ctx) {
		t.Error("parent context must be unaffected")
	}
}
