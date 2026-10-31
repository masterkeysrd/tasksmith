package store

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
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

func TestConversationPermissionSlotEnqueueAndResolution(t *testing.T) {
	ResetStream()
	ClearTranscript()
	ClearSlot()
	defer ResetStream()
	defer ClearTranscript()
	defer ClearSlot()

	acc := newConversationAccumulator()
	acc.setSessionID("sess-1")

	permReq := api.PermissionRequest{
		ID:         "perm-1",
		ToolCallID: "tc-1",
		ToolName:   "bash",
		Action:     api.ActionExec,
		Target:     "go test ./...",
	}

	// 1. EventPermissionRequested enqueues SlotKindPermission
	acc.applyEvent(api.Event{
		Type: api.EventPermissionRequested,
		Payload: api.PermissionRequestedEvent{
			SessionID:   "sess-1",
			Permissions: []api.PermissionRequest{permReq},
		},
	})

	active := GetSlot().Active
	if active == nil || active.Kind != SlotKindPermission {
		t.Fatalf("expected active SlotKindPermission after EventPermissionRequested, got %+v", active)
	}
	if active.ID != "perm-perm-1" {
		t.Errorf("expected slot ID perm-perm-1, got %q", active.ID)
	}
	if reqData, ok := active.Data.(api.PermissionRequest); !ok || reqData.ID != "perm-1" {
		t.Errorf("expected PermissionRequest in slot Data, got %+v", active.Data)
	}

	// 2. EventEntryAdded with matching tool message removes the permission slot
	acc.applyEvent(api.Event{
		Type: api.EventEntryAdded,
		Payload: api.EntryAddedEvent{
			SessionID: "sess-1",
			Entry: api.TimelineEntry{
				ID:   "entry-tool-1",
				Kind: api.TimelineKindMessage,
				Message: &api.Message{
					ID:         "msg-tool-1",
					Role:       api.RoleTool,
					ToolCallID: "tc-1",
					CreatedAt:  time.Now(),
				},
			},
		},
	})

	if GetSlot().Active != nil {
		t.Errorf("expected permission slot to be removed after tool message, got %+v", GetSlot().Active)
	}

	// 3. SeedPermissions enqueues SlotKindPermission
	permReq2 := api.PermissionRequest{
		ID:         "perm-2",
		ToolCallID: "tc-2",
		ToolName:   "write_file",
		Action:     api.ActionWrite,
		Target:     "main.go",
	}
	acc.SeedPermissions([]api.PermissionRequest{permReq2})

	active2 := GetSlot().Active
	if active2 == nil || active2.Kind != SlotKindPermission {
		t.Fatalf("expected active SlotKindPermission after SeedPermissions, got %+v", active2)
	}
	if active2.ID != "perm-perm-2" {
		t.Errorf("expected slot ID perm-perm-2, got %q", active2.ID)
	}

	// 4. DecidePermission removes the slot and removes permission from accumulator
	DecidePermission(permReq2, api.PermissionDecision{
		Kind: api.DecisionAllowOnce,
	})

	if GetSlot().Active != nil {
		t.Errorf("expected permission slot to be removed after DecidePermission, got %+v", GetSlot().Active)
	}

	// 5. EventEntryAdded with TimelineKindPermission dequeues permission slot and does NOT append to transcript
	permReq3 := api.PermissionRequest{
		ID:         "perm-3",
		ToolCallID: "tc-3",
		ToolName:   "bash",
		Action:     api.ActionExec,
		Target:     "cat README.md",
	}
	acc.SeedPermissions([]api.PermissionRequest{permReq3})
	if GetSlot().Active == nil || GetSlot().Active.ID != "perm-perm-3" {
		t.Fatalf("expected active slot perm-perm-3, got %+v", GetSlot().Active)
	}

	initialTranscriptCount := 0
	if tr := GetTranscript(); tr.Items != nil {
		initialTranscriptCount = len(*tr.Items)
	}

	acc.applyEvent(api.Event{
		Type: api.EventEntryAdded,
		Payload: api.EntryAddedEvent{
			SessionID: "sess-1",
			Entry: api.TimelineEntry{
				ID:   "entry-perm-3",
				Kind: api.TimelineKindPermission,
				Permission: &api.PermissionRecord{
					Request: permReq3,
					Decision: api.PermissionDecision{
						RequestID:  "perm-3",
						ToolCallID: "tc-3",
						Kind:       api.DecisionAllowOnce,
					},
				},
			},
		},
	})

	// Permission slot should be removed
	if GetSlot().Active != nil {
		t.Errorf("expected permission slot to be removed after TimelineKindPermission, got %+v", GetSlot().Active)
	}

	// Transcript items count should NOT have increased
	afterTranscriptCount := 0
	if tr := GetTranscript(); tr.Items != nil {
		afterTranscriptCount = len(*tr.Items)
	}
	if afterTranscriptCount != initialTranscriptCount {
		t.Errorf("expected transcript items not to increase on TimelineKindPermission (was %d, now %d)", initialTranscriptCount, afterTranscriptCount)
	}
}

