package command

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/testenv"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/store"
)

func surfaceText(b *mock.Backend) string {
	frame := b.LastFrame().Surface
	if frame == nil {
		return ""
	}
	var sb strings.Builder
	for y := 0; y < frame.Height; y++ {
		var line strings.Builder
		for x := 0; x < frame.Width; x++ {
			c := frame.CellAt(x, y)
			if c.Content != "" {
				line.WriteString(c.Content)
			} else {
				line.WriteByte(' ')
			}
		}
		sb.WriteString(strings.TrimRight(line.String(), " "))
		sb.WriteByte('\n')
	}
	return sb.String()
}

func renderNode(t *testing.T, node trail.Node) string {
	t.Helper()
	env := testenv.Default(100, 24)
	t.Cleanup(env.Close)
	container := element.NewBox(env.Document())
	env.Mount(container)
	kitex.Render(node, container)
	env.Flush()
	return strings.TrimSpace(env.DumpText())
}

func TestTasksBrowserEmptyAndToggle(t *testing.T) {
	store.ResetSession()
	store.ClearSlot()
	defer store.ResetSession()
	defer store.ClearSlot()

	// 1. When no tasks at all
	got := renderNode(t, TasksBrowser("slot-1"))
	if !strings.Contains(got, "Background Tasks") || !strings.Contains(got, "No background tasks.") {
		t.Fatalf("expected empty background tasks message, got %q", got)
	}

	// 2. When only completed tasks exist (none running)
	store.UpsertTask(api.Task{
		ID:         "t-1",
		Command:    "make build",
		Status:     api.TaskStatusCompleted,
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
	})

	got = renderNode(t, TasksBrowser("slot-1"))
	if !strings.Contains(got, "No running background tasks.") {
		t.Fatalf("expected 'No running background tasks.' message, got %q", got)
	}

	// 3. When running task exists
	store.UpsertTask(api.Task{
		ID:        "t-2",
		Title:     "Dev Server",
		Command:   "go run main.go",
		Status:    api.TaskStatusRunning,
		StartedAt: time.Now(),
	})

	got = renderNode(t, TasksBrowser("slot-1"))
	if !strings.Contains(got, "Dev Server") || !strings.Contains(got, "go run main.go") {
		t.Fatalf("expected running task in picker, got %q", got)
	}
}

func TestTasksBrowserKillCompletedTaskNotice(t *testing.T) {
	store.ResetSession()
	store.ClearSlot()
	defer store.ResetSession()
	defer store.ClearSlot()

	store.UpsertTask(api.Task{
		ID:         "t-1",
		Command:    "make build",
		Status:     api.TaskStatusCompleted,
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
	})
	store.UpsertTask(api.Task{
		ID:        "t-2",
		Command:   "npm run dev",
		Status:    api.TaskStatusRunning,
		StartedAt: time.Now(),
	})

	b := mock.New(100, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return TasksBrowser("slot-1")
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()

	out := surfaceText(b)
	if !strings.Contains(out, "npm run dev") {
		t.Fatalf("expected running task t-2: %s", out)
	}

	// Press 'a' to toggle show all (to see completed tasks)
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'a', Text: "a"}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "make build") {
		t.Fatalf("expected make build in all tasks: %s", out)
	}

	// Selected task is t-1 (first item: make build, completed).
	// Press 'x' on completed task.
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'x', Text: "x"}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "Task is already completed.") {
		t.Fatalf("expected 'Task is already completed.' notice: %s", out)
	}

	// Press down to clear notice
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyDown}})
	inst.Frame()
	out = surfaceText(b)
	if strings.Contains(out, "Task is already completed.") {
		t.Fatalf("expected notice to be cleared after down: %s", out)
	}

	// Now on t-2 (running task), press 'x'
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'x', Text: "x"}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "Kill background task") {
		t.Fatalf("expected confirm prompt for running task: %s", out)
	}
}

func TestTaskOutputViewerKillCompletedNotice(t *testing.T) {
	store.ResetSession()
	defer store.ResetSession()

	task := api.Task{
		ID:         "t-finished",
		Command:    "echo done",
		Status:     api.TaskStatusCompleted,
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
	}

	b := mock.New(100, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return taskOutputViewer(taskOutputProps{task: task})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()

	out := surfaceText(b)
	if strings.Contains(out, "Task is already completed.") {
		t.Fatalf("notice should not be visible initially")
	}

	// Press 'x'
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'x', Text: "x"}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "Task is already completed.") {
		t.Fatalf("expected notice after pressing x: %s", out)
	}

	// Press any key (e.g. space) to clear
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeySpace, Text: " "}})
	inst.Frame()
	out = surfaceText(b)
	if strings.Contains(out, "Task is already completed.") {
		t.Fatalf("expected notice cleared: %s", out)
	}
}

func TestTasksBrowserViewerStaysOpenWhenTaskCompletes(t *testing.T) {
	store.ResetSession()
	store.ClearSlot()
	defer store.ResetSession()
	defer store.ClearSlot()

	task := api.Task{
		ID:        "t-live",
		Title:     "Build Job",
		Command:   "go build .",
		Status:    api.TaskStatusRunning,
		StartedAt: time.Now(),
	}
	store.UpsertTask(task)

	b := mock.New(100, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = trail.Run(ctx, TasksBrowser("slot-1"), trail.WithBackend(b))
	}()

	// Wait for running task to appear in list
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(surfaceText(b), "Build Job") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(surfaceText(b), "Build Job") {
		t.Fatalf("expected running task in list: %s", surfaceText(b))
	}

	// Press enter to view output
	b.PostEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(surfaceText(b), "Build Job (running):") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(surfaceText(b), "Build Job (running):") {
		t.Fatalf("expected taskOutputViewer to open: %s", surfaceText(b))
	}

	// Task completes in the background (visibleTasks becomes empty since showAll is false)
	task.Status = api.TaskStatusCompleted
	task.FinishedAt = time.Now()
	store.UpsertTask(task)

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(surfaceText(b), "Build Job (completed):") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	out := surfaceText(b)
	if !strings.Contains(out, "Build Job (completed):") {
		t.Fatalf("expected taskOutputViewer to stay open when task finishes: %s", out)
	}
	if strings.Contains(out, "No running background tasks.") {
		t.Fatalf("expected empty tasks view not to preempt viewer: %s", out)
	}

	// Press esc to return to tasks list
	b.PostEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEscape}})

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(surfaceText(b), "No running background tasks.") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(surfaceText(b), "No running background tasks.") {
		t.Fatalf("expected empty view after esc: %s", surfaceText(b))
	}
}
