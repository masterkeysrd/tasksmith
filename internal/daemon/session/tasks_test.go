package session

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
)

func TestSessionTasksInitAndBackgroundUpdate(t *testing.T) {
	ctx := context.Background()
	_, _, _, pub, mgr := setupTestEnv(t, nil)

	sMeta, _, err := mgr.Create(ctx, CreateParams{Title: "Tasks Session"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	m := mgr.(*manager)
	m.mu.Lock()
	sess := m.sessions[sMeta.ID]
	m.mu.Unlock()

	if err := sess.initResources(ctx); err != nil {
		t.Fatalf("initResources: %v", err)
	}
	if sess.Tasks == nil {
		t.Fatal("expected sess.Tasks to be initialized")
	}

	task := &tasks.Task{
		Kind:    "bash",
		Title:   "echo test",
		Command: "echo hello",
		Wait:    0,
		Fn: func(ctx context.Context, output io.Writer, stdin io.Reader) (int, error) {
			_, _ = output.Write([]byte("hello background\n"))
			return 0, nil
		},
	}

	if err := sess.Tasks.Run(ctx, task); err != nil {
		t.Fatalf("Tasks.Run: %v", err)
	}

	<-task.Done()

	// Wait briefly for onTaskUpdate to propagate
	time.Sleep(20 * time.Millisecond)

	sess.Lock()
	stateTasks := sess.State.Tasks
	sess.Unlock()

	if len(stateTasks) != 1 {
		t.Fatalf("expected 1 task in State.Tasks, got %d", len(stateTasks))
	}
	if stateTasks[0].ID != task.ID {
		t.Fatalf("expected task ID %q, got %q", task.ID, stateTasks[0].ID)
	}
	if stateTasks[0].Status != tasks.StatusCompleted {
		t.Fatalf("expected task status completed, got %v", stateTasks[0].Status)
	}

	// Verify TaskUpdatedEvent was published
	events := pub.recorded()
	var foundEvent bool
	for _, ev := range events {
		if tue, ok := ev.(TaskUpdatedEvent); ok && tue.Task.ID == task.ID {
			foundEvent = true
			if tue.Task.Status != tasks.StatusCompleted && tue.Task.Status != tasks.StatusRunning {
				t.Fatalf("unexpected status in TaskUpdatedEvent: %v", tue.Task.Status)
			}
		}
	}
	if !foundEvent {
		t.Fatal("expected TaskUpdatedEvent to be published")
	}
}

func TestSessionEnqueueAndInbox(t *testing.T) {
	sess := &Session{
		State: NewState(),
		wg:    &sync.WaitGroup{},
	}

	// 1. When Running is true, Enqueue should append to inbox
	sess.Lock()
	sess.Running = true
	sess.Unlock()

	msg1 := message.NewUserText("msg1")
	msg2 := message.NewUserText("msg2")
	sess.Enqueue(msg1)
	sess.Enqueue(msg2)

	queued := sess.popInbox()
	if len(queued) != 2 {
		t.Fatalf("expected 2 messages in inbox, got %d", len(queued))
	}
	if msgText(queued[0]) != "msg1" || msgText(queued[1]) != "msg2" {
		t.Fatalf("unexpected messages in inbox: %+v", queued)
	}

	// After popInbox, inbox should be empty
	if after := sess.popInbox(); len(after) != 0 {
		t.Fatalf("expected empty inbox after pop, got %d", len(after))
	}

	// 2. When WaitingPermission is true, Enqueue should append to inbox
	sess.Lock()
	sess.Running = false
	sess.WaitingPermission = true
	sess.Unlock()

	sess.Enqueue(message.NewUserText("msg3"))
	queued2 := sess.popInbox()
	if len(queued2) != 1 || msgText(queued2[0]) != "msg3" {
		t.Fatalf("expected msg3 in inbox when waiting permission, got %+v", queued2)
	}
}

func TestSessionOnTaskUpdateCompletedNotification(t *testing.T) {
	ctx := context.Background()
	_, _, _, pub, mgr := setupTestEnv(t, nil)

	sMeta, _, err := mgr.Create(ctx, CreateParams{Title: "Notification Session"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	m := mgr.(*manager)
	m.mu.Lock()
	sess := m.sessions[sMeta.ID]
	m.mu.Unlock()

	if err := sess.initResources(ctx); err != nil {
		t.Fatalf("initResources: %v", err)
	}

	// Simulate session running so Enqueue queues into inbox
	sess.Lock()
	sess.Running = true
	sess.Unlock()

	task := &tasks.Task{
		Kind:    "download",
		Title:   "fetch archive",
		Command: "curl https://example.com/archive.tar.gz",
		Wait:    0,
		Fn: func(ctx context.Context, output io.Writer, stdin io.Reader) (int, error) {
			_, _ = output.Write([]byte("download complete: 100%\n"))
			return 0, nil
		},
	}

	if err := sess.Tasks.Run(ctx, task); err != nil {
		t.Fatalf("Tasks.Run: %v", err)
	}
	<-task.Done()

	// Wait briefly for onTaskUpdate to enqueue notification
	time.Sleep(20 * time.Millisecond)

	queued := sess.popInbox()
	if len(queued) != 1 {
		t.Fatalf("expected 1 notification in inbox, got %d", len(queued))
	}

	content := msgText(queued[0])
	if !strings.Contains(content, "<background_task_completed") {
		t.Fatalf("expected notification format, got %q", content)
	}
	if !strings.Contains(content, "task_id=\""+task.ID+"\"") {
		t.Fatalf("expected task_id in notification, got %q", content)
	}
	if !strings.Contains(content, "download complete: 100%") {
		t.Fatalf("expected tail in notification, got %q", content)
	}

	// Check published events
	events := pub.recorded()
	var publishedTaskEvent bool
	for _, ev := range events {
		if tue, ok := ev.(TaskUpdatedEvent); ok && tue.Task.ID == task.ID {
			publishedTaskEvent = true
			break
		}
	}
	if !publishedTaskEvent {
		t.Fatal("expected TaskUpdatedEvent to be published")
	}
}

func TestSessionBackgroundNotificationDoesNotAppearInTimeline(t *testing.T) {
	ctx := context.Background()
	prov := &fakeProvider{
		chunkHandler: func(_ context.Context, _ *llm.Request) ([]message.AssistantChunk, error) {
			return []message.AssistantChunk{
				{
					BaseChunk: message.BaseChunk{ID: "asst-notify-reply"},
					Content: []message.Block{
						&message.TextBlock{Text: "Noted background task completion."},
					},
					Done: true,
				},
			}, nil
		},
	}

	_, _, _, _, mgr := setupTestEnv(t, prov)

	sMeta, _, err := mgr.Create(ctx, CreateParams{Title: "Timeline Session"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	m := mgr.(*manager)
	m.mu.Lock()
	sess := m.sessions[sMeta.ID]
	m.mu.Unlock()

	if err := sess.initResources(ctx); err != nil {
		t.Fatalf("initResources: %v", err)
	}

	// Enqueue notification directly while session is idle
	sess.Enqueue(message.NewUserText("<background_task_completed task_id=\"t-1\" status=\"completed\" exit_code=\"0\">\noutput\n</background_task_completed>"))

	// Wait for the turn to finish
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait error: %v", err)
	}

	// Verify timeline entries
	entries, err := sess.Timeline.List(ctx)
	if err != nil {
		t.Fatalf("Timeline.List error: %v", err)
	}

	var hasAssistant bool
	for _, entry := range entries {
		if entry.Kind == timeline.KindMessage && entry.Message != nil {
			txt := msgText(entry.Message)
			if strings.Contains(txt, "<background_task_completed") {
				t.Fatalf("found background task completed notification in timeline: %+v", entry)
			}
			if strings.Contains(txt, "Noted background task completion") {
				hasAssistant = true
			}
		}
	}

	if !hasAssistant {
		t.Fatal("expected assistant response in timeline")
	}
}