func TestConversationOptimisticQueueingAndEventRemovals(t *testing.T) {
	ResetStream()
	ClearTranscript()
	defer ResetStream()
	defer ClearTranscript()

	acc := newConversationAccumulator()
	acc.setSessionID("sess-queue-test")

	// 1. Initial turn starts
	acc.addPendingUser("user-1", "Primary prompt")
	st := GetStream()
	if !st.Streaming {
		t.Fatal("expected Streaming to be true")
	}
	if st.Queue != nil {
		t.Fatalf("expected nil Queue on initial turn, got %+v", st.Queue)
	}

	// 2. Queue user message while turn is running
	acc.addPendingUser("user-2", "Queued prompt message")
	st = GetStream()
	if st.Queue == nil || len(*st.Queue) != 1 {
		t.Fatalf("expected 1 item in Queue, got %+v", st.Queue)
	}
	q1 := (*st.Queue)[0]
	if q1.ID != "user-2" || q1.Kind != api.QueueKindMessage {
		t.Fatalf("expected QueueKindMessage item user-2, got %+v", q1)
	}

	// 3. Queue command while turn is running
	acc.addPendingCommand("cmd-1", "git status")
	st = GetStream()
	if st.Queue == nil || len(*st.Queue) != 2 {
		t.Fatalf("expected 2 items in Queue, got %+v", st.Queue)
	}
	q2 := (*st.Queue)[1]
	if q2.ID != "cmd-1" || q2.Kind != api.QueueKindCommand {
		t.Fatalf("expected QueueKindCommand item cmd-1, got %+v", q2)
	}

	// 4. Dedup check: re-adding existing ID does not duplicate
	acc.addPendingUser("user-2", "Queued prompt message")
	st = GetStream()
	if len(*st.Queue) != 2 {
		t.Fatalf("expected 2 items in Queue after dedup attempt, got %d", len(*st.Queue))
	}

	// 5. Cancel user-2 by ID
	acc.cancelPendingUser("user-2", errors.New("network failure"))
	st = GetStream()
	if len(*st.Queue) != 1 || (*st.Queue)[0].ID != "cmd-1" {
		t.Fatalf("expected only cmd-1 in Queue after cancel, got %+v", st.Queue)
	}

	// 6. EventCommandStarted for cmd-1 removes it from queue and sets pendingCommand
	acc.applyEvent(api.Event{
		Type: api.EventCommandStarted,
		Payload: api.CommandStartedEvent{
			SessionID: "sess-queue-test",
			ID:        "cmd-1",
			Command:   "git status",
		},
	})
	st = GetStream()
	if st.Queue != nil {
		t.Fatalf("expected empty Queue after cmd-1 started, got %+v", st.Queue)
	}
	if st.PendingCommand != "git status" {
		t.Fatalf("expected PendingCommand 'git status', got %q", st.PendingCommand)
	}

	// 7. Enqueue user-3 and remove via EventEntryAdded
	acc.addPendingUser("user-3", "Third message")
	st = GetStream()
	if st.Queue == nil || len(*st.Queue) != 1 || (*st.Queue)[0].ID != "user-3" {
		t.Fatalf("expected user-3 in Queue, got %+v", st.Queue)
	}

	acc.applyEvent(api.Event{
		Type: api.EventEntryAdded,
		Payload: api.EntryAddedEvent{
			SessionID: "sess-queue-test",
			Entry: api.TimelineEntry{
				ID:   "entry-u3",
				Kind: api.TimelineKindMessage,
				Message: &api.Message{
					ID:        "user-3",
					Role:      api.RoleUser,
					CreatedAt: time.Now().UTC(),
					Content: []api.Block{
						{
							Type: api.BlockText,
							Text: "Third message",
						},
					},
				},
			},
		},
	})
	st = GetStream()
	if st.Queue != nil {
		t.Fatalf("expected empty Queue after EventEntryAdded for user-3, got %+v", st.Queue)
	}

	// 8. SeedQueue and CancelTurn
	acc.SeedQueue([]api.QueueItem{
		{
			ID:      "seed-1",
			Kind:    api.QueueKindCommand,
			Command: "ls -l",
		},
	})
	st = GetStream()
	if st.Queue == nil || len(*st.Queue) != 1 || (*st.Queue)[0].ID != "seed-1" {
		t.Fatalf("expected seed-1 in Queue, got %+v", st.Queue)
	}

	acc.cancelTurn()
	st = GetStream()
	if st.Queue != nil {
		t.Fatalf("expected nil Queue after cancelTurn, got %+v", st.Queue)
	}
}

