package session_test

import (
	"context"
	"testing"

	"github.com/masterkeysrd/loom/message"
	coredb "github.com/masterkeysrd/tasksmith/internal/core/db"
	"github.com/masterkeysrd/tasksmith/internal/session"
)

func TestSessionManager(t *testing.T) {
	// Create a temporary directory to act as the test workspace
	tmpCwd := t.TempDir()

	// Redirect XDG directories during the test to avoid polluting the user's home directories
	t.Setenv("XDG_DATA_HOME", tmpCwd)
	t.Setenv("TASKSMITH_APPNAME", "tasksmith-test")

	// 1. Open the DB connections using the core database package
	db, err := coredb.Open(tmpCwd, "tasksmith.db")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	checkpointsDb, err := coredb.Open(tmpCwd, "checkpoints.db")
	if err != nil {
		t.Fatalf("failed to open checkpoints database: %v", err)
	}
	defer checkpointsDb.Close()

	// 2. Initialize SQLite Store
	store, err := session.NewSQLiteStore(db, checkpointsDb)
	if err != nil {
		t.Fatalf("failed to initialize sqlite store: %v", err)
	}

	// 3. Initialize the Manager business logic
	manager := session.NewManager(store)

	ctx := context.Background()

	// 4. Create a session
	s1, err := manager.CreateSession(ctx, "test-session-1")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	if s1.Title != "test-session-1" {
		t.Errorf("unexpected session created: %+v", s1)
	}

	if s1.ID == "" {
		t.Error("expected generated session ID to be non-empty")
	}

	// 5. Get the session
	gotS1, err := manager.GetSession(ctx, s1.ID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if gotS1.Title != "test-session-1" {
		t.Errorf("expected session title test-session-1, got %s", gotS1.Title)
	}

	// 6. Append messages
	msgUser := message.NewUserText("Hello agent")
	id1, err := manager.AppendMessage(ctx, s1.ID, msgUser)
	if err != nil {
		t.Fatalf("failed to append user message: %v", err)
	}
	if id1 == "" {
		t.Error("expected generated message ID to be non-empty")
	}

	msgAsst := message.NewAssistantText("Hello human")
	id2, err := manager.AppendMessage(ctx, s1.ID, msgAsst)
	if err != nil {
		t.Fatalf("failed to append assistant message: %v", err)
	}
	if id2 == "" {
		t.Error("expected generated message ID to be non-empty")
	}

	// 7. Retrieve messages
	msgs, err := manager.GetMessages(ctx, s1.ID)
	if err != nil {
		t.Fatalf("failed to get messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role() != message.RoleUser || msgs[0].GetContent().Text() != "Hello agent" {
		t.Errorf("unexpected message [0]: %+v", msgs[0])
	}
	if msgs[1].Role() != message.RoleAssistant || msgs[1].GetContent().Text() != "Hello human" {
		t.Errorf("unexpected message [1]: %+v", msgs[1])
	}

	// 8. List sessions
	sessions, err := manager.ListSessions(ctx)
	if err != nil {
		t.Fatalf("failed to list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
	}

	// 9. Test Loom checkpointer setup/initialization via store
	cp, err := store.NewCheckpointer()
	if err != nil {
		t.Fatalf("failed to initialize checkpointer: %v", err)
	}
	if cp == nil {
		t.Error("expected checkpointer instance to be non-nil")
	}

	// 10. Delete session
	err = manager.DeleteSession(ctx, s1.ID)
	if err != nil {
		t.Fatalf("failed to delete session: %v", err)
	}

	// Session should be gone
	_, err = manager.GetSession(ctx, s1.ID)
	if err == nil {
		t.Error("expected error getting deleted session, got nil")
	}

	// Messages should be cascade-deleted
	msgs, err = manager.GetMessages(ctx, s1.ID)
	if err != nil {
		t.Fatalf("getting messages of deleted session failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}
