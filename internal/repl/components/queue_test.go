package components

import (
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/store"
)

func TestQueueComponent(t *testing.T) {
	store.ResetStream()
	defer store.ResetStream()

	render := renderComponent(t, 100)

	// When queue is empty, Queue renders nothing
	got := render(Queue())
	if got != "" {
		t.Fatalf("expected empty string when queue is empty, got %q", got)
	}

	// Seed queue with a message and a command
	store.SeedQueue([]api.QueueItem{
		{
			ID:   "item-1",
			Kind: api.QueueKindMessage,
			Payload: api.QueueMessagePayload{
				Message: api.Message{
					ID:   "item-1",
					Role: api.RoleUser,
					Content: []api.Block{
						{
							Type: api.BlockText,
							Text: "queued prompt message",
						},
					},
				},
			},
			CreatedAt: time.Now().UTC(),
		},
		{
			ID:   "item-2",
			Kind: api.QueueKindCommand,
			Payload: api.QueueCommandPayload{
				Command: "git status",
			},
			CreatedAt: time.Now().UTC(),
		},
	})

	got = render(Queue())
	if !strings.Contains(got, "> queued prompt message") {
		t.Fatalf("expected message item with > prefix, got %q", got)
	}
	if !strings.Contains(got, "! git status") {
		t.Fatalf("expected command item with ! prefix, got %q", got)
	}

	// Empty the queue
	store.SeedQueue(nil)
	got = render(Queue())
	if got != "" {
		t.Fatalf("expected empty string after queue cleared, got %q", got)
	}
}
