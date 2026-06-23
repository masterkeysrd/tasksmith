package session_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
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
	if gotS1.AgentName != "main" {
		t.Errorf("expected default agent 'main', got %q", gotS1.AgentName)
	}
	if gotS1.ProviderName != "ollama" {
		t.Errorf("expected default provider 'ollama', got %q", gotS1.ProviderName)
	}
	if gotS1.ModelName != "qwen3.6:35b-a3b-coding-nvfp4" {
		t.Errorf("expected default model 'qwen3.6:35b-a3b-coding-nvfp4', got %q", gotS1.ModelName)
	}

	// Test UpdateSessionConfig
	err = manager.UpdateSessionConfig(ctx, s1.ID, session.SessionConfig{
		AgentName:    "research",
		ProviderName: "openai",
		ModelName:    "gpt-4o",
	})
	if err != nil {
		t.Fatalf("failed to update session config: %v", err)
	}

	gotS1Updated, err := manager.GetSession(ctx, s1.ID)
	if err != nil {
		t.Fatalf("failed to get updated session: %v", err)
	}
	if gotS1Updated.AgentName != "research" {
		t.Errorf("expected agent 'research', got %q", gotS1Updated.AgentName)
	}
	if gotS1Updated.ProviderName != "openai" {
		t.Errorf("expected provider 'openai', got %q", gotS1Updated.ProviderName)
	}
	if gotS1Updated.ModelName != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %q", gotS1Updated.ModelName)
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

func TestSessionBinaryRehydration(t *testing.T) {
	tmpCwd := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpCwd)
	t.Setenv("TASKSMITH_APPNAME", "tasksmith-test-binary")

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
	t.Setenv("XDG_DATA_HOME", tmpCwd)
	t.Setenv("TASKSMITH_APPNAME", "tasksmith-test-inbox")

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
	_ = manager.SendMessage(ctx, s.ID, "Initial message to start runner")
	err = manager.SendMessage(ctx, s.ID, "Queued feedback message")
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
	t.Setenv("XDG_DATA_HOME", tmpCwd)
	t.Setenv("TASKSMITH_APPNAME", "tasksmith-test-notification")

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

func TestActiveToolStreamInjection(t *testing.T) {
	tmpCwd := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpCwd)
	t.Setenv("TASKSMITH_APPNAME", "tasksmith-test-toolstream")

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
	_ = manager.SendMessage(ctx, s.ID, "Wake up agent") // starts runner state

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
