package components

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
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
		if strings.Contains(txt, "Explain goroutines") && strings.Contains(txt, "Thinking...") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	txt := surfaceText(b)
	if !strings.Contains(txt, "Explain goroutines") {
		t.Fatalf("expected message in frame, got: %q", txt)
	}
	if !strings.Contains(txt, "Thinking...") {
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
