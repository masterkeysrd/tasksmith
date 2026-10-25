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
	FreezeTranscript()
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

	// Transcript should not yet have the message (only the snapshot header from FreezeTranscript)
	tr := GetTranscript()
	if tr.Items != nil {
		for _, item := range *tr.Items {
			if item.Kind == TranscriptKindMessage {
				t.Fatalf("expected no message in transcript before confirmation")
			}
		}
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

	// Message should now be flushed to Transcript (preceded by auto-prepended Header)
	tr = GetTranscript()
	if tr.Items == nil || len(*tr.Items) != 2 {
		t.Fatalf("expected 2 transcript items (header + confirmed msg), got %+v", tr.Items)
	}
	if (*tr.Items)[0].Kind != TranscriptKindHeader {
		t.Fatalf("expected item 0 kind %q, got %q", TranscriptKindHeader, (*tr.Items)[0].Kind)
	}
	item := (*tr.Items)[1]
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
	FreezeTranscript()
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

	// Error banner should be appended to transcript (preceded by auto-prepended Header)
	tr := GetTranscript()
	if tr.Items == nil || len(*tr.Items) != 2 {
		t.Fatalf("expected 2 transcript items (header + error), got %+v", tr.Items)
	}
	if (*tr.Items)[0].Kind != TranscriptKindHeader {
		t.Fatalf("expected item 0 kind %q, got %q", TranscriptKindHeader, (*tr.Items)[0].Kind)
	}
	if (*tr.Items)[1].Kind != TranscriptKindError {
		t.Fatalf("expected TranscriptKindError for item 1, got %q", (*tr.Items)[1].Kind)
	}
}

func TestConversationInterruptedTurnFlushesMessages(t *testing.T) {
	ResetStream()
	ClearTranscript()
	FreezeTranscript()
	defer ResetStream()
	defer ClearTranscript()

	acc := newConversationAccumulator()
	acc.setSessionID("sess-1")

	// 1. User message confirmed
	acc.applyEvent(api.Event{
		Type: api.EventEntryAdded,
		Payload: api.EntryAddedEvent{
			SessionID: "sess-1",
			Entry: api.TimelineEntry{
				ID:   "u1",
				Kind: api.TimelineKindMessage,
				Message: &api.Message{
					ID:        "u1",
					Role:      api.RoleUser,
					CreatedAt: time.Now(),
					Content: []api.Block{
						{Type: api.BlockText, Text: "Run a task"},
					},
				},
			},
		},
	})

	// 2. Draft chunks accumulated for assistant
	acc.applyEvent(api.Event{
		Type: api.EventChunk,
		Payload: api.ChunkEvent{
			SessionID: "sess-1",
			Chunk: api.Chunk{
				ID: "a1",
				Content: []api.Block{
					{Type: api.BlockReasoning, Text: "Thinking partial..."},
				},
			},
		},
	})
	if !GetStream().Streaming {
		t.Fatal("expected Streaming true during draft")
	}

	// 3. Daemon cancels and emits persisted partial assistant message followed by interrupted entry
	acc.applyEvent(api.Event{
		Type: api.EventEntryAdded,
		Payload: api.EntryAddedEvent{
			SessionID: "sess-1",
			Entry: api.TimelineEntry{
				ID:   "a1",
				Kind: api.TimelineKindMessage,
				Message: &api.Message{
					ID:        "a1",
					Role:      api.RoleAssistant,
					CreatedAt: time.Now(),
					Content: []api.Block{
						{Type: api.BlockReasoning, Text: "Thinking partial..."},
					},
				},
			},
		},
	})

	acc.applyEvent(api.Event{
		Type: api.EventEntryAdded,
		Payload: api.EntryAddedEvent{
			SessionID: "sess-1",
			Entry: api.TimelineEntry{
				ID:   "int-1",
				Kind: api.TimelineKindInterrupted,
			},
		},
	})

	// Streaming should be false
	if GetStream().Streaming {
		t.Fatal("expected Streaming false after TimelineKindInterrupted")
	}
	if GetStream().Messages != nil && len(*GetStream().Messages) > 0 {
		t.Fatalf("expected 0 active stream messages, got %d", len(*GetStream().Messages))
	}

	// Transcript should have header, user msg, assistant partial msg, and interrupted notice
	tr := GetTranscript()
	if tr.Items == nil || len(*tr.Items) != 4 {
		t.Fatalf("expected 4 transcript items (header, user, assistant, interrupted), got %d", len(*tr.Items))
	}
	if (*tr.Items)[0].Kind != TranscriptKindHeader {
		t.Errorf("item 0 kind = %q, want header", (*tr.Items)[0].Kind)
	}
	if (*tr.Items)[1].Kind != TranscriptKindMessage {
		t.Errorf("item 1 kind = %q, want message", (*tr.Items)[1].Kind)
	}
	if (*tr.Items)[2].Kind != TranscriptKindMessage {
		t.Errorf("item 2 kind = %q, want message", (*tr.Items)[2].Kind)
	}
	if (*tr.Items)[3].Kind != TranscriptKindTimeline {
		t.Errorf("item 3 kind = %q, want timeline", (*tr.Items)[3].Kind)
	}
}

