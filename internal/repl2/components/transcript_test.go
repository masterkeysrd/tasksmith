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
