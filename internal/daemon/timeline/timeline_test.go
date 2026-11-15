package timeline

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
)

func openTestTimeline(t *testing.T, sessionID string) (Timeline, *sql.DB, context.Context) {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "timeline_test.db")
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	tl, err := New(ctx, db, sessionID)
	if err != nil {
		t.Fatalf("timeline.New: %v", err)
	}
	return tl, db, ctx
}

func TestNew(t *testing.T) {
	ctx := context.Background()

	// 1. Missing DB
	_, err := New(ctx, nil, "session-1")
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New(ctx, nil, s) error = %v, want %v", err, ErrInvalidConfig)
	}

	// 2. Empty session ID
	dbPath := filepath.Join(t.TempDir(), "new_test.db")
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer db.Close()

	for _, emptyID := range []string{"", "   ", "\t\n"} {
		_, err := New(ctx, db, emptyID)
		if !errors.Is(err, ErrEmptySessionID) {
			t.Fatalf("New(ctx, db, %q) error = %v, want %v", emptyID, err, ErrEmptySessionID)
		}
	}

	// 3. Valid DB and session ID
	tl, err := New(ctx, db, "session-1")
	if err != nil {
		t.Fatalf("New(ctx, db, s) unexpected error: %v", err)
	}
	if tl == nil {
		t.Fatal("New(ctx, db, s) returned nil timeline")
	}

	// 4. Idempotent New
	tl2, err := New(ctx, db, "session-1")
	if err != nil {
		t.Fatalf("second New(ctx, db, s) error: %v", err)
	}
	if tl2 == nil {
		t.Fatal("second New(ctx, db, s) returned nil timeline")
	}
}

