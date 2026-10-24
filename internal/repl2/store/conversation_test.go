package store

import (
	"errors"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestConversationOptimisticUserAndConfirmation(t *testing.T) {
	ResetStream()
	ClearTranscript()
	defer ResetStream()
	defer ClearTranscript()

	acc := newConversationAccumulator()
	acc.setSessionID("sess-1")

	// 1. Add pending user message
	acc.addPendingUser("user-msg-1", "Hello assistant")

	st := GetStream()
	if !st.Streaming {
		t.Fatal("expected Streaming to be true with pending user message")
	}
	if st.Messages == nil || len(*st.Messages) != 1 {
		t.Fatalf("expected 1 stream message, got %+v", st.Messages)
	}
	msg := (*st.Messages)[0]
	if msg.ID != "user-msg-1" || msg.Role != api.RoleUser {
		t.Fatalf("unexpected message: %+v", msg)
	}
	if len(msg.Parts) != 1 || msg.Parts[0].Text == nil || msg.Parts[0].Text.Text != "Hello assistant" {
		t.Fatalf("unexpected message parts: %+v", msg.Parts)
	}

	// Transcript should not yet have the message
	tr := GetTranscript()
	if tr.Items != nil && len(*tr.Items) > 0 {
		t.Fatalf("expected empty transcript before confirmation, got %d items", len(*tr.Items))
	}

	// 2. Confirm message via EventEntryAdded
	acc.applyEvent(api.Event{
		Type: api.EventEntryAdded,
		Payload: api.EntryAddedEvent{
			SessionID: "sess-1",
			Entry: api.TimelineEntry{
				ID:   "entry-1",
				Kind: api.TimelineKindMessage,
				Message: &api.Message{
					ID:        "user-msg-1",
					Role:      api.RoleUser,
					CreatedAt: time.Now(),
					Content: []api.Block{
						{Type: api.BlockText, Text: "Hello assistant"},
					},
				},
			},
		},
	})

	// Message should now be flushed to Transcript
	tr = GetTranscript()
	if tr.Items == nil || len(*tr.Items) != 1 {
		t.Fatalf("expected 1 transcript item after confirmation, got %+v", tr.Items)
	}
	item := (*tr.Items)[0]
	if item.Kind != TranscriptKindMessage {
		t.Fatalf("expected item kind %q, got %q", TranscriptKindMessage, item.Kind)
	}
	confMsg, ok := item.Data.(Message)
	if !ok || confMsg.ID != "user-msg-1" {
		t.Fatalf("unexpected item data: %+v", item.Data)
	}

	// And pending user message should be cleared from StreamState
	st = GetStream()
	if st.Messages != nil && len(*st.Messages) > 0 {
		t.Fatalf("expected 0 stream messages after confirmation, got %d", len(*st.Messages))
	}
}

func TestConversationDraftAccumulationAndCompletion(t *testing.T) {
	ResetStream()
	ClearTranscript()
	defer ResetStream()
	defer ClearTranscript()

	acc := newConversationAccumulator()
	acc.setSessionID("sess-1")

	// Add chunk
	acc.applyEvent(api.Event{
		Type: api.EventChunk,
		Payload: api.ChunkEvent{
			SessionID: "sess-1",
			Chunk: api.Chunk{
				ID: "asst-1",
				Content: []api.Block{
					{Type: api.BlockReasoning, Text: "Thinking step 1..."},
				},
			},
		},
	})

	st := GetStream()
	if !st.Streaming {
		t.Fatal("expected Streaming to be true during draft chunk")
	}
	if st.Messages == nil || len(*st.Messages) != 1 {
		t.Fatalf("expected 1 stream message, got %+v", st.Messages)
	}
	asstMsg := (*st.Messages)[0]
	if asstMsg.Role != api.RoleAssistant || len(asstMsg.Parts) != 1 {
		t.Fatalf("unexpected stream message: %+v", asstMsg)
	}
	if asstMsg.Parts[0].Thought == nil || asstMsg.Parts[0].Thought.Text != "Thinking step 1..." {
		t.Fatalf("unexpected thought part: %+v", asstMsg.Parts[0])
	}

	// Append text chunk
	acc.applyEvent(api.Event{
		Type: api.EventChunk,
		Payload: api.ChunkEvent{
			SessionID: "sess-1",
			Chunk: api.Chunk{
				ID: "asst-1",
				Content: []api.Block{
					{Type: api.BlockText, Text: "Hello! How can I help?"},
				},
			},
		},
	})

	st = GetStream()
	asstMsg = (*st.Messages)[0]
	if len(asstMsg.Parts) != 2 {
		t.Fatalf("expected 2 parts (thought + text), got %d", len(asstMsg.Parts))
	}
	if asstMsg.Parts[1].Text == nil || asstMsg.Parts[1].Text.Text != "Hello! How can I help?" {
		t.Fatalf("unexpected text part: %+v", asstMsg.Parts[1])
	}

	// Complete turn
	acc.applyEvent(api.Event{
		Type: api.EventCompleted,
		Payload: api.CompletedEvent{
			SessionID: "sess-1",
		},
	})

	st = GetStream()
	if st.Streaming {
		t.Fatal("expected Streaming to be false after completion")
	}
}

func TestConversationCancelPendingUser(t *testing.T) {
	ResetStream()
	ClearTranscript()
	defer ResetStream()
	defer ClearTranscript()

	acc := newConversationAccumulator()
	acc.setSessionID("sess-1")

	acc.addPendingUser("msg-err", "Failing prompt")
	st := GetStream()
	if len(*st.Messages) != 1 {
		t.Fatalf("expected 1 stream message, got %d", len(*st.Messages))
	}

	// Cancel on error
	acc.cancelPendingUser("msg-err", errors.New("network error"))

	st = GetStream()
	if st.Messages != nil && len(*st.Messages) > 0 {
		t.Fatalf("expected stream messages to be cleared after cancel, got %d", len(*st.Messages))
	}
	if st.Streaming {
		t.Fatal("expected Streaming to be false after error cancel")
	}

	// Error banner should be appended to transcript
	tr := GetTranscript()
	if tr.Items == nil || len(*tr.Items) != 1 {
		t.Fatalf("expected 1 error transcript item, got %+v", tr.Items)
	}
	if (*tr.Items)[0].Kind != TranscriptKindError {
		t.Fatalf("expected TranscriptKindError, got %q", (*tr.Items)[0].Kind)
	}
}