func TestConversationInterruptedToolCall(t *testing.T) {
	ResetStream()
	ClearTranscript()
	FreezeTranscript()
	defer ResetStream()
	defer ClearTranscript()

	acc := newConversationAccumulator()
	acc.setSessionID("sess-1")

	// User message initiates running turn
	acc.applyEvent(api.Event{
		Type: api.EventEntryAdded,
		Payload: api.EntryAddedEvent{
			SessionID: "sess-1",
			Entry: api.TimelineEntry{
				ID:   "u-1",
				Kind: api.TimelineKindMessage,
				Message: &api.Message{
					ID:        "u-1",
					Role:      api.RoleUser,
					CreatedAt: time.Now(),
					Content: []api.Block{
						{Type: api.BlockText, Text: "run sleep"},
					},
				},
			},
		},
	})

	// Assistant message with a tool call
	acc.applyEvent(api.Event{
		Type: api.EventEntryAdded,
		Payload: api.EntryAddedEvent{
			SessionID: "sess-1",
			Entry: api.TimelineEntry{
				ID:   "a-tool",
				Kind: api.TimelineKindMessage,
				Message: &api.Message{
					ID:        "a-tool",
					Role:      api.RoleAssistant,
					CreatedAt: time.Now(),
					Content: []api.Block{
						{
							Type: api.BlockToolCall,
							ToolCall: &api.ToolCall{
								ID:   "call-1",
								Name: "bash",
								Args: map[string]any{"command": "sleep 10"},
							},
						},
					},
				},
			},
		},
	})

	// While running, tool is running and in stream messages
	st := GetStream()
	if st.Messages == nil || len(*st.Messages) != 1 {
		t.Fatalf("expected 1 stream message while tool running, got %+v", st.Messages)
	}
	toolPart := (*st.Messages)[0].Parts[0].Tool
	if toolPart == nil || toolPart.Status != ToolRunning {
		t.Fatalf("expected ToolRunning status, got %+v", toolPart)
	}

	// Now interrupted timeline event arrives
	acc.applyEvent(api.Event{
		Type: api.EventEntryAdded,
		Payload: api.EntryAddedEvent{
			SessionID: "sess-1",
			Entry: api.TimelineEntry{
				ID:   "int-2",
				Kind: api.TimelineKindInterrupted,
			},
		},
	})

	// Active stream should now be empty
	if GetStream().Messages != nil && len(*GetStream().Messages) > 0 {
		t.Fatalf("expected active stream to be empty, got %d", len(*GetStream().Messages))
	}

	// Tool message should be flushed to Transcript with ToolInterrupted status!
	tr := GetTranscript()
	if tr.Items == nil || len(*tr.Items) != 4 {
		t.Fatalf("expected 4 transcript items (header + user + flushed tool message + interrupted), got %d", len(*tr.Items))
	}
	if (*tr.Items)[0].Kind != TranscriptKindHeader {
		t.Fatalf("expected item 0 kind %q, got %q", TranscriptKindHeader, (*tr.Items)[0].Kind)
	}
	msgItem, ok := (*tr.Items)[2].Data.(Message)
	if !ok || len(msgItem.Parts) != 1 || msgItem.Parts[0].Tool == nil {
		t.Fatalf("unexpected transcript item 2: %+v", (*tr.Items)[2].Data)
	}
	if msgItem.Parts[0].Tool.Status != ToolInterrupted {
		t.Fatalf("expected ToolInterrupted, got %q", msgItem.Parts[0].Tool.Status)
	}
	if (*tr.Items)[3].Kind != TranscriptKindTimeline {
		t.Fatalf("expected TranscriptKindTimeline for item 3, got %q", (*tr.Items)[3].Kind)
	}
}
