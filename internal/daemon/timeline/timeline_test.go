package timeline

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

func openTestStore(t *testing.T) (Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "timeline_test.db")
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store, err := New(ctx, db)
	if err != nil {
		t.Fatalf("timeline.New: %v", err)
	}
	return store, ctx
}

func TestNew(t *testing.T) {
	ctx := context.Background()

	// 1. Missing DB
	_, err := New(ctx, nil)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New(ctx, nil) error = %v, want %v", err, ErrInvalidConfig)
	}

	// 2. Valid DB
	dbPath := filepath.Join(t.TempDir(), "new_test.db")
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer db.Close()

	store, err := New(ctx, db)
	if err != nil {
		t.Fatalf("New(ctx, db) unexpected error: %v", err)
	}
	if store == nil {
		t.Fatal("New(ctx, db) returned nil store")
	}

	// 3. Idempotent New
	store2, err := New(ctx, db)
	if err != nil {
		t.Fatalf("second New(ctx, db) error: %v", err)
	}
	if store2 == nil {
		t.Fatal("second New(ctx, db) returned nil store")
	}
}

func TestAppendAndGet_AllKinds(t *testing.T) {
	store, ctx := openTestStore(t)
	sessionID := "session-all-kinds"

	tests := []struct {
		name      string
		makeEntry func() *Entry
		verify    func(t *testing.T, original, loaded *Entry)
	}{
		{
			name: "message user",
			makeEntry: func() *Entry {
				return NewMessageEntry(sessionID, message.NewUserText("Hello TaskSmith"))
			},
			verify: func(t *testing.T, original, loaded *Entry) {
				if loaded.Kind != KindMessage {
					t.Fatalf("loaded.Kind = %q, want %q", loaded.Kind, KindMessage)
				}
				if loaded.Message == nil {
					t.Fatal("loaded.Message is nil")
				}
				if loaded.Message.Role() != message.RoleUser {
					t.Errorf("loaded.Message.Role() = %q, want %q", loaded.Message.Role(), message.RoleUser)
				}
				if loaded.Message.GetID() != loaded.ID {
					t.Errorf("loaded.Message.GetID() = %q, want loaded.ID %q", loaded.Message.GetID(), loaded.ID)
				}
			},
		},
		{
			name: "message assistant",
			makeEntry: func() *Entry {
				return NewMessageEntry(sessionID, message.NewAssistantText("Hello user!"))
			},
			verify: func(t *testing.T, original, loaded *Entry) {
				if loaded.Kind != KindMessage {
					t.Fatalf("loaded.Kind = %q, want %q", loaded.Kind, KindMessage)
				}
				if loaded.Message == nil {
					t.Fatal("loaded.Message is nil")
				}
				if loaded.Message.Role() != message.RoleAssistant {
					t.Errorf("loaded.Message.Role() = %q, want %q", loaded.Message.Role(), message.RoleAssistant)
				}
			},
		},
		{
			name: "model change",
			makeEntry: func() *Entry {
				return NewModelEntry(sessionID, ModelChange{
					From:   "claude-3-haiku",
					To:     "claude-3-5-sonnet",
					Effort: "high",
				})
			},
			verify: func(t *testing.T, original, loaded *Entry) {
				if loaded.Kind != KindModel {
					t.Fatalf("loaded.Kind = %q, want %q", loaded.Kind, KindModel)
				}
				if loaded.Model == nil {
					t.Fatal("loaded.Model is nil")
				}
				if *loaded.Model != *original.Model {
					t.Errorf("loaded.Model = %+v, want %+v", *loaded.Model, *original.Model)
				}
			},
		},
		{
			name: "effort change",
			makeEntry: func() *Entry {
				return NewEffortEntry(sessionID, EffortChange{
					From: "low",
					To:   "high",
				})
			},
			verify: func(t *testing.T, original, loaded *Entry) {
				if loaded.Kind != KindEffort {
					t.Fatalf("loaded.Kind = %q, want %q", loaded.Kind, KindEffort)
				}
				if loaded.Effort == nil {
					t.Fatal("loaded.Effort is nil")
				}
				if *loaded.Effort != *original.Effort {
					t.Errorf("loaded.Effort = %+v, want %+v", *loaded.Effort, *original.Effort)
				}
			},
		},
		{
			name: "agent change",
			makeEntry: func() *Entry {
				return NewAgentEntry(sessionID, AgentChange{
					From: "coder",
					To:   "planner",
				})
			},
			verify: func(t *testing.T, original, loaded *Entry) {
				if loaded.Kind != KindAgent {
					t.Fatalf("loaded.Kind = %q, want %q", loaded.Kind, KindAgent)
				}
				if loaded.Agent == nil {
					t.Fatal("loaded.Agent is nil")
				}
				if *loaded.Agent != *original.Agent {
					t.Errorf("loaded.Agent = %+v, want %+v", *loaded.Agent, *original.Agent)
				}
			},
		},
		{
			name: "mode change",
			makeEntry: func() *Entry {
				return NewModeEntry(sessionID, ModeChange{
					From: tools.ModeDefault,
					To:   tools.ModeAuto,
				})
			},
			verify: func(t *testing.T, original, loaded *Entry) {
				if loaded.Kind != KindMode {
					t.Fatalf("loaded.Kind = %q, want %q", loaded.Kind, KindMode)
				}
				if loaded.Mode == nil {
					t.Fatal("loaded.Mode is nil")
				}
				if *loaded.Mode != *original.Mode {
					t.Errorf("loaded.Mode = %+v, want %+v", *loaded.Mode, *original.Mode)
				}
			},
		},
		{
			name: "permission record",
			makeEntry: func() *Entry {
				return NewPermissionEntry(sessionID, PermissionRecord{
					Request: tools.PermissionRequest{
						ID:            "perm-req-1",
						ToolCallID:    "call-1",
						ToolName:      "bash",
						Action:        tools.ActionExec,
						Safety:        tools.SafetyUnsafe,
						Summary:       "run build script",
						GrantPatterns: []string{"bash*"},
					},
					Decision: tools.PermissionDecision{
						RequestID:    "perm-req-1",
						ToolCallID:   "call-1",
						Kind:         tools.DecisionAllowSession,
						GrantPattern: "bash*",
					},
				})
			},
			verify: func(t *testing.T, original, loaded *Entry) {
				if loaded.Kind != KindPermission {
					t.Fatalf("loaded.Kind = %q, want %q", loaded.Kind, KindPermission)
				}
				if loaded.Permission == nil {
					t.Fatal("loaded.Permission is nil")
				}
				if loaded.Permission.Request.ID != original.Permission.Request.ID {
					t.Errorf("loaded.Permission.Request.ID = %q, want %q", loaded.Permission.Request.ID, original.Permission.Request.ID)
				}
				if loaded.Permission.Decision.Kind != original.Permission.Decision.Kind {
					t.Errorf("loaded.Permission.Decision.Kind = %q, want %q", loaded.Permission.Decision.Kind, original.Permission.Decision.Kind)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := tt.makeEntry()
			if err := store.Append(ctx, entry); err != nil {
				t.Fatalf("Append failed: %v", err)
			}
			if entry.ID == "" {
				t.Fatal("Append did not assign an ID")
			}
			if entry.CreatedAt.IsZero() {
				t.Fatal("Append did not set CreatedAt")
			}

			loaded, err := store.Get(ctx, entry.ID)
			if err != nil {
				t.Fatalf("Get(%q) failed: %v", entry.ID, err)
			}
			if loaded.ID != entry.ID {
				t.Errorf("loaded.ID = %q, want %q", loaded.ID, entry.ID)
			}
			if loaded.SessionID != entry.SessionID {
				t.Errorf("loaded.SessionID = %q, want %q", loaded.SessionID, entry.SessionID)
			}
			if !loaded.CreatedAt.Equal(entry.CreatedAt) {
				t.Errorf("loaded.CreatedAt = %v, want %v", loaded.CreatedAt, entry.CreatedAt)
			}

			tt.verify(t, entry, loaded)
		})
	}
}

func TestAppendMessage_Convenience(t *testing.T) {
	store, ctx := openTestStore(t)
	sessionID := "session-append-msg"

	// 1. Message without an existing ID
	userMsg := message.NewUserText("User question")
	entry1, err := store.AppendMessage(ctx, sessionID, userMsg)
	if err != nil {
		t.Fatalf("AppendMessage failed: %v", err)
	}
	if entry1.ID == "" {
		t.Fatal("entry1.ID is empty")
	}
	if userMsg.GetID() != entry1.ID {
		t.Fatalf("userMsg.GetID() = %q, want entry1.ID %q", userMsg.GetID(), entry1.ID)
	}

	// 2. Message with existing ID
	existingID := uuid.Must(uuid.NewV7()).String()
	asstMsg := message.NewAssistantText("Assistant answer")
	asstMsg.SetID(existingID)

	entry2, err := store.AppendMessage(ctx, sessionID, asstMsg)
	if err != nil {
		t.Fatalf("AppendMessage with existing ID failed: %v", err)
	}
	if entry2.ID != existingID {
		t.Fatalf("entry2.ID = %q, want %q", entry2.ID, existingID)
	}

	// 3. Messages retrieval
	msgs, err := store.Messages(ctx, sessionID)
	if err != nil {
		t.Fatalf("Messages failed: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
	if msgs[0].GetID() != entry1.ID || msgs[1].GetID() != entry2.ID {
		t.Errorf("retrieved message IDs = [%q, %q], want [%q, %q]", msgs[0].GetID(), msgs[1].GetID(), entry1.ID, entry2.ID)
	}
}

func TestJSONRoundTrip_AllKinds(t *testing.T) {
	sessionID := "session-json-rt"
	now := time.Now().UTC().Truncate(time.Microsecond)

	userMsg := message.NewUserText("JSON test user")
	userMsg.SetID(uuid.Must(uuid.NewV7()).String())

	entries := []*Entry{
		{
			ID:        userMsg.GetID(),
			SessionID: sessionID,
			Kind:      KindMessage,
			CreatedAt: now,
			Message:   userMsg,
		},
		{
			ID:        uuid.Must(uuid.NewV7()).String(),
			SessionID: sessionID,
			Kind:      KindModel,
			CreatedAt: now,
			Model: &ModelChange{
				From:   "claude-3-opus",
				To:     "claude-3-5-sonnet",
				Effort: "medium",
			},
		},
		{
			ID:        uuid.Must(uuid.NewV7()).String(),
			SessionID: sessionID,
			Kind:      KindEffort,
			CreatedAt: now,
			Effort: &EffortChange{
				From: "low",
				To:   "high",
			},
		},
		{
			ID:        uuid.Must(uuid.NewV7()).String(),
			SessionID: sessionID,
			Kind:      KindAgent,
			CreatedAt: now,
			Agent: &AgentChange{
				From: "build",
				To:   "review",
			},
		},
		{
			ID:        uuid.Must(uuid.NewV7()).String(),
			SessionID: sessionID,
			Kind:      KindMode,
			CreatedAt: now,
			Mode: &ModeChange{
				From: tools.ModePlan,
				To:   tools.ModeAuto,
			},
		},
		{
			ID:        uuid.Must(uuid.NewV7()).String(),
			SessionID: sessionID,
			Kind:      KindPermission,
			CreatedAt: now,
			Permission: &PermissionRecord{
				Request: tools.PermissionRequest{
					ID:         "req-json-1",
					ToolCallID: "tc-1",
					ToolName:   "write",
					Action:     tools.ActionWrite,
					Safety:     tools.SafetySafe,
					Target:     "foo.go",
					Summary:    "write foo.go",
				},
				Decision: tools.PermissionDecision{
					RequestID:  "req-json-1",
					ToolCallID: "tc-1",
					Kind:       tools.DecisionAllowOnce,
				},
			},
		},
	}

	for _, original := range entries {
		t.Run(string(original.Kind), func(t *testing.T) {
			data, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}

			var decoded Entry
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("json.Unmarshal failed: %v", err)
			}

			if decoded.ID != original.ID {
				t.Errorf("decoded.ID = %q, want %q", decoded.ID, original.ID)
			}
			if decoded.SessionID != original.SessionID {
				t.Errorf("decoded.SessionID = %q, want %q", decoded.SessionID, original.SessionID)
			}
			if decoded.Kind != original.Kind {
				t.Errorf("decoded.Kind = %q, want %q", decoded.Kind, original.Kind)
			}
			if !decoded.CreatedAt.Equal(original.CreatedAt) {
				t.Errorf("decoded.CreatedAt = %v, want %v", decoded.CreatedAt, original.CreatedAt)
			}

			switch original.Kind {
			case KindMessage:
				if decoded.Message == nil {
					t.Fatal("decoded.Message is nil")
				}
				if decoded.Message.Role() != original.Message.Role() {
					t.Errorf("role = %q, want %q", decoded.Message.Role(), original.Message.Role())
				}
				if decoded.Message.GetID() != original.Message.GetID() {
					t.Errorf("message id = %q, want %q", decoded.Message.GetID(), original.Message.GetID())
				}
			case KindModel:
				if *decoded.Model != *original.Model {
					t.Errorf("model = %+v, want %+v", *decoded.Model, *original.Model)
				}
			case KindEffort:
				if *decoded.Effort != *original.Effort {
					t.Errorf("effort = %+v, want %+v", *decoded.Effort, *original.Effort)
				}
			case KindAgent:
				if *decoded.Agent != *original.Agent {
					t.Errorf("agent = %+v, want %+v", *decoded.Agent, *original.Agent)
				}
			case KindMode:
				if *decoded.Mode != *original.Mode {
					t.Errorf("mode = %+v, want %+v", *decoded.Mode, *original.Mode)
				}
			case KindPermission:
				if !reflect.DeepEqual(decoded.Permission, original.Permission) {
					t.Errorf("permission = %+v, want %+v", decoded.Permission, original.Permission)
				}
			}
		})
	}
}

func TestChronologicalUUIDv7Ordering(t *testing.T) {
	store, ctx := openTestStore(t)
	sessionID := "session-ordering"

	var appendedIDs []string
	for i := 0; i < 8; i++ {
		time.Sleep(2 * time.Millisecond) // ensure distinct UUIDv7 timestamps
		entry := NewMessageEntry(sessionID, message.NewUserText("msg"))
		if err := store.Append(ctx, entry); err != nil {
			t.Fatalf("Append %d failed: %v", i, err)
		}
		appendedIDs = append(appendedIDs, entry.ID)

		// Also verify that the generated ID is a valid UUIDv7
		parsed, err := uuid.Parse(entry.ID)
		if err != nil {
			t.Fatalf("invalid UUID %q: %v", entry.ID, err)
		}
		if parsed.Version() != 7 {
			t.Fatalf("UUID %q version = %d, want 7", entry.ID, parsed.Version())
		}
	}

	// Verify that appended IDs were strictly ascending
	for i := 1; i < len(appendedIDs); i++ {
		if !(appendedIDs[i-1] < appendedIDs[i]) {
			t.Errorf("expected appendedIDs[%d] (%q) < appendedIDs[%d] (%q)", i-1, appendedIDs[i-1], i, appendedIDs[i])
		}
	}

	// Verify List returns in strict id ASC order
	list, err := store.List(ctx, sessionID)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != len(appendedIDs) {
		t.Fatalf("len(list) = %d, want %d", len(list), len(appendedIDs))
	}
	for i, entry := range list {
		if entry.ID != appendedIDs[i] {
			t.Errorf("list[%d].ID = %q, want %q", i, entry.ID, appendedIDs[i])
		}
	}

	// Verify Messages returns in strict id ASC order
	msgs, err := store.Messages(ctx, sessionID)
	if err != nil {
		t.Fatalf("Messages failed: %v", err)
	}
	if len(msgs) != len(appendedIDs) {
		t.Fatalf("len(msgs) = %d, want %d", len(msgs), len(appendedIDs))
	}
	for i, msg := range msgs {
		if msg.GetID() != appendedIDs[i] {
			t.Errorf("msgs[%d].GetID() = %q, want %q", i, msg.GetID(), appendedIDs[i])
		}
	}
}

func TestListAndListByKind(t *testing.T) {
	store, ctx := openTestStore(t)
	sessA := "session-A"
	sessB := "session-B"

	// Session A entries: 2 messages, 1 model, 1 mode
	eA1 := NewMessageEntry(sessA, message.NewUserText("A-1"))
	eA2 := NewModelEntry(sessA, ModelChange{From: "m1", To: "m2"})
	eA3 := NewMessageEntry(sessA, message.NewAssistantText("A-2"))
	eA4 := NewModeEntry(sessA, ModeChange{From: tools.ModeDefault, To: tools.ModeAsk})

	for _, e := range []*Entry{eA1, eA2, eA3, eA4} {
		if err := store.Append(ctx, e); err != nil {
			t.Fatalf("Append to %s failed: %v", sessA, err)
		}
	}

	// Session B entries: 1 message, 1 effort
	eB1 := NewMessageEntry(sessB, message.NewUserText("B-1"))
	eB2 := NewEffortEntry(sessB, EffortChange{From: "low", To: "high"})

	for _, e := range []*Entry{eB1, eB2} {
		if err := store.Append(ctx, e); err != nil {
			t.Fatalf("Append to %s failed: %v", sessB, err)
		}
	}

	// Verify List for SessA
	listA, err := store.List(ctx, sessA)
	if err != nil {
		t.Fatalf("List(%q) failed: %v", sessA, err)
	}
	if len(listA) != 4 {
		t.Fatalf("len(listA) = %d, want 4", len(listA))
	}

	// Verify List for SessB
	listB, err := store.List(ctx, sessB)
	if err != nil {
		t.Fatalf("List(%q) failed: %v", sessB, err)
	}
	if len(listB) != 2 {
		t.Fatalf("len(listB) = %d, want 2", len(listB))
	}

	// Verify ListByKind for SessA messages
	msgsA, err := store.ListByKind(ctx, sessA, KindMessage)
	if err != nil {
		t.Fatalf("ListByKind(sessA, KindMessage) failed: %v", err)
	}
	if len(msgsA) != 2 {
		t.Fatalf("len(msgsA) = %d, want 2", len(msgsA))
	}
	if msgsA[0].ID != eA1.ID || msgsA[1].ID != eA3.ID {
		t.Errorf("msgsA IDs = [%q, %q], want [%q, %q]", msgsA[0].ID, msgsA[1].ID, eA1.ID, eA3.ID)
	}

	// Verify ListByKind for SessA model
	modelsA, err := store.ListByKind(ctx, sessA, KindModel)
	if err != nil {
		t.Fatalf("ListByKind(sessA, KindModel) failed: %v", err)
	}
	if len(modelsA) != 1 || modelsA[0].ID != eA2.ID {
		t.Errorf("modelsA len/ID mismatch")
	}

	// Verify ListByKind for nonexistent kind in SessA returns empty slice
	effortsA, err := store.ListByKind(ctx, sessA, KindEffort)
	if err != nil {
		t.Fatalf("ListByKind(sessA, KindEffort) failed: %v", err)
	}
	if len(effortsA) != 0 {
		t.Errorf("len(effortsA) = %d, want 0", len(effortsA))
	}
}

func TestMessages(t *testing.T) {
	store, ctx := openTestStore(t)
	sessionID := "session-messages-test"

	// Append mix of message and non-message entries
	if err := store.Append(ctx, NewModelEntry(sessionID, ModelChange{From: "m1", To: "m2"})); err != nil {
		t.Fatalf("Append model: %v", err)
	}
	e1, err := store.AppendMessage(ctx, sessionID, message.NewUserText("Hello"))
	if err != nil {
		t.Fatalf("AppendMessage 1: %v", err)
	}
	if err := store.Append(ctx, NewEffortEntry(sessionID, EffortChange{From: "low", To: "high"})); err != nil {
		t.Fatalf("Append effort: %v", err)
	}
	e2, err := store.AppendMessage(ctx, sessionID, message.NewAssistantText("Hi"))
	if err != nil {
		t.Fatalf("AppendMessage 2: %v", err)
	}

	msgs, err := store.Messages(ctx, sessionID)
	if err != nil {
		t.Fatalf("Messages failed: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
	if msgs[0].GetID() != e1.ID || msgs[1].GetID() != e2.ID {
		t.Errorf("message IDs = [%q, %q], want [%q, %q]", msgs[0].GetID(), msgs[1].GetID(), e1.ID, e2.ID)
	}

	// Nonexistent session returns empty MessageList
	emptyMsgs, err := store.Messages(ctx, "session-none")
	if err != nil {
		t.Fatalf("Messages empty session failed: %v", err)
	}
	if len(emptyMsgs) != 0 {
		t.Errorf("len(emptyMsgs) = %d, want 0", len(emptyMsgs))
	}
}

func TestDeleteSession(t *testing.T) {
	store, ctx := openTestStore(t)
	sess1 := "session-del-1"
	sess2 := "session-del-2"

	e1, err := store.AppendMessage(ctx, sess1, message.NewUserText("msg1"))
	if err != nil {
		t.Fatalf("AppendMessage sess1: %v", err)
	}
	e2, err := store.AppendMessage(ctx, sess2, message.NewUserText("msg2"))
	if err != nil {
		t.Fatalf("AppendMessage sess2: %v", err)
	}

	// Delete sess1
	if err := store.DeleteSession(ctx, sess1); err != nil {
		t.Fatalf("DeleteSession(%q) failed: %v", sess1, err)
	}

	// Verify sess1 is empty
	list1, err := store.List(ctx, sess1)
	if err != nil {
		t.Fatalf("List(%q) failed: %v", sess1, err)
	}
	if len(list1) != 0 {
		t.Errorf("len(list1) = %d, want 0", len(list1))
	}

	// Verify Get for sess1 entry returns ErrEntryNotFound
	_, err = store.Get(ctx, e1.ID)
	if !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("Get(%q) error = %v, want %v", e1.ID, err, ErrEntryNotFound)
	}

	// Verify sess2 remains intact
	list2, err := store.List(ctx, sess2)
	if err != nil {
		t.Fatalf("List(%q) failed: %v", sess2, err)
	}
	if len(list2) != 1 || list2[0].ID != e2.ID {
		t.Errorf("sess2 entries modified unexpectedly")
	}

	loaded2, err := store.Get(ctx, e2.ID)
	if err != nil || loaded2.ID != e2.ID {
		t.Errorf("Get(%q) for sess2 failed: %v", e2.ID, err)
	}
}

func TestErrorCases(t *testing.T) {
	store, ctx := openTestStore(t)
	validSession := "valid-session"

	// 1. Append nil entry
	if err := store.Append(ctx, nil); !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("Append(nil) error = %v, want %v", err, ErrInvalidEntry)
	}

	// 2. Append empty or whitespace session ID
	for _, emptyID := range []string{"", "   ", "\t\n"} {
		entry := NewMessageEntry(emptyID, message.NewUserText("hi"))
		if err := store.Append(ctx, entry); !errors.Is(err, ErrEmptySessionID) {
			t.Errorf("Append(sessionID=%q) error = %v, want %v", emptyID, err, ErrEmptySessionID)
		}
	}

	// 3. Invalid / missing payloads per Kind
	invalidCases := []struct {
		name  string
		entry *Entry
	}{
		{
			name:  "KindMessage nil",
			entry: &Entry{SessionID: validSession, Kind: KindMessage, Message: nil},
		},
		{
			name:  "KindModel nil",
			entry: &Entry{SessionID: validSession, Kind: KindModel, Model: nil},
		},
		{
			name:  "KindModel empty To",
			entry: &Entry{SessionID: validSession, Kind: KindModel, Model: &ModelChange{To: ""}},
		},
		{
			name:  "KindModel whitespace To",
			entry: &Entry{SessionID: validSession, Kind: KindModel, Model: &ModelChange{To: "   "}},
		},
		{
			name:  "KindEffort nil",
			entry: &Entry{SessionID: validSession, Kind: KindEffort, Effort: nil},
		},
		{
			name:  "KindEffort empty To",
			entry: &Entry{SessionID: validSession, Kind: KindEffort, Effort: &EffortChange{To: ""}},
		},
		{
			name:  "KindAgent nil",
			entry: &Entry{SessionID: validSession, Kind: KindAgent, Agent: nil},
		},
		{
			name:  "KindAgent empty To",
			entry: &Entry{SessionID: validSession, Kind: KindAgent, Agent: &AgentChange{To: ""}},
		},
		{
			name:  "KindMode nil",
			entry: &Entry{SessionID: validSession, Kind: KindMode, Mode: nil},
		},
		{
			name:  "KindMode empty To",
			entry: &Entry{SessionID: validSession, Kind: KindMode, Mode: &ModeChange{To: ""}},
		},
		{
			name:  "KindPermission nil",
			entry: &Entry{SessionID: validSession, Kind: KindPermission, Permission: nil},
		},
		{
			name:  "Unknown Kind",
			entry: &Entry{SessionID: validSession, Kind: "nonexistent"},
		},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := store.Append(ctx, tc.entry); !errors.Is(err, ErrInvalidEntry) {
				t.Errorf("Append() error = %v, want %v", err, ErrInvalidEntry)
			}
		})
	}

	// 4. AppendMessage errors
	if _, err := store.AppendMessage(ctx, "", message.NewUserText("hi")); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("AppendMessage(empty) error = %v, want %v", err, ErrEmptySessionID)
	}
	if _, err := store.AppendMessage(ctx, validSession, nil); !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("AppendMessage(nil msg) error = %v, want %v", err, ErrInvalidEntry)
	}

	// 5. Get errors
	if _, err := store.Get(ctx, ""); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("Get(\"\") error = %v, want %v", err, ErrEntryNotFound)
	}
	if _, err := store.Get(ctx, "   "); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("Get(\"   \") error = %v, want %v", err, ErrEntryNotFound)
	}
	if _, err := store.Get(ctx, "does-not-exist"); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("Get(nonexistent) error = %v, want %v", err, ErrEntryNotFound)
	}

	// 6. List errors
	if _, err := store.List(ctx, ""); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("List(\"\") error = %v, want %v", err, ErrEmptySessionID)
	}
	if _, err := store.List(ctx, "   "); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("List(\"   \") error = %v, want %v", err, ErrEmptySessionID)
	}

	// 7. ListByKind errors
	if _, err := store.ListByKind(ctx, "", KindMessage); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("ListByKind(\"\") error = %v, want %v", err, ErrEmptySessionID)
	}
	if _, err := store.ListByKind(ctx, "   ", KindMessage); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("ListByKind(\"   \") error = %v, want %v", err, ErrEmptySessionID)
	}

	// 8. Messages errors
	if _, err := store.Messages(ctx, ""); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("Messages(\"\") error = %v, want %v", err, ErrEmptySessionID)
	}
	if _, err := store.Messages(ctx, "   "); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("Messages(\"   \") error = %v, want %v", err, ErrEmptySessionID)
	}

	// 9. DeleteSession errors
	if err := store.DeleteSession(ctx, ""); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("DeleteSession(\"\") error = %v, want %v", err, ErrEmptySessionID)
	}
	if err := store.DeleteSession(ctx, "   "); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("DeleteSession(\"   \") error = %v, want %v", err, ErrEmptySessionID)
	}
}
