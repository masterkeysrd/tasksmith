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
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
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

func allText(b *mock.Backend) string {
	return insertedAboveText(b) + surfaceText(b)
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
	store.FreezeTranscript()

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

	// Append other kinds; should not panic and return valid nodes
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

	store.SetHeaderMeta("1.0.0", "/workspace/clear-test")
	store.SetModelInfo("TestModel", "medium")
	store.FreezeTranscript()
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

	// Trigger ClearScreen on frozen dirty transcript
	store.ClearScreen()

	// Wait for clear to take effect and snapshot header to be reprinted in InsertedAbove
	var postClearText string
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		postClearText = allText(b.Backend)
		if b.ClearCalls > 0 && strings.Contains(postClearText, "TaskSmith v1.0.0") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if b.ClearCalls < 1 {
		t.Fatalf("expected backend Clear() to have been called at least once, got %d", b.ClearCalls)
	}
	if !strings.Contains(postClearText, "TaskSmith v1.0.0") {
		t.Fatalf("expected Header after clear on screen, got:\n%s", postClearText)
	}
	if strings.Contains(postClearText, "old message that should be cleared") {
		t.Fatalf("expected old messages to be cleared from scrollback, got:\n%s", postClearText)
	}
}

func TestTranscriptLiveDraftAndFreezeOnSubmit(t *testing.T) {
	store.ClearTranscript()
	defer store.ClearTranscript()

	store.SetHeaderMeta("1.0.0", "/workspace/live-test")
	store.SetModelInfo("model-alpha", "low")

	b := mock.New(80, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = trail.Run(ctx, Transcript(), trail.WithBackend(b))
	}()

	// 1. Initial render: Header is live on surface, scrollback (InsertedAbove) is empty
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(surfaceText(b), "model-alpha with low effort") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(surfaceText(b), "model-alpha with low effort") {
		t.Fatalf("expected live header on surface, got:\n%s", surfaceText(b))
	}
	if len(b.InsertedAbove) != 0 {
		t.Fatalf("expected InsertedAbove to be empty in draft mode, got %d buffers", len(b.InsertedAbove))
	}

	// 2. Append command output in draft mode: remains live on surface, NOT in InsertedAbove
	store.AppendCommandOutput("/model model-alpha", "Model set to model-alpha")
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(surfaceText(b), "Model set to model-alpha") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(surfaceText(b), "Model set to model-alpha") {
		t.Fatalf("expected command output live on surface, got:\n%s", surfaceText(b))
	}
	if len(b.InsertedAbove) != 0 {
		t.Fatalf("expected InsertedAbove to remain empty while in draft mode, got %d buffers", len(b.InsertedAbove))
	}

	// 3. Reactively update model info in place: live header updates even after command outputs
	store.SetModelInfo("model-beta", "high")
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(surfaceText(b), "model-beta with high effort") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(surfaceText(b), "model-beta with high effort") {
		t.Fatalf("expected live header to update model info in place, got:\n%s", surfaceText(b))
	}
	if len(b.InsertedAbove) != 0 {
		t.Fatalf("expected InsertedAbove to remain empty after model update, got %d buffers", len(b.InsertedAbove))
	}

	// 4. Freeze transcript (e.g. on first prompt send): freezes snapshot header and draft items into InsertedAbove
	store.FreezeTranscript()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(insertedAboveText(b), "Model set to model-alpha") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	aboveText := insertedAboveText(b)
	if !strings.Contains(aboveText, "model-beta with high effort") {
		t.Fatalf("expected frozen header in InsertedAbove after FreezeTranscript, got:\n%s", aboveText)
	}
	if !strings.Contains(aboveText, "Model set to model-alpha") {
		t.Fatalf("expected command output in InsertedAbove after FreezeTranscript, got:\n%s", aboveText)
	}
}

func TestTranscriptSessionSwitch(t *testing.T) {
	store.ClearTranscript()
	defer store.ClearTranscript()

	store.SetHeaderMeta("1.0.0", "/workspace/resumed")
	store.SetModelInfo("resumed-model", "high")

	b := mock.New(80, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = trail.Run(ctx, Transcript(), trail.WithBackend(b))
	}()

	// Switch session: snapshot header and enter frozen mode
	store.ResetForSessionSwitch()
	store.AppendTimelineEntry(api.TimelineEntry{
		ID:        "tl-entry-1",
		SessionID: "s-resumed",
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(insertedAboveText(b), "resumed-model with high effort") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	aboveText := insertedAboveText(b)
	if !strings.Contains(aboveText, "TaskSmith v1.0.0") || !strings.Contains(aboveText, "resumed-model with high effort") {
		t.Fatalf("expected resumed session header in InsertedAbove, got:\n%s", aboveText)
	}
}
