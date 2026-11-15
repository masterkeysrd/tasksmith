package store

import (
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestSubagentsStoreSelectors(t *testing.T) {
	ResetSession()

	if subs := GetSubagents(); subs != nil {
		t.Fatalf("expected nil subagents initially, got %v", subs)
	}

	sub1 := api.Session{
		ID:        "sub-1",
		Status:    api.StatusIdle,
		CreatedAt: time.Now(),
		Config:    api.SessionConfig{Agent: "explore"},
	}
	sub2 := api.Session{
		ID:        "sub-2",
		Status:    api.StatusExecuting,
		CreatedAt: time.Now(),
		Config:    api.SessionConfig{Agent: "general"},
	}
	sub3 := api.Session{
		ID:        "sub-3",
		Status:    api.StatusWaitingPermission,
		CreatedAt: time.Now(),
		Config:    api.SessionConfig{Agent: "worker"},
		PendingPermissions: []api.PermissionRequest{
			{ID: "perm-1", ToolName: "bash", Target: "rm -rf /tmp/foo"},
		},
	}

	UpsertSubagent(sub1)
	UpsertSubagent(sub2)
	UpsertSubagent(sub3)

	all := GetSubagents()
	if len(all) != 3 {
		t.Fatalf("expected 3 subagents, got %d", len(all))
	}

	running := GetRunningSubagents()
	if len(running) != 2 {
		t.Fatalf("expected 2 running subagents (executing and waiting), got %d", len(running))
	}

	waiting := GetWaitingSubagents()
	if len(waiting) != 1 || waiting[0].ID != "sub-3" {
		t.Fatalf("expected sub-3 waiting, got %v", waiting)
	}

	// Update sub1 to running
	sub1.Status = api.StatusThinking
	UpsertSubagent(sub1)
	if len(GetRunningSubagents()) != 3 {
		t.Fatalf("expected 3 running subagents after update, got %d", len(GetRunningSubagents()))
	}
}

func TestSubagentConversationIsolated(t *testing.T) {
	conv := NewSubagentConversation("child-session")

	// Initially empty
	if len(conv.Messages()) != 0 {
		t.Fatalf("expected 0 messages initially, got %d", len(conv.Messages()))
	}

	// Load timeline
	now := time.Now().UTC()
	entries := []api.TimelineEntry{
		{
			ID:   "e1",
			Kind: api.TimelineKindMessage,
			Message: &api.Message{
				ID:        "m1",
				Role:      api.RoleUser,
				CreatedAt: now,
				Content: []api.Block{
					{Type: api.BlockText, Text: "Please audit the codebase"},
				},
			},
		},
		{
			ID:   "e2",
			Kind: api.TimelineKindMessage,
			Message: &api.Message{
				ID:        "m2",
				Role:      api.RoleAssistant,
				CreatedAt: now.Add(time.Second),
				Content: []api.Block{
					{Type: api.BlockText, Text: "I will start by listing files"},
					{
						Type: api.BlockToolCall,
						ToolCall: &api.ToolCall{
							ID:   "c1",
							Name: "ls",
							Args: map[string]any{"path": "."},
						},
					},
				},
			},
		},
		{
			ID:   "e3",
			Kind: api.TimelineKindMessage,
			Message: &api.Message{
				ID:         "m3",
				Role:       api.RoleTool,
				ToolCallID: "c1",
				Name:       "ls",
				CreatedAt:  now.Add(2 * time.Second),
				Content: []api.Block{
					{Type: api.BlockText, Text: "main.go\nREADME.md"},
				},
			},
		},
	}

	conv.LoadTimeline(entries)

	msgs := conv.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (user + assistant with tool pair), got %d", len(msgs))
	}
	if msgs[0].Role != api.RoleUser {
		t.Errorf("expected role user, got %s", msgs[0].Role)
	}
	if msgs[1].Role != api.RoleAssistant {
		t.Errorf("expected role assistant, got %s", msgs[1].Role)
	}
	if len(msgs[1].Parts) != 2 {
		t.Fatalf("expected 2 parts in assistant message, got %d", len(msgs[1].Parts))
	}
	toolPart := msgs[1].Parts[1].Tool
	if toolPart == nil || toolPart.Response == nil {
		t.Fatal("expected tool part with paired response")
	}

	// Apply live chunk event
	conv.ApplyEvent(api.Event{
		Type: api.EventChunk,
		Payload: api.ChunkEvent{
			SessionID: "child-session",
			Chunk: api.Chunk{
				ID: "m4",
				Content: []api.Block{
					{Type: api.BlockText, Text: "Continuing analysis..."},
				},
			},
		},
	})

	if !conv.IsStreaming() {
		t.Error("expected conversation to report streaming after chunk")
	}
	msgsAfterChunk := conv.Messages()
	if len(msgsAfterChunk) != 3 {
		t.Fatalf("expected 3 messages after live chunk, got %d", len(msgsAfterChunk))
	}

	// Verify global conversation was not touched
	globalMsgs := GetStream().Messages
	if globalMsgs != nil && len(*globalMsgs) > 0 {
		t.Error("globalConversation should not have received subagent chunks")
	}
}
