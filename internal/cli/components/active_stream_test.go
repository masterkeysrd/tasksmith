package components

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func TestActiveStreamNilWhenIdle(t *testing.T) {
	store.ResetStream()
	defer store.ResetStream()

	render := renderComponent(t, 100)
	got := render(ActiveStream())
	if got != "" {
		t.Fatalf("expected empty string when stream is idle, got %q", got)
	}
}

func TestActiveStreamStreamingAndHint(t *testing.T) {
	store.ResetApp()
	store.ResetStream()
	defer store.ResetApp()
	defer store.ResetStream()

	if store.GetApp().Hint != store.DefaultHint {
		t.Fatalf("expected DefaultHint initially, got %q", store.GetApp().Hint)
	}

	b := mock.New(80, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = trail.Run(ctx, ActiveStream(), trail.WithBackend(b))
	}()

	time.Sleep(15 * time.Millisecond)
	if store.GetApp().Hint != store.DefaultHint {
		t.Fatalf("expected DefaultHint while stream idle, got %q", store.GetApp().Hint)
	}

	// Trigger streaming prompt via chunk event
	store.ApplyEvent(api.Event{
		Type: api.EventChunk,
		Payload: api.ChunkEvent{
			Chunk: api.Chunk{
				ID: "asst-1",
				Content: []api.Block{
					{Type: api.BlockText, Text: "Explain goroutines"},
				},
			},
		},
	})

	// Wait for StreamingHint to mount and update store hint
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if store.GetApp().Hint == "streaming • esc to cancel" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if store.GetApp().Hint != "streaming • esc to cancel" {
		t.Fatalf("expected streaming hint, got %q", store.GetApp().Hint)
	}

	// Wait for frame to contain assistant message and spinner
	for time.Now().Before(deadline) {
		txt := surfaceText(b)
		if strings.Contains(txt, "Explain goroutines") && strings.Contains(txt, "Generating...") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	txt := surfaceText(b)
	if !strings.Contains(txt, "Explain goroutines") {
		t.Fatalf("expected message in frame, got: %q", txt)
	}
	if !strings.Contains(txt, "Generating...") {
		t.Fatalf("expected spinner text in frame, got: %q", txt)
	}

	// Complete turn
	store.ApplyEvent(api.Event{
		Type:    api.EventCompleted,
		Payload: api.CompletedEvent{},
	})

	for time.Now().Before(deadline) {
		if store.GetApp().Hint == store.DefaultHint {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if store.GetApp().Hint != store.DefaultHint {
		t.Fatalf("expected DefaultHint restored after streaming reset, got %q", store.GetApp().Hint)
	}
}

func TestActiveStreamWaitingPermissionHidesSpinner(t *testing.T) {
	store.ResetApp()
	store.ResetStream()
	defer store.ResetApp()
	defer store.ResetStream()

	b := mock.New(80, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = trail.Run(ctx, ActiveStream(), trail.WithBackend(b))
	}()

	// Trigger streaming prompt via chunk event
	store.ApplyEvent(api.Event{
		Type: api.EventChunk,
		Payload: api.ChunkEvent{
			Chunk: api.Chunk{
				ID: "asst-1",
				Content: []api.Block{
					{Type: api.BlockText, Text: "Checking permissions..."},
				},
			},
		},
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		txt := surfaceText(b)
		if strings.Contains(txt, "Generating...") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	txt := surfaceText(b)
	if !strings.Contains(txt, "Generating...") {
		t.Fatalf("expected Generating... while streaming, got %q", txt)
	}

	// Trigger EventPermissionRequested -> WaitingPermission becomes true
	store.ApplyEvent(api.Event{
		Type: api.EventPermissionRequested,
		Payload: api.PermissionRequestedEvent{
			Permissions: []api.PermissionRequest{
				{
					ID:         "req-1",
					ToolCallID: "tc-1",
					ToolName:   "bash",
					Action:     api.ActionExec,
				},
			},
		},
	})

	for time.Now().Before(deadline) {
		txt = surfaceText(b)
		if !strings.Contains(txt, "Generating...") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	txt = surfaceText(b)
	if strings.Contains(txt, "Generating...") {
		t.Fatalf("expected Generating... spinner to be hidden when WaitingPermission is true, got %q", txt)
	}
	if !strings.Contains(txt, "Checking permissions...") {
		t.Fatalf("expected message to remain rendered, got %q", txt)
	}
}

func TestActiveStreamSpinnerStates(t *testing.T) {
	store.ResetApp()
	store.ResetStream()
	defer store.ResetApp()
	defer store.ResetStream()

	b := mock.New(80, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = trail.Run(ctx, ActiveStream(), trail.WithBackend(b))
	}()

	// 1. Initial chunk with reasoning block -> StatusThinking -> "Thinking..."
	store.ApplyEvent(api.Event{
		Type: api.EventChunk,
		Payload: api.ChunkEvent{
			Chunk: api.Chunk{
				ID: "asst-1",
				Content: []api.Block{
					{Type: api.BlockReasoning, Text: "Analyzing problem..."},
				},
			},
		},
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		txt := surfaceText(b)
		if strings.Contains(txt, "Thinking...") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	txt := surfaceText(b)
	if !strings.Contains(txt, "Thinking...") {
		t.Fatalf("expected Thinking... spinner, got %q", txt)
	}

	// 2. Chunk with text -> StatusGenerating -> "Generating..."
	store.ApplyEvent(api.Event{
		Type: api.EventChunk,
		Payload: api.ChunkEvent{
			Chunk: api.Chunk{
				ID: "asst-1",
				Content: []api.Block{
					{Type: api.BlockText, Text: "Here is the plan."},
				},
			},
		},
	})

	for time.Now().Before(deadline) {
		txt = surfaceText(b)
		if strings.Contains(txt, "Generating...") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	txt = surfaceText(b)
	if !strings.Contains(txt, "Generating...") {
		t.Fatalf("expected Generating... spinner, got %q", txt)
	}

	// 3. StatusChangedEvent -> StatusExecuting -> "Executing..."
	store.ApplyEvent(api.Event{
		Type: api.EventStatusChanged,
		Payload: api.StatusChangedEvent{
			Status: api.StatusExecuting,
		},
	})

	for time.Now().Before(deadline) {
		txt = surfaceText(b)
		if strings.Contains(txt, "Executing...") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	txt = surfaceText(b)
	if !strings.Contains(txt, "Executing...") {
		t.Fatalf("expected Executing... spinner, got %q", txt)
	}

	// 4. StatusChangedEvent -> StatusThinking -> "Thinking..."
	store.ApplyEvent(api.Event{
		Type: api.EventStatusChanged,
		Payload: api.StatusChangedEvent{
			Status: api.StatusThinking,
		},
	})

	for time.Now().Before(deadline) {
		txt = surfaceText(b)
		if strings.Contains(txt, "Thinking...") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	txt = surfaceText(b)
	if !strings.Contains(txt, "Thinking...") {
		t.Fatalf("expected Thinking... spinner after tool completion, got %q", txt)
	}
}

func TestActiveStreamPinnedChecklistWhileIdle(t *testing.T) {
	store.ResetStream()
	defer store.ResetStream()

	// 1. Seed todos with incomplete items
	store.SeedTodos([]api.TodoItem{
		{
			Content:    "Implement todos tool",
			ActiveText: "Implementing todos tool",
			Status:     "in_progress",
		},
		{
			Content:    "Run unit tests",
			ActiveText: "Running unit tests",
			Status:     "pending",
		},
	})

	render := renderComponent(t, 100)
	got := render(ActiveStream())
	if got == "" {
		t.Fatal("expected ActiveStream to render pinned checklist when incomplete todos exist")
	}
	if !strings.Contains(got, "2 todos (0 done, 1 in progress, 1 pending)") {
		t.Fatalf("expected header with count, got %q", got)
	}
	if !strings.Contains(got, "⎿") || !strings.Contains(got, "◼ Implement todos tool") {
		t.Fatalf("expected row 1 with ⎿ and in_progress marker, got %q", got)
	}
	if !strings.Contains(got, "◻ Run unit tests") {
		t.Fatalf("expected row 2 with pending marker, got %q", got)
	}

	// 2. Mark all as completed -> pinned checklist hides automatically
	store.SeedTodos([]api.TodoItem{
		{
			Content:    "Implement todos tool",
			ActiveText: "Implementing todos tool",
			Status:     "completed",
		},
		{
			Content:    "Run unit tests",
			ActiveText: "Running unit tests",
			Status:     "completed",
		},
	})

	got = render(ActiveStream())
	if got != "" {
		t.Fatalf("expected ActiveStream to be empty when all todos completed, got %q", got)
	}
}

func TestActiveStreamClearsChecklistOnNewDraft(t *testing.T) {
	store.ResetSession()
	store.ResetStream()
	store.ClearTranscript()
	defer store.ResetSession()
	defer store.ResetStream()
	defer store.ClearTranscript()

	store.SeedTodos([]api.TodoItem{{Content: "Previous task", Status: "pending"}})
	render := renderComponent(t, 100)
	if got := render(ActiveStream()); !strings.Contains(got, "Previous task") {
		t.Fatalf("expected previous checklist, got %q", got)
	}
	store.NewDraftSession("")
	if got := render(ActiveStream()); strings.Contains(got, "Previous task") || strings.Contains(got, "todos") {
		t.Fatalf("checklist remained after new draft: %q", got)
	}
}

func TestActiveStreamStreamingSpinnerWithActiveTextAndChecklist(t *testing.T) {
	store.ResetStream()
	defer store.ResetStream()

	// Seed todos
	store.SeedTodos([]api.TodoItem{
		{
			Content:    "Implement todos tool",
			ActiveText: "Implementing todos tool",
			Status:     "in_progress",
		},
		{
			Content:    "Run unit tests",
			ActiveText: "Running unit tests",
			Status:     "pending",
		},
	})

	// Trigger streaming status
	store.ApplyEvent(api.Event{
		Type: api.EventStatusChanged,
		Payload: api.StatusChangedEvent{
			Status: api.StatusThinking,
		},
	})

	render := renderComponent(t, 100)
	got := render(ActiveStream())
	if !strings.Contains(got, "Thinking (Implementing todos tool)...") {
		t.Fatalf("expected spinner with active text 'Thinking (Implementing todos tool)...', got %q", got)
	}
	if !strings.Contains(got, "⎿") || !strings.Contains(got, "◼ Implement todos tool") {
		t.Fatalf("expected checklist row 1 underneath spinner, got %q", got)
	}
	if !strings.Contains(got, "◻ Run unit tests") {
		t.Fatalf("expected checklist row 2 underneath spinner, got %q", got)
	}
}

func TestDelegatedSpinnerText(t *testing.T) {
	running := api.Session{
		ID:       "sub-1",
		Status:   api.StatusThinking,
		Activity: "Staging changes",
		Config:   api.SessionConfig{Agent: "main"},
	}
	noActivity := api.Session{ID: "sub-2", Status: api.StatusThinking, Config: api.SessionConfig{Agent: "explore"}}
	awaiting := api.Session{ID: "sub-3", Status: api.StatusWaitingPermission, Config: api.SessionConfig{Agent: "general"}}

	tests := []struct {
		name       string
		subagents  []api.Session
		activeText string
		want       string
	}{
		{
			name:      "activity wins",
			subagents: []api.Session{running},
			want:      "Waiting on subagent (main: Staging changes)",
		},
		{
			name:       "active todo fills a quiet child",
			subagents:  []api.Session{noActivity},
			activeText: "Running unit tests",
			want:       "Waiting on subagent (explore: Running unit tests)",
		},
		{
			name:      "status is the last resort",
			subagents: []api.Session{awaiting},
			want:      "Waiting on subagent (general: Waiting for permission)",
		},
		{
			name:      "multiple children count",
			subagents: []api.Session{running, noActivity},
			want:      "Waiting on 2 subagents (main: Staging changes)",
		},
		{
			name:      "bare status is the fallback",
			subagents: []api.Session{{ID: "sub-4", Status: api.StatusExecuting, Config: api.SessionConfig{Agent: "worker"}}},
			want:      "Waiting on subagent (worker: Executing)",
		},
		{
			name:      "unknown persona still reads naturally",
			subagents: []api.Session{{ID: "sub-5", Status: api.StatusExecuting}},
			want:      "Waiting on subagent (subagent: Executing)",
		},
		{
			name:      "no children",
			subagents: nil,
			want:      "Waiting on subagent...",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := delegatedSpinnerText(tc.subagents, tc.activeText); got != tc.want {
				t.Fatalf("delegatedSpinnerText() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStreamingHintDelegatedWording(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	render := renderComponent(t, 80)
	render(StreamingHint(StreamingHintProps{Delegated: true}))

	if got := store.GetApp().Hint; got != "subagent running • esc to cancel" {
		t.Fatalf("expected delegated hint, got %q", got)
	}
}

func TestStreamingHintDefaultWording(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	render := renderComponent(t, 80)
	render(StreamingHint(StreamingHintProps{}))

	if got := store.GetApp().Hint; got != "streaming • esc to cancel" {
		t.Fatalf("expected streaming hint, got %q", got)
	}
}

// A delegated command must not render as the main agent thinking. In production
// the daemon records the invocation on the parent timeline and starts no parent
// turn, so a user timeline entry plus a running child is the real state.
func TestActiveStreamDelegatedSpinnerLabel(t *testing.T) {
	store.ResetApp()
	store.ResetSession()
	store.ResetStream()
	defer store.ResetApp()
	defer store.ResetSession()
	defer store.ResetStream()

	b := mock.New(100, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	store.UpsertSubagent(api.Session{
		ID:       "sub-1",
		ParentID: "sess-1",
		Title:    "/commit",
		Status:   api.StatusThinking,
		Activity: "Staging changes",
		Config:   api.SessionConfig{Agent: "main"},
	})

	go func() {
		_ = trail.Run(ctx, ActiveStream(), trail.WithBackend(b))
	}()

	store.ApplyEvent(api.Event{
		Type: api.EventEntryAdded,
		Payload: api.EntryAddedEvent{
			SessionID: "sess-1",
			Entry: api.TimelineEntry{
				ID:   "entry-1",
				Kind: api.TimelineKindMessage,
				Message: &api.Message{
					ID:        "cmd-msg-1",
					Role:      api.RoleUser,
					CreatedAt: time.Now().UTC(),
					Content:   []api.Block{{Type: api.BlockText, Text: "/commit"}},
				},
			},
		},
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(surfaceText(b), "Waiting on subagent") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	txt := surfaceText(b)
	if !strings.Contains(txt, "Waiting on subagent (main: Staging changes)") {
		t.Fatalf("expected delegated spinner label in frame, got: %q", txt)
	}
	if strings.Contains(txt, "Thinking...") {
		t.Fatalf("delegated turn must not render as local thinking, got: %q", txt)
	}
}
