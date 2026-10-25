package components

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
)

func insertedAboveText(b *mock.Backend) string {
	var sb strings.Builder
	for _, buf := range b.InsertedAbove {
		for y := 0; y < buf.Height; y++ {
			var line strings.Builder
			for x := 0; x < buf.Width; x++ {
				c := buf.CellAt(x, y)
				if c.Content == "" {
					line.WriteByte(' ')
				} else {
					line.WriteString(c.Content)
				}
			}
			trimmed := strings.TrimRight(line.String(), " ")
			if trimmed != "" {
				sb.WriteString(trimmed)
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
}

func TestTranscriptRendersHeaderAndRemountsOnReset(t *testing.T) {
	store.ClearTranscript()
	defer store.ClearTranscript()

	store.AppendHeader(store.HeaderData{
		Version:   "1.0.0",
		Model:     "TestModel",
		Thinking:  "medium",
		Workspace: "/workspace/initial",
	})

	b := mock.New(80, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = trail.Run(ctx, Transcript(), trail.WithBackend(b))
	}()

	// Wait for header to be committed to InsertedAbove static scrollback
	var initialText string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		initialText = insertedAboveText(b)
		if strings.Contains(initialText, "TaskSmith v1.0.0") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	for _, want := range []string{
		"TaskSmith v1.0.0",
		"TestModel with medium effort",
		"/workspace/initial",
	} {
		if !strings.Contains(initialText, want) {
			t.Fatalf("static scrollback missing %q, got:\n%s", want, initialText)
		}
	}

	// Reset transcript with a new header, which increments Epoch
	store.ResetTranscript(store.TranscriptItem{
		ID:   "header",
		Kind: store.TranscriptKindHeader,
		Data: store.HeaderData{
			Version:   "2.0.0",
			Model:     "NewModel",
			Thinking:  "high",
			Workspace: "/workspace/reset",
		},
	})

	// Wait for new header to be rendered and committed on remount
	var postResetText string
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		postResetText = insertedAboveText(b)
		if strings.Contains(postResetText, "TaskSmith v2.0.0") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	for _, want := range []string{
		"TaskSmith v2.0.0",
		"NewModel with high effort",
		"/workspace/reset",
	} {
		if !strings.Contains(postResetText, want) {
			t.Fatalf("static scrollback missing reset item %q, got:\n%s", want, postResetText)
		}
	}
}

func TestTranscriptPlaceholderKinds(t *testing.T) {
	store.ClearTranscript()
	defer store.ClearTranscript()

	// Append other kinds; should not panic and return nil nodes
	store.AppendTimelineEntry(api.TimelineEntry{ID: "tl-1"})
	store.AppendNotice("notice text")
	store.AppendError("err title", "err msg")
	store.AppendCommandOutput("/cmd", "output")

	b := mock.New(80, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = trail.Run(ctx, Transcript(), trail.WithBackend(b))
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.LastFrame().Surface != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Verify nothing broke and surface is clean
	if b.LastFrame().Surface == nil {
		t.Fatal("expected rendered surface")
	}
}

type clearMockBackend struct {
	*mock.Backend
}

func (c *clearMockBackend) Clear() {
	c.Backend.Clear()
	c.InsertedAbove = nil
}

func (c *clearMockBackend) InsertAbove(buf *backend.Buffer) {
	c.Backend.InsertAbove(buf)
}

func TestTranscriptClearReprintHeader(t *testing.T) {
	store.ClearTranscript()
	defer store.ClearTranscript()

	store.AppendHeader(store.HeaderData{
		Version:   "1.0.0",
		Model:     "TestModel",
		Thinking:  "medium",
		Workspace: "/workspace/clear-test",
	})
	store.AppendNotice("old message that should be cleared")

	rawBackend := mock.New(80, 24)
	b := &clearMockBackend{Backend: rawBackend}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = trail.Run(ctx, Transcript(), trail.WithBackend(b))
	}()

	// Wait for initial header and notice to be committed to InsertedAbove
	var initialText string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		initialText = insertedAboveText(b.Backend)
		if strings.Contains(initialText, "TaskSmith v1.0.0") && strings.Contains(initialText, "old message that should be cleared") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if !strings.Contains(initialText, "TaskSmith v1.0.0") || !strings.Contains(initialText, "old message that should be cleared") {
		t.Fatalf("expected initial header and notice in scrollback, got:\n%s", initialText)
	}

	// Trigger ClearScreen on dirty transcript
	store.ClearScreen()

	// Wait for clear to take effect and new header to be reprinted into InsertedAbove
	var postClearText string
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		postClearText = insertedAboveText(b.Backend)
		if b.ClearCalls > 0 && strings.Contains(postClearText, "TaskSmith v1.0.0") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if b.ClearCalls < 1 {
		t.Fatalf("expected backend Clear() to have been called at least once, got %d", b.ClearCalls)
	}
	if !strings.Contains(postClearText, "TaskSmith v1.0.0") {
		t.Fatalf("expected reprinted Header after clear in scrollback, got:\n%s", postClearText)
	}
	if strings.Contains(postClearText, "old message that should be cleared") {
		t.Fatalf("expected old messages to be cleared from scrollback, got:\n%s", postClearText)
	}
}
