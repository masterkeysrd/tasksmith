package session_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/model"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/agent/resolver"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	coredb "github.com/masterkeysrd/tasksmith/internal/core/db"
	"github.com/masterkeysrd/tasksmith/internal/core/preview"
	"github.com/masterkeysrd/tasksmith/internal/session"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
)

func TestSessionManager(t *testing.T) {
	// Create a temporary directory to act as the test workspace
	tmpCwd := t.TempDir()

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
	manager := session.NewManager(session.ManagerConfig{Store: store})

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
	if gotS1.Settings.AgentName != "main" {
		t.Errorf("expected default agent 'main', got %q", gotS1.Settings.AgentName)
	}
	if gotS1.Settings.ProviderName != "ollama" {
		t.Errorf("expected default provider 'ollama', got %q", gotS1.Settings.ProviderName)
	}
	if gotS1.Settings.ModelName != "qwen3.6:35b-a3b-coding-nvfp4" {
		t.Errorf("expected default model 'qwen3.6:35b-a3b-coding-nvfp4', got %q", gotS1.Settings.ModelName)
	}

	// Test UpdateSessionConfig
	err = manager.UpdateSessionConfig(ctx, s1.ID, session.SessionConfig{
		Settings: model.SessionSettings{
			AgentName:    "research",
			ProviderName: "openai",
			ModelName:    "gpt-4o",
		},
	})
	if err != nil {
		t.Fatalf("failed to update session config: %v", err)
	}

	gotS1Updated, err := manager.GetSession(ctx, s1.ID)
	if err != nil {
		t.Fatalf("failed to get updated session: %v", err)
	}
	if gotS1Updated.Settings.AgentName != "research" {
		t.Errorf("expected agent 'research', got %q", gotS1Updated.Settings.AgentName)
	}
	if gotS1Updated.Settings.ProviderName != "openai" {
		t.Errorf("expected provider 'openai', got %q", gotS1Updated.Settings.ProviderName)
	}
	if gotS1Updated.Settings.ModelName != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %q", gotS1Updated.Settings.ModelName)
	}

	// Test Todos Persistence
	todosList, err := manager.ListTodos(ctx, s1.ID)
	if err != nil {
		t.Fatalf("failed to list todos initially: %v", err)
	}
	if len(todosList) != 0 {
		t.Errorf("expected 0 todos initially, got %d", len(todosList))
	}

	testTodos := []tools.Todo{
		{Description: "Test Task 1", Status: "pending"},
		{Description: "Test Task 2", Status: "in_progress", ActiveText: "doing tests"},
	}

	err = manager.UpdateTodos(ctx, s1.ID, testTodos)
	if err != nil {
		t.Fatalf("failed to update todos: %v", err)
	}

	todosListUpdated, err := manager.ListTodos(ctx, s1.ID)
	if err != nil {
		t.Fatalf("failed to list todos after update: %v", err)
	}
	if len(todosListUpdated) != 2 {
		t.Fatalf("expected 2 todos, got %d", len(todosListUpdated))
	}
	if todosListUpdated[0].Description != "Test Task 1" || todosListUpdated[0].Status != "pending" {
		t.Errorf("incorrect todo [0]: %+v", todosListUpdated[0])
	}
	if todosListUpdated[1].ActiveText != "doing tests" {
		t.Errorf("incorrect todo [1]: %+v", todosListUpdated[1])
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
	sessions, err := manager.ListSessions(ctx, session.ListSessionsQuery{})
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

func TestSessionBinaryRehydration(t *testing.T) {
	tmpCwd := t.TempDir()

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

	store, err := session.NewSQLiteStore(db, checkpointsDb)
	if err != nil {
		t.Fatalf("failed to initialize sqlite store: %v", err)
	}

	manager := session.NewManager(session.ManagerConfig{Store: store})
	ctx := context.Background()

	s, err := manager.CreateSession(ctx, "binary-test-session")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// 1. Create a dummy binary file in the session storage directory
	storage := session.NewLocalFileStorage(tmpCwd, s.ID)
	dummyBytes := []byte("image-data-payload-12345")

	// Save the file in the storage directory
	toolCallID := "call-bin-1"
	storagePath := "call-bin-1_logo.png"

	cachedPath, err := storage.Save(ctx, storagePath, bytes.NewReader(dummyBytes))
	if err != nil {
		t.Fatalf("failed to save cached binary file: %v", err)
	}

	// 2. Create the tool message with an ImageBlock containing raw data
	toolMsg := &message.Tool{
		ToolCallID: toolCallID,
		Name:       "view",
		Content: message.Content{
			&message.ImageBlock{
				MIMEType: "image/png",
				Data:     dummyBytes, // Pass raw bytes to in-memory block
			},
		},
		StructuredContent: map[string]any{
			"path":        "logo.png",
			"cached_path": cachedPath,
			"mime_type":   "image/png",
			"is_binary":   true,
		},
	}

	// 3. Append message (Save to SQLite DB)
	msgID, err := manager.AppendMessage(ctx, s.ID, toolMsg)
	if err != nil {
		t.Fatalf("failed to append tool message: %v", err)
	}

	// 4. Query the raw database content directly to verify the raw bytes are NOT stored
	var rawContent string
	err = db.Get(&rawContent, "SELECT content FROM messages WHERE id = ?", msgID)
	if err != nil {
		t.Fatalf("failed to query raw DB message record: %v", err)
	}

	if strings.Contains(rawContent, "image-data-payload-12345") || strings.Contains(rawContent, "aW1hZ2UtZGF0YS1wYXlsb2FkLTEyMzQ1") {
		t.Errorf("expected SQLite message content to not contain raw bytes/base64 payload, but found it: %s", rawContent)
	}

	// 5. Retrieve messages and verify re-hydration of raw bytes from disk cache
	msgs, err := manager.GetMessages(ctx, s.ID)
	if err != nil {
		t.Fatalf("failed to get messages: %v", err)
	}

	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	toolMsgResult, ok := msgs[0].(*message.Tool)
	if !ok {
		t.Fatalf("expected Tool message, got %T", msgs[0])
	}

	if len(toolMsgResult.Content) != 1 {
		t.Fatalf("expected 1 block in Content, got %d", len(toolMsgResult.Content))
	}

	imageBlock, ok := toolMsgResult.Content[0].(*message.ImageBlock)
	if !ok {
		t.Fatalf("expected ImageBlock, got %T", toolMsgResult.Content[0])
	}

	if !bytes.Equal(imageBlock.Data, dummyBytes) {
		t.Errorf("expected ImageBlock Data to be re-hydrated to %q, got %q", string(dummyBytes), string(imageBlock.Data))
	}
}

func TestSessionInboxQueue(t *testing.T) {
	tmpCwd := t.TempDir()

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

	store, err := session.NewSQLiteStore(db, checkpointsDb)
	if err != nil {
		t.Fatalf("failed to initialize sqlite store: %v", err)
	}

	manager := session.NewManager(session.ManagerConfig{Store: store})
	ctx := context.Background()

	s, err := manager.CreateSession(ctx, "inbox-queue-test")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// 1. Initially, queued messages list should be empty
	queued, err := manager.GetQueuedMessages(s.ID)
	if err != nil {
		t.Fatalf("failed to get queued messages: %v", err)
	}
	if len(queued) != 0 {
		t.Errorf("expected 0 queued messages initially, got %d", len(queued))
	}

	// 2. Call SendMessage twice immediately
	// The first call starts the run, and the second call will find it running and queue the message.
	_ = manager.SendMessage(ctx, s.ID, "Initial message to start runner", nil)
	err = manager.SendMessage(ctx, s.ID, "Queued feedback message", nil)
	if err != nil {
		t.Fatalf("failed to send queued feedback message: %v", err)
	}

	// 3. Verify that the message is in the queued messages list
	queued, err = manager.GetQueuedMessages(s.ID)
	if err != nil {
		t.Fatalf("failed to get queued messages: %v", err)
	}
	if len(queued) != 1 {
		t.Errorf("expected 1 queued message, got %d", len(queued))
	} else {
		text := queued[0].GetContent().Text()
		if text != "Queued feedback message" {
			t.Errorf("expected queued message text 'Queued feedback message', got %q", text)
		}
	}
}

func TestSendSystemNotification(t *testing.T) {
	tmpCwd := t.TempDir()

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

	store, err := session.NewSQLiteStore(db, checkpointsDb)
	if err != nil {
		t.Fatalf("failed to initialize sqlite store: %v", err)
	}

	manager := session.NewManager(session.ManagerConfig{Store: store})
	ctx := context.Background()

	s, err := manager.CreateSession(ctx, "notification-test")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	meta := map[string]any{
		"is_system_notification": true,
		"notification_type":      "task_completion",
		"task_id":                "task-123",
	}

	err = manager.SendSystemNotification(ctx, s.ID, "Wake up agent", meta)
	if err != nil {
		t.Fatalf("failed to send system notification: %v", err)
	}

	msgs, err := manager.GetMessages(ctx, s.ID)
	if err != nil {
		t.Fatalf("failed to get messages: %v", err)
	}

	if len(msgs) != 1 {
		t.Fatalf("expected 1 message in database, got %d", len(msgs))
	}

	msg := msgs[0]
	if msg.Role() != message.RoleUser {
		t.Errorf("expected role user, got %s", msg.Role())
	}

	if msg.GetContent().Text() != "Wake up agent" {
		t.Errorf("expected message text 'Wake up agent', got %q", msg.GetContent().Text())
	}

	msgMeta := msg.GetMetadata()
	if msgMeta == nil {
		t.Fatalf("expected metadata to be non-nil")
	}

	if val, ok := msgMeta["is_system_notification"].(bool); !ok || !val {
		t.Errorf("expected is_system_notification to be true, got %v", msgMeta["is_system_notification"])
	}

	if val, ok := msgMeta["notification_type"].(string); !ok || val != "task_completion" {
		t.Errorf("expected notification_type 'task_completion', got %v", msgMeta["notification_type"])
	}

	if val, ok := msgMeta["task_id"].(string); !ok || val != "task-123" {
		t.Errorf("expected task_id 'task-123', got %v", msgMeta["task_id"])
	}
}

func TestSendMessageRejectionOnPendingAuth(t *testing.T) {
	tmpCwd := t.TempDir()

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

	store, err := session.NewSQLiteStore(db, checkpointsDb)
	if err != nil {
		t.Fatalf("failed to initialize sqlite store: %v", err)
	}

	manager := session.NewManager(session.ManagerConfig{Store: store})
	ctx := context.Background()

	s, err := manager.CreateSession(ctx, "pending-auth-test")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Manually set the session to pending_auth state with pending authorizations
	manager.SetPendingAuthorizationsDebug(s.ID, []permissions.AuthorizationRequest{
		{
			ToolCallID:  "call_123",
			ToolName:    "bash",
			Description: "Run a command",
			Preview:     preview.DefaultTextPreview{Text: "ls -la"},
			GrantRequests: []permissions.PermissionGrantRequest{
				{
					ID:          "default",
					Description: "Run a command",
					Options: []permissions.PermissionOption{
						{Target: "all", Label: "Allow"},
					},
				},
			},
		},
	})

	// Attempt to send a message — should be rejected
	err = manager.SendMessage(ctx, s.ID, "Hello agent", nil)
	if err == nil {
		t.Fatal("expected error when sending message while pending auth, got nil")
	}

	expectedMsg := "cannot send message while tool authorization is pending"
	if err.Error() != expectedMsg {
		t.Errorf("expected error message %q, got %q", expectedMsg, err.Error())
	}

	// Verify the session status is still pending_auth
	status, _, _, _, pendingAuths, _, _ := manager.GetSessionState(ctx, s.ID)
	if status != session.StatusPendingAuth {
		t.Errorf("expected session status %q, got %q", session.StatusPendingAuth, status)
	}
	if len(pendingAuths) != 1 {
		t.Errorf("expected 1 pending authorization, got %d", len(pendingAuths))
	}
}

func TestSubmitAuthorizationDecisionTransitionsStatus(t *testing.T) {
	tmpCwd := t.TempDir()

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

	store, err := session.NewSQLiteStore(db, checkpointsDb)
	if err != nil {
		t.Fatalf("failed to initialize sqlite store: %v", err)
	}

	manager := session.NewManager(session.ManagerConfig{Store: store})
	ctx := context.Background()

	s, err := manager.CreateSession(ctx, "auth-transition-test")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Set the session to pending_auth state
	manager.SetPendingAuthorizationsDebug(s.ID, []permissions.AuthorizationRequest{
		{
			ToolCallID:  "call_456",
			ToolName:    "edit",
			Description: "Edit a file",
			Preview:     preview.DefaultTextPreview{Text: "@@ -1,1 +1,2 @@"},
		},
	})

	// Verify status is pending_auth
	status, _, _, _, pendingAuths, _, _ := manager.GetSessionState(ctx, s.ID)
	if status != session.StatusPendingAuth {
		t.Fatalf("expected session status %q, got %q", session.StatusPendingAuth, status)
	}
	if len(pendingAuths) != 1 {
		t.Fatalf("expected 1 pending authorization, got %d", len(pendingAuths))
	}

	// Submit an approval decision
	err = manager.SubmitAuthorizationDecision(ctx, s.ID, permissions.AuthorizationDecision{
		ToolCallID: "call_456",
		Approved:   true,
		Scope:      permissions.ScopeOnce,
	})
	if err != nil {
		t.Fatalf("failed to submit authorization decision: %v", err)
	}

	// Verify the session status transitioned to running
	status, _, _, _, _, _, _ = manager.GetSessionState(ctx, s.ID)
	if status != session.StatusRunning {
		t.Errorf("expected session status %q after submitting decision, got %q", session.StatusRunning, status)
	}

	// Verify pending authorizations are cleared
	_, _, _, _, pendingAuths, _, _ = manager.GetSessionState(ctx, s.ID)
	if len(pendingAuths) != 0 {
		t.Errorf("expected 0 pending authorizations after submitting decision, got %d", len(pendingAuths))
	}
}

func TestSubmitDecisionInvalidState(t *testing.T) {
	tmpCwd := t.TempDir()

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

	store, err := session.NewSQLiteStore(db, checkpointsDb)
	if err != nil {
		t.Fatalf("failed to initialize sqlite store: %v", err)
	}

	manager := session.NewManager(session.ManagerConfig{Store: store})
	ctx := context.Background()

	s, err := manager.CreateSession(ctx, "invalid-state-test")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Set the session to error state
	manager.SetStatusDebug(s.ID, session.StatusError)

	// Try to submit a decision — should fail
	err = manager.SubmitAuthorizationDecision(ctx, s.ID, permissions.AuthorizationDecision{
		ToolCallID: "call_789",
		Approved:   true,
		Scope:      permissions.ScopeOnce,
	})
	if err == nil {
		t.Fatal("expected error when submitting decision in error state, got nil")
	}

	expectedMsg := "session is in an invalid state for submitting decisions: error"
	if err.Error() != expectedMsg {
		t.Errorf("expected error message %q, got %q", expectedMsg, err.Error())
	}
}

func TestMessageQueueAfterDecisionSubmission(t *testing.T) {
	tmpCwd := t.TempDir()

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

	store, err := session.NewSQLiteStore(db, checkpointsDb)
	if err != nil {
		t.Fatalf("failed to initialize sqlite store: %v", err)
	}

	manager := session.NewManager(session.ManagerConfig{Store: store})
	ctx := context.Background()

	s, err := manager.CreateSession(ctx, "queue-after-decision-test")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Set the session to pending_auth state
	manager.SetPendingAuthorizationsDebug(s.ID, []permissions.AuthorizationRequest{
		{
			ToolCallID:  "call_q1",
			ToolName:    "bash",
			Description: "Test command",
		},
	})

	// Submit a decision to transition to running
	err = manager.SubmitAuthorizationDecision(ctx, s.ID, permissions.AuthorizationDecision{
		ToolCallID: "call_q1",
		Approved:   true,
		Scope:      permissions.ScopeOnce,
	})
	if err != nil {
		t.Fatalf("failed to submit authorization decision: %v", err)
	}

	// Verify status is running
	status, _, _, _, _, _, _ := manager.GetSessionState(ctx, s.ID)
	if status != session.StatusRunning {
		t.Fatalf("expected session status %q after submitting decision, got %q", session.StatusRunning, status)
	}

	// Now send a message — should be queued (since status is running)
	err = manager.SendMessage(ctx, s.ID, "Queued message after decision", nil)
	if err != nil {
		t.Fatalf("failed to send queued message: %v", err)
	}

	// Verify the message is in the queued list
	queued, err := manager.GetQueuedMessages(s.ID)
	if err != nil {
		t.Fatalf("failed to get queued messages: %v", err)
	}
	if len(queued) != 1 {
		t.Fatalf("expected 1 queued message, got %d", len(queued))
	}
	if queued[0].GetContent().Text() != "Queued message after decision" {
		t.Errorf("expected queued message text 'Queued message after decision', got %q", queued[0].GetContent().Text())
	}
}

func TestActiveToolStreamInjection(t *testing.T) {
	tmpCwd := t.TempDir()

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

	store, err := session.NewSQLiteStore(db, checkpointsDb)
	if err != nil {
		t.Fatalf("failed to initialize sqlite store: %v", err)
	}

	manager := session.NewManager(session.ManagerConfig{Store: store})
	ctx := context.Background()

	s, err := manager.CreateSession(ctx, "toolstream-test")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// 1. Manually add an assistant message containing a tool call to the database
	asstMsg := &message.Assistant{
		Content: message.Content{
			&message.TextBlock{Text: "Running a command..."},
			&message.ToolCall{ID: "call_abc", Name: "bash"},
		},
	}
	_, err = manager.AppendMessage(ctx, s.ID, asstMsg)
	if err != nil {
		t.Fatalf("failed to append assistant message: %v", err)
	}

	// 2. Set active running session state with a tool stream chunk in memory
	_ = manager.SendMessage(ctx, s.ID, "Wake up agent", nil) // starts runner state

	// Inject stream content manually into the manager's active sessions map
	manager.SetToolStreamDebug(s.ID, "call_abc", "Hello from streaming tool output!")

	// 3. Retrieve messages and assert that the temporary tool message has been injected
	msgs, err := manager.GetMessages(ctx, s.ID)
	if err != nil {
		t.Fatalf("failed to get messages: %v", err)
	}

	foundTool := false
	for _, m := range msgs {
		if m.Role() == message.RoleTool {
			tMsg, ok := m.(*message.Tool)
			if !ok {
				continue
			}
			if tMsg.ToolCallID == "call_abc" {
				foundTool = true
				if tMsg.Content.Text() != "Hello from streaming tool output!" {
					t.Errorf("expected streaming tool output 'Hello from streaming tool output!', got %q", tMsg.Content.Text())
				}
				if tMsg.GetMetadata()["status"] != "running" {
					t.Errorf("expected metadata status 'running', got %v", tMsg.GetMetadata()["status"])
				}
			}
		}
	}

	if !foundTool {
		t.Errorf("expected temporary running tool message to be injected in message list")
	}
}

func TestSendMessageFileTracking(t *testing.T) {
	tmpCwd := t.TempDir()

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

	// 3. Create a workspace
	ws := workspace.New(tmpCwd)

	// 4. Initialize Manager with the workspace
	manager := session.NewManager(session.ManagerConfig{
		Store:     store,
		Workspace: ws,
	})

	ctx := context.Background()

	// 5. Create session
	s, err := manager.CreateSession(ctx, "test-file-tracking-session")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// 6. Write a mock file to the workspace
	filePath := filepath.Join(tmpCwd, "test_file.txt")
	testContent := "hello world from tasksmith file attachment testing"
	if err := os.WriteFile(filePath, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// 7. Get FileTracker for the session
	tracker, err := manager.FileTracker(s.ID)
	if err != nil {
		t.Fatalf("failed to get session file tracker: %v", err)
	}

	// Verify initially the file is NOT known to the tracker
	known, err := tracker.IsKnown(ctx, "test_file.txt")
	if err != nil {
		t.Fatalf("IsKnown failed: %v", err)
	}
	if known {
		t.Error("expected file to be unknown initially")
	}

	// 8. Send a message that attaches this file
	err = manager.SendMessage(ctx, s.ID, "Please check the attached file.", []resolver.Reference{
		{
			Type:  resolver.TypeFile,
			Value: "test_file.txt",
		},
	})
	if err != nil {
		t.Fatalf("failed to send message with attachment: %v", err)
	}

	// 9. Now, verify the file IS known to the tracker
	known, err = tracker.IsKnown(ctx, "test_file.txt")
	if err != nil {
		t.Fatalf("IsKnown failed after attach: %v", err)
	}
	if !known {
		t.Error("expected file to be known to the tracker after being attached to user message")
	}
}

type mockSubagentGraphRunner struct {
	runFunc func(ctx context.Context, opts tools.SubagentOptions) (string, error)
}

func (m *mockSubagentGraphRunner) Run(ctx context.Context, opts tools.SubagentOptions) (string, error) {
	if m.runFunc != nil {
		return m.runFunc(ctx, opts)
	}
	return "mock output", nil
}

func TestSubmitAuthorizationDecision_SubagentRouting(t *testing.T) {
	tmpCwd := t.TempDir()

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

	store, err := session.NewSQLiteStore(db, checkpointsDb)
	if err != nil {
		t.Fatalf("failed to initialize sqlite store: %v", err)
	}

	ws := workspace.New(tmpCwd)
	manager := session.NewManager(session.ManagerConfig{
		Store:     store,
		Workspace: ws,
	})
	ctx := context.Background()

	s, err := manager.CreateSession(ctx, "subagent-auth-routing-test")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Register a mock subagent runner
	oldSubagentRunner := tools.SubagentRunner
	tools.SubagentRunner = &mockSubagentGraphRunner{
		runFunc: func(ctx context.Context, opts tools.SubagentOptions) (string, error) {
			<-ctx.Done()
			return "mock output", nil
		},
	}
	defer func() { tools.SubagentRunner = oldSubagentRunner }()

	resolverObj := resolver.New(resolver.Config{
		Cwd:       tmpCwd,
		Workspace: ws,
	})
	handlers := &tools.ToolHandlers{
		CWD:         tmpCwd,
		SessionID:   s.ID,
		TaskManager: manager.TaskManager(),
		Resolver:    resolverObj,
	}

	runner := &tools.AgentRunner{
		AgentRef: "researcher",
		Task:     "find documents",
		Mode:     "transient",
		TaskID:   "task-subagent-123",
		Handlers: handlers,
	}

	task, err := manager.TaskManager().Submit(ctx, tools.SubmitOptions{
		SessionID: s.ID,
		TaskType:  "agent",
		Name:      "subagent_task",
		Runner:    runner,
		WaitMs:    0, // Run in background immediately
	})
	if err != nil {
		t.Fatalf("failed to submit subagent task: %v", err)
	}

	// Simulate starting the runner which sets up authDecisionChan
	go func() {
		_ = runner.Start(ctx, io.Discard, io.Discard)
	}()

	// Wait briefly for Start to run and initialize authDecisionChan
	time.Sleep(50 * time.Millisecond)

	reqs := []permissions.AuthorizationRequest{
		{
			ToolCallID:  "call_subagent_tool",
			ToolName:    "write_file",
			Description: "Write test contents",
		},
	}

	// 1. Simulate the subagent bubbling up auth requests
	runner.SetPendingAuthorizations(reqs)
	manager.TaskManager().BubbleUpAuthRequest(task.ID, reqs)

	// 2. Verify parent session state is now pending_auth
	status, _, _, _, pendingAuths, _, _ := manager.GetSessionState(ctx, s.ID)
	if status != session.StatusPendingAuth {
		t.Errorf("expected parent status pending_auth, got %s", status)
	}
	if len(pendingAuths) != 1 || pendingAuths[0].ToolCallID != "call_subagent_tool" {
		t.Errorf("expected 1 pending auth for subagent, got %v", pendingAuths)
	}

	// 3. Submit decision to parent session manager
	decision := permissions.AuthorizationDecision{
		ToolCallID: "call_subagent_tool",
		Approved:   true,
	}
	err = manager.SubmitAuthorizationDecision(ctx, s.ID, decision)
	if err != nil {
		t.Fatalf("failed to submit authorization decision: %v", err)
	}

	// 4. Verify that the subagent runner received the decision on its channel
	d, err := runner.WaitForAuthDecision(ctx)
	if err != nil {
		t.Fatalf("WaitForAuthDecision failed: %v", err)
	}
	if d.ToolCallID != "call_subagent_tool" || !d.Approved {
		t.Errorf("expected approved decision for call_subagent_tool, got %v", d)
	}

	// 5. Verify parent session cleared its pending authorizations
	status2, _, _, _, pendingAuths2, _, _ := manager.GetSessionState(ctx, s.ID)
	if status2 != session.StatusRunning {
		t.Errorf("expected parent status running, got %s", status2)
	}
	if len(pendingAuths2) != 0 {
		t.Errorf("expected parent pending auths to be cleared, got %v", pendingAuths2)
	}
}

func TestForceCompaction(t *testing.T) {
	tmpCwd := t.TempDir()

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

	store, err := session.NewSQLiteStore(db, checkpointsDb)
	if err != nil {
		t.Fatalf("failed to initialize sqlite store: %v", err)
	}

	manager := session.NewManager(session.ManagerConfig{Store: store})
	ctx := context.Background()

	s, err := manager.CreateSession(ctx, "force-compaction-test")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// 1. Calling ForceCompaction on StatusIdle should transition status to StatusRunning.
	err = manager.ForceCompaction(ctx, s.ID)
	if err != nil {
		t.Fatalf("failed to force compaction: %v", err)
	}

	status, _, _, _, _, _, _ := manager.GetSessionState(ctx, s.ID)
	if status == session.StatusRunning {
		err = manager.ForceCompaction(ctx, s.ID)
		if err == nil {
			t.Error("expected error when calling ForceCompaction on a running session, got nil")
		}
	}

	// 2. Set to pending auth and assert error
	manager.SetStatusDebug(s.ID, session.StatusPendingAuth)
	err = manager.ForceCompaction(ctx, s.ID)
	if err == nil {
		t.Error("expected error when calling ForceCompaction on a pending_auth session, got nil")
	}
}

func TestSetActiveAgentAndSlashCommands(t *testing.T) {
	tmpCwd := t.TempDir()

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

	store, err := session.NewSQLiteStore(db, checkpointsDb)
	if err != nil {
		t.Fatalf("failed to initialize sqlite store: %v", err)
	}

	ws := workspace.New(tmpCwd)
	manager := session.NewManager(session.ManagerConfig{
		Store:     store,
		Workspace: ws,
	})
	ctx := context.Background()

	s, err := manager.CreateSession(ctx, "agent-test")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// 1. Initially default agent is "main"
	sess, err := manager.GetSession(ctx, s.ID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if sess.Settings.AgentName != "main" {
		t.Errorf("expected default agent 'main', got %q", sess.Settings.AgentName)
	}

	// 2. Set active agent to "custom-agent"
	err = manager.SetActiveAgent(ctx, s.ID, "custom-agent")
	if err != nil {
		t.Fatalf("failed to set active agent: %v", err)
	}

	sess, err = manager.GetSession(ctx, s.ID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if sess.Settings.AgentName != "custom-agent" {
		t.Errorf("expected agent 'custom-agent', got %q", sess.Settings.AgentName)
	}

	// 3. Send "/create-skill" — should intercept and switch to "skill-creator"
	// Without a workspace manifest, all tools are allowed by default.
	err = manager.SendMessage(ctx, s.ID, "/create-skill", nil)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	sess, err = manager.GetSession(ctx, s.ID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if sess.Settings.AgentName != "skill-creator" {
		t.Errorf("expected agent 'skill-creator' after /create-skill command, got %q", sess.Settings.AgentName)
	}

	// 4. Restore the agent using SetActiveAgent("")
	err = manager.SetActiveAgent(ctx, s.ID, "")
	if err != nil {
		t.Fatalf("failed to restore active agent: %v", err)
	}

	// 5. Create a workspace manifest that does NOT authorize set_active_agent
	wsPath := filepath.Join(tmpCwd, "WORKSPACE.md")
	manifestContent := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: test-ws
spec:
  projects: ["."]
  policies:
    tools:
      include: ["ls", "view"]
---`
	if err := os.WriteFile(wsPath, []byte(manifestContent), 0644); err != nil {
		t.Fatalf("failed to write WORKSPACE.md: %v", err)
	}

	err = ws.Load(ctx)
	if err != nil {
		t.Fatalf("failed to load workspace: %v", err)
	}

	// Sending "/create-skill" now should be rejected due to missing tool authorizations
	err = manager.SendMessage(ctx, s.ID, "/create-skill", nil)
	if err == nil {
		t.Fatal("expected SendMessage to fail due to blocked tool policy, got nil")
	}
	if !strings.Contains(err.Error(), "requires set_active_agent") {
		t.Errorf("expected error to mention 'set_active_agent', got: %v", err)
	}
}
