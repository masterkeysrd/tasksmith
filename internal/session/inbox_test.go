package session

import (
	"context"
	"testing"

	"github.com/masterkeysrd/loom/message"
	coredb "github.com/masterkeysrd/tasksmith/internal/core/db"
)

func TestSessionInboxPopAndPersist(t *testing.T) {
	tmpCwd := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpCwd)
	t.Setenv("TASKSMITH_APPNAME", "tasksmith-test-inbox-private")

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

	store, err := NewSQLiteStore(db, checkpointsDb)
	if err != nil {
		t.Fatalf("failed to initialize sqlite store: %v", err)
	}

	manager := NewManager(store, nil, nil)
	ctx := context.Background()

	s, err := manager.CreateSession(ctx, "inbox-pop-test")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	activeSess := &ActiveSession{
		ID:     s.ID,
		Status: StatusRunning,
	}
	manager.activeSessions[s.ID] = activeSess

	// Queue a message in memory
	userMsg := message.NewUserText("Queued message content")
	userMsg.SetID("msg_test_queued_1")
	activeSess.Inbox = append(activeSess.Inbox, userMsg)

	// Verify in-memory queue
	if len(activeSess.Inbox) != 1 {
		t.Fatalf("expected 1 message in inbox, got %d", len(activeSess.Inbox))
	}

	// Instantiate sessionInbox and pop
	inbox := &sessionInbox{
		sess: activeSess,
		m:    manager,
	}

	popped := inbox.PopMessages()
	if len(popped) != 1 {
		t.Fatalf("expected 1 popped message, got %d", len(popped))
	}

	if popped[0].GetContent().Text() != "Queued message content" {
		t.Errorf("expected popped message content 'Queued message content', got %q", popped[0].GetContent().Text())
	}

	// The queue should now be empty in-memory
	if len(activeSess.Inbox) != 0 {
		t.Errorf("expected in-memory queue to be empty, got %d", len(activeSess.Inbox))
	}

	// Verify that the message was successfully saved to the database messages table!
	dbMsgs, err := manager.GetMessages(ctx, s.ID)
	if err != nil {
		t.Fatalf("failed to get messages from database: %v", err)
	}

	if len(dbMsgs) != 1 {
		t.Fatalf("expected 1 message saved to database, got %d", len(dbMsgs))
	}

	if dbMsgs[0].GetContent().Text() != "Queued message content" {
		t.Errorf("expected database message content 'Queued message content', got %q", dbMsgs[0].GetContent().Text())
	}
}
