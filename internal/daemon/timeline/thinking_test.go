package timeline

import (
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestThinkingEventPersistence(t *testing.T) {
	tl, _, ctx := openTestTimeline(t, "thinking-session")
	entry := &Entry{Kind: api.TimelineKindThinking, Thinking: &api.ThinkingChange{To: &api.ThinkingSetting{Type: "toggle", Enabled: false}}}
	if err := tl.Append(ctx, entry); err != nil {
		t.Fatal(err)
	}
	loaded, err := tl.Get(ctx, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Thinking == nil || loaded.Thinking.To == nil || loaded.Thinking.To.Type != "toggle" || loaded.Thinking.To.Enabled {
		t.Fatalf("unexpected event: %+v", loaded.Thinking)
	}
	clone := loaded.Clone()
	clone.Thinking.To.Enabled = true
	if loaded.Thinking.To.Enabled {
		t.Fatal("clone changed original thinking event")
	}
	reset := &Entry{Kind: api.TimelineKindThinking, Thinking: &api.ThinkingChange{From: loaded.Thinking.To}}
	if err := tl.Append(ctx, reset); err != nil {
		t.Fatal(err)
	}
	loaded, err = tl.Get(ctx, reset.ID)
	if err != nil || loaded.Thinking == nil || loaded.Thinking.To != nil {
		t.Fatalf("reset event: %+v, %v", loaded, err)
	}
}