func TestAppendAndGet_AllKinds(t *testing.T) {
	sessionID := "session-all-kinds"
	tl, _, ctx := openTestTimeline(t, sessionID)

	tests := []struct {
		name      string
		makeEntry func() *Entry
		verify    func(t *testing.T, original, loaded *Entry)
	}{
		{
			name: "message user",
			makeEntry: func() *Entry {
				return NewMessageEntry(message.NewUserText("Hello TaskSmith"))
			},
			verify: func(t *testing.T, original, loaded *Entry) {
				if loaded.Kind != api.TimelineKindMessage {
					t.Fatalf("loaded.Kind = %q, want %q", loaded.Kind, api.TimelineKindMessage)
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
				return NewMessageEntry(message.NewAssistantText("Hello user!"))
			},
			verify: func(t *testing.T, original, loaded *Entry) {
				if loaded.Kind != api.TimelineKindMessage {
					t.Fatalf("loaded.Kind = %q, want %q", loaded.Kind, api.TimelineKindMessage)
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
				return NewModelEntry(api.ModelChange{
					From:   "claude-3-haiku",
					To:     "claude-3-5-sonnet",
					Effort: "high",
				})
			},
			verify: func(t *testing.T, original, loaded *Entry) {
				if loaded.Kind != api.TimelineKindModel {
					t.Fatalf("loaded.Kind = %q, want %q", loaded.Kind, api.TimelineKindModel)
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
				return NewEffortEntry(api.EffortChange{
					From: "low",
					To:   "high",
				})
			},
			verify: func(t *testing.T, original, loaded *Entry) {
				if loaded.Kind != api.TimelineKindEffort {
					t.Fatalf("loaded.Kind = %q, want %q", loaded.Kind, api.TimelineKindEffort)
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
				return NewAgentEntry(api.AgentChange{
					From: "coder",
					To:   "planner",
				})
			},
			verify: func(t *testing.T, original, loaded *Entry) {
				if loaded.Kind != api.TimelineKindAgent {
					t.Fatalf("loaded.Kind = %q, want %q", loaded.Kind, api.TimelineKindAgent)
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
				return NewModeEntry(api.ModeChange{
					From: api.ModeDefault,
					To:   api.ModeAuto,
				})
			},
			verify: func(t *testing.T, original, loaded *Entry) {
				if loaded.Kind != api.TimelineKindMode {
					t.Fatalf("loaded.Kind = %q, want %q", loaded.Kind, api.TimelineKindMode)
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
				return NewPermissionEntry(api.PermissionRecord{
					Request: api.PermissionRequest{
						ID:            "perm-req-1",
						ToolCallID:    "call-1",
						ToolName:      "bash",
						Action:        api.ActionExec,
						Safety:        api.SafetyUnsafe,
						Summary:       "run build script",
						GrantPatterns: []string{"bash*"},
					},
					Decision: api.PermissionDecision{
						RequestID:    "perm-req-1",
						ToolCallID:   "call-1",
						Kind:         api.DecisionAllowSession,
						GrantPattern: "bash*",
					},
				})
			},
			verify: func(t *testing.T, original, loaded *Entry) {
				if loaded.Kind != api.TimelineKindPermission {
					t.Fatalf("loaded.Kind = %q, want %q", loaded.Kind, api.TimelineKindPermission)
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
		{
			name: "compacted record",
			makeEntry: func() *Entry {
				return NewCompactedEntry(api.CompactedRecord{
					Trigger:      "auto",
					Focus:        "test focus",
					Summary:      "## Summary",
					BeforeTokens: 100000,
					AfterTokens:  10000,
					DurationMs:   1500,
				})
			},
			verify: func(t *testing.T, original, loaded *Entry) {
				if loaded.Kind != api.TimelineKindCompacted {
					t.Fatalf("loaded.Kind = %q, want %q", loaded.Kind, api.TimelineKindCompacted)
				}
				if loaded.Compacted == nil {
					t.Fatal("loaded.Compacted is nil")
				}
				if *loaded.Compacted != *original.Compacted {
					t.Errorf("loaded.Compacted = %+v, want %+v", *loaded.Compacted, *original.Compacted)
				}
			},
		},
		{
			name: "cache broken record",
			makeEntry: func() *Entry {
				return NewCacheBrokenEntry(api.CacheBrokenRecord{
					PrevCached:  80000,
					CurrCached:  1000,
					InputTokens: 85000,
				})
			},
			verify: func(t *testing.T, original, loaded *Entry) {
				if loaded.Kind != api.TimelineKindCacheBroken {
					t.Fatalf("loaded.Kind = %q, want %q", loaded.Kind, api.TimelineKindCacheBroken)
				}
				if loaded.CacheBroken == nil {
					t.Fatal("loaded.CacheBroken is nil")
				}
				if *loaded.CacheBroken != *original.CacheBroken {
					t.Errorf("loaded.CacheBroken = %+v, want %+v", *loaded.CacheBroken, *original.CacheBroken)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := tt.makeEntry()
			if err := tl.Append(ctx, entry); err != nil {
				t.Fatalf("Append failed: %v", err)
			}
			if entry.ID == "" {
				t.Fatal("Append did not assign an ID")
			}
			if entry.CreatedAt.IsZero() {
				t.Fatal("Append did not set CreatedAt")
			}

			loaded, err := tl.Get(ctx, entry.ID)
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

func TestAppend_MessageHandling(t *testing.T) {
	sessionID := "session-append-msg"
	tl, _, ctx := openTestTimeline(t, sessionID)

	// 1. Message without an existing ID
	userMsg := message.NewUserText("User question")
	entry1 := NewMessageEntry(userMsg)
	if err := tl.Append(ctx, entry1); err != nil {
		t.Fatalf("Append failed: %v", err)
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

	entry2 := NewMessageEntry(asstMsg)
	if err := tl.Append(ctx, entry2); err != nil {
		t.Fatalf("Append with existing ID failed: %v", err)
	}
	if entry2.ID != existingID {
		t.Fatalf("entry2.ID = %q, want %q", entry2.ID, existingID)
	}

	// 3. Messages retrieval
	msgs, err := tl.Messages(ctx)
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

func TestChronologicalUUIDv7Ordering(t *testing.T) {
	sessionID := "session-ordering"
	tl, _, ctx := openTestTimeline(t, sessionID)

	var appendedIDs []string
	for i := range 8 {
		time.Sleep(2 * time.Millisecond) // ensure distinct UUIDv7 timestamps
		entry := NewMessageEntry(message.NewUserText("msg"))
		if err := tl.Append(ctx, entry); err != nil {
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
	list, err := tl.List(ctx)
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
	msgs, err := tl.Messages(ctx)
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

func TestList(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "timeline_test.db")
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer db.Close()

	sessA := "session-A"
	sessB := "session-B"

	tlA, err := New(ctx, db, sessA)
	if err != nil {
		t.Fatalf("New A: %v", err)
	}
	tlB, err := New(ctx, db, sessB)
	if err != nil {
		t.Fatalf("New B: %v", err)
	}

	// Session A entries: 2 messages, 1 model, 1 mode
	eA1 := NewMessageEntry(message.NewUserText("A-1"))
	eA2 := NewModelEntry(api.ModelChange{From: "m1", To: "m2"})
	eA3 := NewMessageEntry(message.NewAssistantText("A-2"))
	eA4 := NewModeEntry(api.ModeChange{From: api.ModeDefault, To: api.ModeAsk})

	for _, e := range []*Entry{eA1, eA2, eA3, eA4} {
		if err := tlA.Append(ctx, e); err != nil {
			t.Fatalf("Append to %s failed: %v", sessA, err)
		}
	}

	// Session B entries: 1 message, 1 effort
	eB1 := NewMessageEntry(message.NewUserText("B-1"))
	eB2 := NewEffortEntry(api.EffortChange{From: "low", To: "high"})

	for _, e := range []*Entry{eB1, eB2} {
		if err := tlB.Append(ctx, e); err != nil {
			t.Fatalf("Append to %s failed: %v", sessB, err)
		}
	}

	// Verify List for SessA
	listA, err := tlA.List(ctx)
	if err != nil {
		t.Fatalf("List(%q) failed: %v", sessA, err)
	}
	if len(listA) != 4 {
		t.Fatalf("len(listA) = %d, want 4", len(listA))
	}
	if listA[0].ID != eA1.ID || listA[1].ID != eA2.ID || listA[2].ID != eA3.ID || listA[3].ID != eA4.ID {
		t.Errorf("listA IDs = [%q, %q, %q, %q], want [%q, %q, %q, %q]",
			listA[0].ID, listA[1].ID, listA[2].ID, listA[3].ID,
			eA1.ID, eA2.ID, eA3.ID, eA4.ID)
	}

	// Verify List for SessB
	listB, err := tlB.List(ctx)
	if err != nil {
		t.Fatalf("List(%q) failed: %v", sessB, err)
	}
	if len(listB) != 2 {
		t.Fatalf("len(listB) = %d, want 2", len(listB))
	}
	if listB[0].ID != eB1.ID || listB[1].ID != eB2.ID {
		t.Errorf("listB IDs = [%q, %q], want [%q, %q]",
			listB[0].ID, listB[1].ID, eB1.ID, eB2.ID)
	}
}

func TestMessages(t *testing.T) {
	sessionID := "session-messages-test"
	tl, _, ctx := openTestTimeline(t, sessionID)

	// Append mix of message and non-message entries
	if err := tl.Append(ctx, NewModelEntry(api.ModelChange{From: "m1", To: "m2"})); err != nil {
		t.Fatalf("Append model: %v", err)
	}
	e1 := NewMessageEntry(message.NewUserText("Hello"))
	if err := tl.Append(ctx, e1); err != nil {
		t.Fatalf("Append message 1: %v", err)
	}
	if err := tl.Append(ctx, NewEffortEntry(api.EffortChange{From: "low", To: "high"})); err != nil {
		t.Fatalf("Append effort: %v", err)
	}
	e2 := NewMessageEntry(message.NewAssistantText("Hi"))
	if err := tl.Append(ctx, e2); err != nil {
		t.Fatalf("Append message 2: %v", err)
	}

	msgs, err := tl.Messages(ctx)
	if err != nil {
		t.Fatalf("Messages failed: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
	if msgs[0].GetID() != e1.ID || msgs[1].GetID() != e2.ID {
		t.Errorf("message IDs = [%q, %q], want [%q, %q]", msgs[0].GetID(), msgs[1].GetID(), e1.ID, e2.ID)
	}
}

func TestDelete(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "timeline_test.db")
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer db.Close()

	sess1 := "session-del-1"
	sess2 := "session-del-2"

	tl1, err := New(ctx, db, sess1)
	if err != nil {
		t.Fatalf("New tl1: %v", err)
	}
	tl2, err := New(ctx, db, sess2)
	if err != nil {
		t.Fatalf("New tl2: %v", err)
	}

	e1 := NewMessageEntry(message.NewUserText("msg1"))
	if err := tl1.Append(ctx, e1); err != nil {
		t.Fatalf("Append sess1: %v", err)
	}
	e2 := NewMessageEntry(message.NewUserText("msg2"))
	if err := tl2.Append(ctx, e2); err != nil {
		t.Fatalf("Append sess2: %v", err)
	}

	// Delete sess1
	if err := tl1.Delete(ctx); err != nil {
		t.Fatalf("Delete sess1 failed: %v", err)
	}

	// Verify sess1 is empty
	list1, err := tl1.List(ctx)
	if err != nil {
		t.Fatalf("List(%q) failed: %v", sess1, err)
	}
	if len(list1) != 0 {
		t.Errorf("len(list1) = %d, want 0", len(list1))
	}

	// Verify Get for sess1 entry returns ErrEntryNotFound
	_, err = tl1.Get(ctx, e1.ID)
	if !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("Get(%q) error = %v, want %v", e1.ID, err, ErrEntryNotFound)
	}

	// Verify sess2 remains intact
	list2, err := tl2.List(ctx)
	if err != nil {
		t.Fatalf("List(%q) failed: %v", sess2, err)
	}
	if len(list2) != 1 || list2[0].ID != e2.ID {
		t.Errorf("sess2 entries modified unexpectedly")
	}

	loaded2, err := tl2.Get(ctx, e2.ID)
	if err != nil || loaded2.ID != e2.ID {
		t.Errorf("Get(%q) for sess2 failed: %v", e2.ID, err)
	}
}

func TestErrorCases(t *testing.T) {
	validSession := "valid-session"
	tl, _, ctx := openTestTimeline(t, validSession)

	// 1. Append nil entry
	if err := tl.Append(ctx, nil); !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("Append(nil) error = %v, want %v", err, ErrInvalidEntry)
	}

	// 2. Append mismatched session ID
	mismatchedEntry := NewMessageEntry(message.NewUserText("hi"))
	mismatchedEntry.SessionID = "other-session"
	if err := tl.Append(ctx, mismatchedEntry); !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("Append(mismatched) error = %v, want %v", err, ErrInvalidEntry)
	}

	// 3. Invalid / missing payloads per Kind
	invalidCases := []struct {
		name  string
		entry *Entry
	}{
		{
			name:  "api.TimelineKindMessage nil",
			entry: &Entry{TimelineEntry: api.TimelineEntry{SessionID: validSession, Kind: api.TimelineKindMessage}, Message: nil},
		},
		{
			name:  "api.TimelineKindModel nil",
			entry: &Entry{TimelineEntry: api.TimelineEntry{SessionID: validSession, Kind: api.TimelineKindModel, Model: nil}},
		},
		{
			name:  "api.TimelineKindModel empty To",
			entry: &Entry{TimelineEntry: api.TimelineEntry{SessionID: validSession, Kind: api.TimelineKindModel, Model: &api.ModelChange{To: ""}}},
		},
		{
			name:  "api.TimelineKindModel whitespace To",
			entry: &Entry{TimelineEntry: api.TimelineEntry{SessionID: validSession, Kind: api.TimelineKindModel, Model: &api.ModelChange{To: "   "}}},
		},
		{
			name:  "api.TimelineKindEffort nil",
			entry: &Entry{TimelineEntry: api.TimelineEntry{SessionID: validSession, Kind: api.TimelineKindEffort, Effort: nil}},
		},
		{
			name:  "api.TimelineKindEffort empty To",
			entry: &Entry{TimelineEntry: api.TimelineEntry{SessionID: validSession, Kind: api.TimelineKindEffort, Effort: &api.EffortChange{To: ""}}},
		},
		{
			name:  "api.TimelineKindAgent nil",
			entry: &Entry{TimelineEntry: api.TimelineEntry{SessionID: validSession, Kind: api.TimelineKindAgent, Agent: nil}},
		},
		{
			name:  "api.TimelineKindAgent empty To",
			entry: &Entry{TimelineEntry: api.TimelineEntry{SessionID: validSession, Kind: api.TimelineKindAgent, Agent: &api.AgentChange{To: ""}}},
		},
		{
			name:  "api.TimelineKindMode nil",
			entry: &Entry{TimelineEntry: api.TimelineEntry{SessionID: validSession, Kind: api.TimelineKindMode, Mode: nil}},
		},
		{
			name:  "api.TimelineKindMode empty To",
			entry: &Entry{TimelineEntry: api.TimelineEntry{SessionID: validSession, Kind: api.TimelineKindMode, Mode: &api.ModeChange{To: ""}}},
		},
		{
			name:  "api.TimelineKindPermission nil",
			entry: &Entry{TimelineEntry: api.TimelineEntry{SessionID: validSession, Kind: api.TimelineKindPermission, Permission: nil}},
		},
		{
			name:  "api.TimelineKindCompacted nil",
			entry: &Entry{TimelineEntry: api.TimelineEntry{SessionID: validSession, Kind: api.TimelineKindCompacted, Compacted: nil}},
		},
		{
			name:  "api.TimelineKindCacheBroken nil",
			entry: &Entry{TimelineEntry: api.TimelineEntry{SessionID: validSession, Kind: api.TimelineKindCacheBroken, CacheBroken: nil}},
		},
		{
			name:  "Unknown Kind",
			entry: &Entry{TimelineEntry: api.TimelineEntry{SessionID: validSession, Kind: "nonexistent"}},
		},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tl.Append(ctx, tc.entry); !errors.Is(err, ErrInvalidEntry) {
				t.Errorf("Append() error = %v, want %v", err, ErrInvalidEntry)
			}
		})
	}

	// 4. Get errors
	if _, err := tl.Get(ctx, ""); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("Get(\"\") error = %v, want %v", err, ErrEntryNotFound)
	}
	if _, err := tl.Get(ctx, "   "); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("Get(\"   \") error = %v, want %v", err, ErrEntryNotFound)
	}
	if _, err := tl.Get(ctx, "does-not-exist"); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("Get(nonexistent) error = %v, want %v", err, ErrEntryNotFound)
	}
}

func TestInterruptedEntry(t *testing.T) {
	sessionID := "sess-interrupted"
	tl, _, ctx := openTestTimeline(t, sessionID)

	entry := NewInterruptedEntry()
	if entry.Kind != api.TimelineKindInterrupted {
		t.Fatalf("expected api.TimelineKindInterrupted, got %q", entry.Kind)
	}

	if err := tl.Append(ctx, entry); err != nil {
		t.Fatalf("Append(NewInterruptedEntry): %v", err)
	}
	if entry.ID == "" {
		t.Fatal("expected non-empty generated ID")
	}

	got, err := tl.Get(ctx, entry.ID)
	if err != nil {
		t.Fatalf("Get(%q): %v", entry.ID, err)
	}
	if got.Kind != api.TimelineKindInterrupted {
		t.Fatalf("got kind %q, want %q", got.Kind, api.TimelineKindInterrupted)
	}
	if got.SessionID != sessionID {
		t.Fatalf("got sessionID %q, want %q", got.SessionID, sessionID)
	}

	list, err := tl.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Kind != api.TimelineKindInterrupted {
		t.Fatalf("unexpected List result: %+v", list)
	}
}

func TestCompactedAndCacheBrokenEntries(t *testing.T) {
	sessionID := "sess-comp-cb"
	tl, _, ctx := openTestTimeline(t, sessionID)

	// 1. Compacted Entry
	compactRec := api.CompactedRecord{
		Trigger:      "auto",
		Focus:        "architecture",
		Summary:      "## Current State\n- building API types",
		BeforeTokens: 120000,
		AfterTokens:  15000,
		DurationMs:   2150,
	}
	compEntry := NewCompactedEntry(compactRec)
	if compEntry.Kind != api.TimelineKindCompacted {
		t.Fatalf("expected api.TimelineKindCompacted, got %q", compEntry.Kind)
	}
	if err := tl.Append(ctx, compEntry); err != nil {
		t.Fatalf("Append(NewCompactedEntry): %v", err)
	}
	if compEntry.SessionID != sessionID {
		t.Fatalf("expected sessionID %q, got %q", sessionID, compEntry.SessionID)
	}
	if compEntry.ID == "" {
		t.Fatal("expected non-empty generated ID")
	}

	gotComp, err := tl.Get(ctx, compEntry.ID)
	if err != nil {
		t.Fatalf("Get(%q): %v", compEntry.ID, err)
	}
	if gotComp.Kind != api.TimelineKindCompacted || gotComp.Compacted == nil {
		t.Fatalf("gotComp = %+v, want api.TimelineKindCompacted with Compacted != nil", gotComp)
	}
	if *gotComp.Compacted != compactRec {
		t.Errorf("gotComp.Compacted = %+v, want %+v", *gotComp.Compacted, compactRec)
	}

	// 2. Cache Broken Entry
	cbRec := api.CacheBrokenRecord{
		PrevCached:  64000,
		CurrCached:  0,
		InputTokens: 70000,
	}
	cbEntry := NewCacheBrokenEntry(cbRec)
	if cbEntry.Kind != api.TimelineKindCacheBroken {
		t.Fatalf("expected api.TimelineKindCacheBroken, got %q", cbEntry.Kind)
	}
	if err := tl.Append(ctx, cbEntry); err != nil {
		t.Fatalf("Append(NewCacheBrokenEntry): %v", err)
	}
	if cbEntry.SessionID != sessionID {
		t.Fatalf("expected sessionID %q, got %q", sessionID, cbEntry.SessionID)
	}
	if cbEntry.ID == "" {
		t.Fatal("expected non-empty generated ID")
	}

	gotCB, err := tl.Get(ctx, cbEntry.ID)
	if err != nil {
		t.Fatalf("Get(%q): %v", cbEntry.ID, err)
	}
	if gotCB.Kind != api.TimelineKindCacheBroken || gotCB.CacheBroken == nil {
		t.Fatalf("gotCB = %+v, want api.TimelineKindCacheBroken with CacheBroken != nil", gotCB)
	}
	if *gotCB.CacheBroken != cbRec {
		t.Errorf("gotCB.CacheBroken = %+v, want %+v", *gotCB.CacheBroken, cbRec)
	}

	// List checks
	list, err := tl.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2", len(list))
	}
	if list[0].ID != compEntry.ID || list[1].ID != cbEntry.ID {
		t.Fatalf("list IDs = [%q, %q], want [%q, %q]", list[0].ID, list[1].ID, compEntry.ID, cbEntry.ID)
	}
}

func TestEntryClone(t *testing.T) {
	now := time.Now().UTC()
	original := Entry{
		TimelineEntry: api.TimelineEntry{
			ID:        "entry-1",
			SessionID: "sess-1",
			Kind:      api.TimelineKindCompacted,
			CreatedAt: now,
			Command: &api.CommandRecord{
				Command:    "echo test",
				Output:     "test",
				ExitCode:   0,
				DurationMs: 10,
			},
			Model: &api.ModelChange{
				From:   "model-a",
				To:     "model-b",
				Effort: "high",
			},
			Effort: &api.EffortChange{
				From: "low",
				To:   "high",
			},
			Agent: &api.AgentChange{
				From: "agent-a",
				To:   "agent-b",
			},
			Mode: &api.ModeChange{
				From: api.ModeAsk,
				To:   api.ModeAuto,
			},
			Permission: &api.PermissionRecord{
				Request: api.PermissionRequest{
					ID:            "req-1",
					GrantPatterns: []string{"pattern-1"},
				},
				Decision: api.PermissionDecision{
					RequestID: "req-1",
				},
			},
			Compacted: &api.CompactedRecord{
				Trigger:      "manual",
				Focus:        "focus-orig",
				Summary:      "summary-orig",
				BeforeTokens: 50000,
				AfterTokens:  5000,
				DurationMs:   1200,
			},
			CacheBroken: &api.CacheBrokenRecord{
				PrevCached:  30000,
				CurrCached:  0,
				InputTokens: 32000,
			},
		},
	}

	cloned := original.Clone()

	// Verify values match
	if cloned.ID != original.ID || cloned.SessionID != original.SessionID || cloned.Kind != original.Kind {
		t.Fatalf("cloned header fields mismatch: %+v vs %+v", cloned, original)
	}
	if *cloned.Compacted != *original.Compacted {
		t.Fatalf("cloned Compacted mismatch: %+v vs %+v", *cloned.Compacted, *original.Compacted)
	}
	if *cloned.CacheBroken != *original.CacheBroken {
		t.Fatalf("cloned CacheBroken mismatch: %+v vs %+v", *cloned.CacheBroken, *original.CacheBroken)
	}

	// Verify deep copy isolation: mutating original doesn't affect clone
	original.Compacted.Focus = "focus-mutated"
	original.CacheBroken.PrevCached = 99999
	original.Command.Command = "mutated cmd"

	if cloned.Compacted.Focus != "focus-orig" {
		t.Errorf("cloned Compacted was mutated: got %q, want %q", cloned.Compacted.Focus, "focus-orig")
	}
	if cloned.CacheBroken.PrevCached != 30000 {
		t.Errorf("cloned CacheBroken was mutated: got %d, want 30000", cloned.CacheBroken.PrevCached)
	}
	if cloned.Command.Command != "echo test" {
		t.Errorf("cloned Command was mutated: got %q, want %q", cloned.Command.Command, "echo test")
	}

	// Verify empty clone works
	emptyEntry := Entry{TimelineEntry: api.TimelineEntry{ID: "empty"}}
	emptyCloned := emptyEntry.Clone()
	if emptyCloned.Compacted != nil || emptyCloned.CacheBroken != nil {
		t.Errorf("empty cloned has non-nil records: %+v", emptyCloned)
	}
}