type trackingRoundTripper struct {
	updateSessionCount atomic.Int32
}

func (t *trackingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodPatch {
		t.updateSessionCount.Add(1)
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"sess-1","config":{"mode":"acceptEdits"}}`)),
			Header:     make(http.Header),
		}
		resp.Header.Set("Content-Type", "application/json")
		return resp, nil
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"id":"sess-1","config":{"mode":"default"}}`)),
		Header:     make(http.Header),
	}
	resp.Header.Set("Content-Type", "application/json")
	return resp, nil
}

func TestTimelineKindModeDoesNotTriggerUpdateSession(t *testing.T) {
	ResetSession()
	ResetStream()
	ClearTranscript()
	defer ResetSession()
	defer ResetStream()
	defer ClearTranscript()

	origDelay := modeDebounceDelay
	modeDebounceDelay = 30 * time.Millisecond
	defer func() { modeDebounceDelay = origDelay }()

	rt := &trackingRoundTripper{}
	c, err := client.New("/tmp/dummy.sock", client.WithCWD("/workspace"), client.WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	sessMu.Lock()
	activeClient = c
	activeCtx = context.Background()
	sessMu.Unlock()

	SetSession(&api.Session{
		ID:     "sess-1",
		Config: api.SessionConfig{Mode: api.ModeDefault},
	})

	if GetSession().Config.Mode != api.ModeDefault {
		t.Fatalf("expected ModeDefault, got %s", GetSession().Config.Mode)
	}

	acc := newConversationAccumulator()
	acc.setSessionID("sess-1")

	// 1. Receiving api.TimelineKindMode via applyEvent (SSE event)
	acc.applyEvent(api.Event{
		Type: api.EventEntryAdded,
		Payload: api.EntryAddedEvent{
			SessionID: "sess-1",
			Entry: api.TimelineEntry{
				ID:   "entry-mode-1",
				Kind: api.TimelineKindMode,
				Mode: &api.ModeChange{From: api.ModeDefault, To: api.ModePlan},
			},
		},
	})

	if got := GetSession().Config.Mode; got != api.ModePlan {
		t.Fatalf("expected mode updated to ModePlan by applyEvent, got %q", got)
	}

	// Give any async goroutines time to run
	time.Sleep(50 * time.Millisecond)
	if count := rt.updateSessionCount.Load(); count != 0 {
		t.Fatalf("expected 0 UpdateSession calls from applyEvent TimelineKindMode, got %d", count)
	}

	// 2. Receiving api.TimelineKindMode via loadTimeline
	acc.loadTimeline([]api.TimelineEntry{
		{
			ID:   "entry-mode-2",
			Kind: api.TimelineKindMode,
			Mode: &api.ModeChange{From: api.ModePlan, To: api.ModeAcceptEdits},
		},
	})

	if got := GetSession().Config.Mode; got != api.ModeAcceptEdits {
		t.Fatalf("expected mode updated to ModeAcceptEdits by loadTimeline, got %q", got)
	}

	time.Sleep(50 * time.Millisecond)
	if count := rt.updateSessionCount.Load(); count != 0 {
		t.Fatalf("expected 0 UpdateSession calls from loadTimeline TimelineKindMode, got %d", count)
	}

	// 3. In contrast, calling SetMode directly SHOULD call UpdateSession
	SetMode(api.ModeAuto)
	if got := GetSession().Config.Mode; got != api.ModeAuto {
		t.Fatalf("expected mode updated to ModeAuto by SetMode, got %q", got)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if rt.updateSessionCount.Load() > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if count := rt.updateSessionCount.Load(); count != 1 {
		t.Fatalf("expected 1 UpdateSession call from SetMode, got %d", count)
	}
}

func TestLoadTimelineChronologicalOrderAndToolPairing(t *testing.T) {
	ResetSession()
	ResetStream()
	ClearTranscript()
	FreezeTranscript()
	defer ResetSession()
	defer ResetStream()
	defer ClearTranscript()

	acc := newConversationAccumulator()
	acc.setSessionID("sess-timeline-order")

	now := time.Now()
	entries := []api.TimelineEntry{
		// 1. User message
		{
			ID:   "e-user",
			Kind: api.TimelineKindMessage,
			Message: &api.Message{
				ID:        "msg-user",
				Role:      api.RoleUser,
				CreatedAt: now,
				Content: []api.Block{
					{Type: api.BlockText, Text: "Please view file"},
				},
			},
		},
		// 2. Assistant tool call
		{
			ID:   "e-assistant-call",
			Kind: api.TimelineKindMessage,
			Message: &api.Message{
				ID:        "msg-assistant-call",
				Role:      api.RoleAssistant,
				CreatedAt: now.Add(time.Second),
				Content: []api.Block{
					{
						Type: api.BlockToolCall,
						ToolCall: &api.ToolCall{
							ID:   "tc-1",
							Name: "view",
							Args: map[string]any{"path": "main.go"},
						},
					},
				},
			},
		},
		// 3. Mode change interleaved between tool call and tool response!
		{
			ID:   "e-mode",
			Kind: api.TimelineKindMode,
			Mode: &api.ModeChange{From: api.ModeDefault, To: api.ModeAcceptEdits},
		},
		// 4. Tool response message
		{
			ID:   "e-tool-resp",
			Kind: api.TimelineKindMessage,
			Message: &api.Message{
				ID:         "msg-tool-resp",
				Role:       api.RoleTool,
				ToolCallID: "tc-1",
				CreatedAt:  now.Add(2 * time.Second),
				Content: []api.Block{
					{Type: api.BlockText, Text: "package main"},
				},
			},
		},
		// 5. Non-message command
		{
			ID:   "e-cmd",
			Kind: api.TimelineKindCommand,
			Command: &api.CommandRecord{
				Command:  "ls -la",
				ExitCode: 0,
			},
		},
		// 6. Assistant final text message
		{
			ID:   "e-assistant-final",
			Kind: api.TimelineKindMessage,
			Message: &api.Message{
				ID:        "msg-assistant-final",
				Role:      api.RoleAssistant,
				CreatedAt: now.Add(3 * time.Second),
				Content: []api.Block{
					{Type: api.BlockText, Text: "Done viewing file."},
				},
			},
		},
	}

	acc.loadTimeline(entries)

	tr := GetTranscript()
	if tr.Items == nil {
		t.Fatal("expected non-nil transcript items")
	}

	// Filter out the auto-prepended Header item
	var items []TranscriptItem
	for _, it := range *tr.Items {
		if it.Kind != TranscriptKindHeader {
			items = append(items, it)
		}
	}

	for i, it := range items {
		t.Logf("item %d: ID=%s, Kind=%s", i, it.ID, it.Kind)
	}

	if len(items) != 5 {
		t.Fatalf("expected 5 transcript items (user, assistant-with-tool, mode, cmd, assistant-final), got %d: %+v", len(items), items)
	}

	// 1. User message
	if items[0].Kind != TranscriptKindMessage {
		t.Fatalf("expected item 0 to be Message, got %v", items[0].Kind)
	}
	userMsg := items[0].Data.(Message)
	if userMsg.ID != "msg-user" {
		t.Fatalf("expected item 0 ID msg-user, got %q", userMsg.ID)
	}

	// 2. Assistant message with paired tool call and response
	if items[1].Kind != TranscriptKindMessage {
		t.Fatalf("expected item 1 to be Message, got %v", items[1].Kind)
	}
	asstMsg := items[1].Data.(Message)
	if asstMsg.ID != "msg-assistant-call" {
		t.Fatalf("expected item 1 ID msg-assistant-call, got %q", asstMsg.ID)
	}
	if len(asstMsg.Parts) != 1 || asstMsg.Parts[0].Tool == nil {
		t.Fatalf("expected 1 tool part, got %+v", asstMsg.Parts)
	}
	toolPart := asstMsg.Parts[0].Tool
	if toolPart.Call == nil || toolPart.Call.Name != "view" {
		t.Fatalf("expected tool call view, got %+v", toolPart.Call)
	}
	if toolPart.Response == nil {
		t.Fatal("expected tool response to be paired with tool call even with interleaved mode change")
	}
	if toolPart.Status != ToolSucceeded {
		t.Fatalf("expected ToolSucceeded, got %v", toolPart.Status)
	}

	// 3. Mode change
	if items[2].Kind != TranscriptKindTimeline {
		t.Fatalf("expected item 2 to be TranscriptKindTimeline (mode), got %v", items[2].Kind)
	}
	modeEntry := items[2].Data.(api.TimelineEntry)
	if modeEntry.ID != "e-mode" || modeEntry.Kind != api.TimelineKindMode {
		t.Fatalf("expected item 2 ID e-mode, got %+v", modeEntry)
	}

	// 4. Command
	if items[3].Kind != TranscriptKindTimeline {
		t.Fatalf("expected item 3 to be TranscriptKindTimeline (command), got %v", items[3].Kind)
	}
	cmdEntry := items[3].Data.(api.TimelineEntry)
	if cmdEntry.ID != "e-cmd" || cmdEntry.Command == nil || cmdEntry.Command.Command != "ls -la" {
		t.Fatalf("expected command ls -la, got %+v", cmdEntry.Command)
	}

	// 5. Final assistant message
	if items[4].Kind != TranscriptKindMessage {
		t.Fatalf("expected item 4 to be Message, got %v", items[4].Kind)
	}
	finalMsg := items[4].Data.(Message)
	if finalMsg.ID != "msg-assistant-final" {
		t.Fatalf("expected item 4 ID msg-assistant-final, got %q", finalMsg.ID)
	}
}

func TestConversationTodosStateAndEvents(t *testing.T) {
	ResetStream()
	defer ResetStream()

	// 1. SeedTodos
	initialTodos := []api.TodoItem{
		{
			Content:    "Step 1",
			ActiveText: "Doing step 1",
			Status:     "in_progress",
		},
		{
			Content:    "Step 2",
			ActiveText: "Queued step 2",
			Status:     "pending",
		},
	}
	SeedTodos(initialTodos)

	st := GetStream()
	if st.Todos == nil {
		t.Fatal("expected non-nil stream.Todos after SeedTodos")
	}
	if len(*st.Todos) != 2 || (*st.Todos)[0].Content != "Step 1" || (*st.Todos)[0].Status != "in_progress" {
		t.Fatalf("unexpected stream.Todos: %+v", *st.Todos)
	}

	// 2. EventTodosUpdated
	updatedTodos := []api.TodoItem{
		{
			Content:    "Step 1",
			ActiveText: "Doing step 1",
			Status:     "completed",
		},
		{
			Content:    "Step 2",
			ActiveText: "Doing step 2",
			Status:     "in_progress",
		},
	}
	ApplyEvent(api.Event{
		Type: api.EventTodosUpdated,
		Payload: api.TodosUpdatedEvent{
			SessionID: "test-sess",
			Todos:     updatedTodos,
		},
	})

	st = GetStream()
	if st.Todos == nil {
		t.Fatal("expected non-nil stream.Todos after EventTodosUpdated")
	}
	if len(*st.Todos) != 2 || (*st.Todos)[0].Status != "completed" || (*st.Todos)[1].Status != "in_progress" {
		t.Fatalf("unexpected stream.Todos after update: %+v", *st.Todos)
	}

	// 3. Tool response message with todos
	outBytes, _ := json.Marshal(api.TodosOutput{
		Todos: []api.TodoItem{
			{
				Content:    "Step 1",
				ActiveText: "Doing step 1",
				Status:     "completed",
			},
			{
				Content:    "Step 2",
				ActiveText: "Doing step 2",
				Status:     "completed",
			},
		},
		Total:      2,
		Completed:  2,
		InProgress: 0,
		Pending:    0,
		Message:    "All done",
	})
	ApplyEvent(api.Event{
		Type: api.EventEntryAdded,
		Payload: api.EntryAddedEvent{
			SessionID: "test-sess",
			Entry: api.TimelineEntry{
				ID:   "e-tool-todos",
				Kind: api.TimelineKindMessage,
				Message: &api.Message{
					ID:                "msg-tool-todos",
					Role:              api.RoleTool,
					ToolCallID:        "call-todos-1",
					Name:              "todos",
					StructuredContent: outBytes,
				},
			},
		},
	})

	st = GetStream()
	if st.Todos == nil {
		t.Fatal("expected non-nil stream.Todos after tool response")
	}
	if len(*st.Todos) != 2 || (*st.Todos)[0].Status != "completed" || (*st.Todos)[1].Status != "completed" {
		t.Fatalf("unexpected stream.Todos after tool response: %+v", *st.Todos)
	}

	// 4. ResetStream clears todos
	ResetStream()
	st = GetStream()
	if st.Todos != nil {
		t.Fatalf("expected nil stream.Todos after ResetStream, got %+v", *st.Todos)
	}
}

func TestCompactionEventsAndTimeline(t *testing.T) {
	ResetStream()
	ClearTranscript()
	ResetSession()
	defer ResetStream()
	defer ClearTranscript()
	defer ResetSession()

	acc := newConversationAccumulator()
	acc.setSessionID("sess-comp")

	// 1. EventCompactionStarted sets stream status to StreamRunning and sessionStatus to StatusCompacting
	acc.applyEvent(api.Event{
		Type: api.EventCompactionStarted,
		Payload: api.CompactionStartedEvent{
			SessionID: "sess-comp",
			ID:        "comp-1",
		},
	})
	st := GetStream()
	if !st.Streaming || st.Status != api.StatusCompacting {
		t.Fatalf("expected Streaming=true and Status=StatusCompacting, got Streaming=%v, Status=%q", st.Streaming, st.Status)
	}

	// 2. EventContextUpdated updates session context
	ctxState := api.ContextState{
		UsedTokens:    45000,
		MaxTokens:     200000,
		OutputLimit:   32000,
		ReserveTokens: 40000,
		TriggerTokens: 160000,
		FreeTokens:    115000,
		Breakdown: api.ContextBreakdown{
			Summary: 15000,
		},
	}
	acc.applyEvent(api.Event{
		Type: api.EventContextUpdated,
		Payload: api.ContextUpdatedEvent{
			SessionID: "sess-comp",
			Context:   ctxState,
		},
	})
	if GetContext().UsedTokens != 45000 || GetContext().Breakdown.Summary != 15000 {
		t.Fatalf("expected session context updated, got %+v", GetContext())
	}

	// 3. EventEntryAdded with TimelineKindCompacted flushes to transcript
	acc.applyEvent(api.Event{
		Type: api.EventEntryAdded,
		Payload: api.EntryAddedEvent{
			SessionID: "sess-comp",
			Entry: api.TimelineEntry{
				ID:        "tl-comp-1",
				SessionID: "sess-comp",
				Kind:      api.TimelineKindCompacted,
				Compacted: &api.CompactedRecord{
					BeforeTokens: 140000,
					AfterTokens:  15000,
				},
			},
		},
	})
	tr := GetTranscript()
	if tr.Items == nil || len(*tr.Items) == 0 {
		t.Fatalf("expected timeline entry flushed to transcript")
	}
	lastItem := (*tr.Items)[len(*tr.Items)-1]
	if lastItem.Kind != TranscriptKindTimeline {
		t.Fatalf("expected TranscriptKindTimeline, got %v", lastItem.Kind)
	}
	entry, ok := lastItem.Data.(api.TimelineEntry)
	if !ok || entry.Kind != api.TimelineKindCompacted || entry.Compacted.BeforeTokens != 140000 {
		t.Fatalf("unexpected timeline entry data: %+v", lastItem.Data)
	}

	// 4. EventEntryAdded with TimelineKindCacheBroken flushes to transcript
	acc.applyEvent(api.Event{
		Type: api.EventEntryAdded,
		Payload: api.EntryAddedEvent{
			SessionID: "sess-comp",
			Entry: api.TimelineEntry{
				ID:        "tl-cb-1",
				SessionID: "sess-comp",
				Kind:      api.TimelineKindCacheBroken,
				CacheBroken: &api.CacheBrokenRecord{
					PrevCached: 50000,
					CurrCached: 0,
				},
			},
		},
	})
	tr = GetTranscript()
	lastItem = (*tr.Items)[len(*tr.Items)-1]
	entry, ok = lastItem.Data.(api.TimelineEntry)
	if !ok || entry.Kind != api.TimelineKindCacheBroken || entry.CacheBroken.PrevCached != 50000 {
		t.Fatalf("unexpected cache_broken entry data: %+v", lastItem.Data)
	}
}
