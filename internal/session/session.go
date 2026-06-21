package session

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/llm"
	loomollama "github.com/masterkeysrd/loom/llm/ollama"
	"github.com/masterkeysrd/loom/message"
	agentgraph "github.com/masterkeysrd/tasksmith/internal/agent/graph"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
)

// SessionStatus represents the runtime execution status of a session thread.
type SessionStatus string

const (
	StatusIdle    SessionStatus = "idle"
	StatusRunning SessionStatus = "running"
	StatusError   SessionStatus = "error"
)

// Session represents a domain session.
type Session struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ActiveSession holds the in-memory execution status of a running Loom agent.
type ActiveSession struct {
	ID                    string
	Status                SessionStatus
	Error                 string
	CurrentStreamText     string
	CurrentStreamThinking string
	Cancel                context.CancelFunc
	Inbox                 []message.Message
	InboxMu               sync.Mutex
}

// Manager coordinates session business logic and delegates persistence to the Store interface.
type Manager struct {
	store Store
	ws    *workspace.Workspace

	mu             sync.RWMutex
	activeSessions map[string]*ActiveSession
}

// NewManager creates a new Manager backed by the provided Store and Workspace.
func NewManager(store Store, ws *workspace.Workspace) *Manager {
	return &Manager{
		store:          store,
		ws:             ws,
		activeSessions: make(map[string]*ActiveSession),
	}
}

// CreateSession generates IDs, timestamps, and persists a session.
func (m *Manager) CreateSession(ctx context.Context, title string) (*Session, error) {
	u, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session UUID: %w", err)
	}
	id := fmt.Sprintf("sess_%s", u.String())
	now := time.Now().UTC()

	sd := SessionData{
		ID:        id,
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := m.store.CreateSession(ctx, sd); err != nil {
		return nil, err
	}

	return &Session{
		ID:        id,
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// ListSessions lists all sessions.
func (m *Manager) ListSessions(ctx context.Context) ([]Session, error) {
	sds, err := m.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}

	sessions := make([]Session, len(sds))
	for i, sd := range sds {
		sessions[i] = Session{
			ID:        sd.ID,
			Title:     sd.Title,
			CreatedAt: sd.CreatedAt,
			UpdatedAt: sd.UpdatedAt,
		}
	}
	return sessions, nil
}

// GetSession gets a single session by ID.
func (m *Manager) GetSession(ctx context.Context, id string) (*Session, error) {
	sd, err := m.store.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Session{
		ID:        sd.ID,
		Title:     sd.Title,
		CreatedAt: sd.CreatedAt,
		UpdatedAt: sd.UpdatedAt,
	}, nil
}

// DeleteSession deletes a session.
func (m *Manager) DeleteSession(ctx context.Context, id string) error {
	m.mu.Lock()
	if sess, ok := m.activeSessions[id]; ok {
		if sess.Cancel != nil {
			sess.Cancel()
		}
		delete(m.activeSessions, id)
	}
	m.mu.Unlock()

	return m.store.DeleteSession(ctx, id)
}

// RenameSession updates the title of a session.
func (m *Manager) RenameSession(ctx context.Context, id, title string) error {
	return m.store.RenameSession(ctx, id, title)
}

// ArchiveSession soft-deletes a session by setting its archived_at timestamp.
func (m *Manager) ArchiveSession(ctx context.Context, id string) error {
	m.mu.Lock()
	if sess, ok := m.activeSessions[id]; ok {
		if sess.Cancel != nil {
			sess.Cancel()
		}
		delete(m.activeSessions, id)
	}
	m.mu.Unlock()

	return m.store.ArchiveSession(ctx, id)
}

// GetSessionState returns the in-memory runtime execution state of the specified session.
func (m *Manager) GetSessionState(sessionID string) (SessionStatus, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sess, ok := m.activeSessions[sessionID]
	if !ok {
		return StatusIdle, ""
	}
	return sess.Status, sess.Error
}

// SendMessage appends the user message and initiates the background Loom agent execution.
func (m *Manager) SendMessage(ctx context.Context, sessionID string, text string) error {
	m.mu.Lock()
	sess, exists := m.activeSessions[sessionID]
	if !exists {
		sess = &ActiveSession{
			ID:     sessionID,
			Status: StatusIdle,
		}
		m.activeSessions[sessionID] = sess
	}

	if sess.Status == StatusRunning {
		userMsg := message.NewUserText(text)
		u, err := uuid.NewV7()
		if err != nil {
			m.mu.Unlock()
			return fmt.Errorf("failed to generate message UUID: %w", err)
		}
		userMsg.SetID(fmt.Sprintf("msg_%s", u.String()))

		sess.InboxMu.Lock()
		sess.Inbox = append(sess.Inbox, userMsg)
		sess.InboxMu.Unlock()
		m.mu.Unlock()
		return nil
	}

	sess.Status = StatusRunning
	sess.Error = ""
	sess.CurrentStreamText = ""
	sess.CurrentStreamThinking = ""

	runCtx, cancel := context.WithCancel(context.Background())
	sess.Cancel = cancel
	m.mu.Unlock()

	// 1. Append user message to database
	userMsg := message.NewUserText(text)
	if _, err := m.AppendMessage(ctx, sessionID, userMsg); err != nil {
		m.setSessionError(sessionID, err)
		cancel()
		return err
	}

	// 2. Start running Loom agent workflow asynchronously in background
	go func() {
		defer func() {
			m.mu.Lock()
			if sess.Status == StatusRunning {
				sess.Status = StatusIdle
			}
			m.mu.Unlock()
			cancel()
		}()

		// Load checkpointer
		cp, err := m.store.NewCheckpointer()
		if err != nil {
			m.setSessionError(sessionID, fmt.Errorf("failed to load checkpointer: %w", err))
			return
		}

		// Setup Ollama LLM provider & model
		provider, err := loomollama.NewDefaultProvider()
		if err != nil {
			m.setSessionError(sessionID, fmt.Errorf("failed to create default provider: %w", err))
			return
		}
		model, err := llm.NewModel(provider, "qwen3.6:35b-a3b-coding-nvfp4", nil)
		if err != nil {
			m.setSessionError(sessionID, fmt.Errorf("failed to create model: %w", err))
			return
		}

		// Compile graph
		storage := NewLocalFileStorage(m.ws.CWD(), sessionID)
		ag, err := agentgraph.New(runCtx, agentgraph.NewLoomModel(model), m.ws, storage, &sessionInbox{sess: sess, m: m})
		if err != nil {
			m.setSessionError(sessionID, fmt.Errorf("failed to construct agent graph: %w", err))
			return
		}
		g, err := ag.Build(cp)
		if err != nil {
			m.setSessionError(sessionID, fmt.Errorf("failed to build agent graph: %w", err))
			return
		}

		// Setup input command to load current state and append new user message
		inputCmd := graph.Update[agentgraph.AgentState](func(state agentgraph.AgentState) agentgraph.AgentState {
			state.Messages = append(state.Messages, userMsg)
			return state
		})

		// Run the graph streaming loop
		loc := &graph.Location{ThreadID: sessionID}
		seq, err := g.Stream(runCtx, inputCmd, loc)
		if err != nil {
			m.setSessionError(sessionID, fmt.Errorf("failed to start agent stream: %w", err))
			return
		}

		var asstMsg message.Message
		hasTypedChunks := false

		// Consume the stream, appending text chunks dynamically in memory
		for ev, err := range seq {
			if err != nil {
				m.setSessionError(sessionID, fmt.Errorf("stream execution error: %w", err))
				return
			}

			if ev.Event == graph.EventLLMChunk {
				var textChunk string
				var thinkingChunk string
				switch d := ev.Data.(type) {
				case message.AssistantChunk:
					hasTypedChunks = true
					textChunk = message.Content(d.Content).Text()
					thinkingChunk = message.Content(d.Content).Thought()
				case *message.AssistantChunk:
					hasTypedChunks = true
					textChunk = message.Content(d.Content).Text()
					thinkingChunk = message.Content(d.Content).Thought()
				case string:
					if !hasTypedChunks {
						textChunk = d
					}
				}

				if textChunk != "" || thinkingChunk != "" {
					m.mu.Lock()
					sess.CurrentStreamText += textChunk
					sess.CurrentStreamThinking += thinkingChunk
					m.mu.Unlock()
				}
			} else if ev.Event == "agent_message" {
				if agentMsg, ok := ev.Data.(message.Message); ok {
					if _, err := m.AppendMessage(context.Background(), sessionID, agentMsg); err != nil {
						m.setSessionError(sessionID, fmt.Errorf("failed to save agent message: %w", err))
						return
					}
					m.mu.Lock()
					sess.CurrentStreamText = ""
					sess.CurrentStreamThinking = ""
					m.mu.Unlock()
				}
			} else if ev.Event == "user_message" {
				m.mu.Lock()
				sess.CurrentStreamText = ""
				sess.CurrentStreamThinking = ""
				m.mu.Unlock()
			} else if ev.Event == "tool_message" {
				if toolMsg, ok := ev.Data.(message.Message); ok {
					if _, err := m.AppendMessage(context.Background(), sessionID, toolMsg); err != nil {
						m.setSessionError(sessionID, fmt.Errorf("failed to save tool message: %w", err))
						return
					}
				}
			} else if ev.Event == graph.EventCompleted {
				var finalState *agentgraph.AgentState
				if snap, ok := ev.Data.(graph.Snapshot[agentgraph.AgentState]); ok {
					finalState = &snap.State
				} else if snapPtr, ok := ev.Data.(*graph.Snapshot[agentgraph.AgentState]); ok {
					if snapPtr != nil {
						finalState = &snapPtr.State
					}
				}
				if finalState != nil && len(finalState.Messages) > 0 {
					lastMsg := finalState.Messages[len(finalState.Messages)-1]
					if lastMsg.Role() == message.RoleAssistant {
						asstMsg = lastMsg
					}
				}
			}
		}

		// Persist the finalized assistant message to the database
		if asstMsg == nil {
			if snap, err := g.Load(context.Background(), *loc); err == nil && len(snap.State.Messages) > 0 {
				lastMsg := snap.State.Messages[len(snap.State.Messages)-1]
				if lastMsg.Role() == message.RoleAssistant {
					asstMsg = lastMsg
				}
			}
		}

		if asstMsg == nil {
			m.mu.RLock()
			finalText := sess.CurrentStreamText
			finalThinking := sess.CurrentStreamThinking
			m.mu.RUnlock()
			var content message.Content
			if finalThinking != "" {
				content = append(content, &message.ThinkingBlock{Thinking: finalThinking})
			}
			if finalText != "" {
				content = append(content, &message.TextBlock{Text: finalText})
			}
			asstMsg = &message.Assistant{
				Content: content,
			}
		}

		// Clear the active stream state before persisting to database, so there is no duplication window
		m.mu.Lock()
		sess.Status = StatusIdle
		sess.CurrentStreamText = ""
		sess.CurrentStreamThinking = ""
		m.mu.Unlock()

		if _, err := m.AppendMessage(context.Background(), sessionID, asstMsg); err != nil {
			m.setSessionError(sessionID, fmt.Errorf("failed to save final assistant message: %w", err))
			return
		}
	}()

	return nil
}

func (m *Manager) setSessionError(sessionID string, err error) {
	m.mu.Lock()
	if sess, ok := m.activeSessions[sessionID]; ok {
		sess.Status = StatusError
		sess.Error = err.Error()
	}
	m.mu.Unlock()
}

// AppendMessage serializes a Loom message, computes ID, and writes it to the store.
func (m *Manager) AppendMessage(ctx context.Context, sessionID string, msg message.Message) (string, error) {
	msgID := msg.GetID()
	if msgID == "" {
		u, err := uuid.NewV7()
		if err != nil {
			return "", fmt.Errorf("failed to generate message UUID: %w", err)
		}
		msgID = fmt.Sprintf("msg_%s", u.String())
		msg.SetID(msgID)
	}

	var msgToSave message.Message = msg
	if tMsg, ok := msg.(*message.Tool); ok && tMsg.Name == "view" {
		var clonedContent message.Content
		for _, b := range tMsg.Content {
			switch block := b.(type) {
			case *message.ImageBlock:
				clonedContent = append(clonedContent, &message.ImageBlock{
					MIMEType: block.MIMEType,
					URL:      block.URL,
					Extras:   block.Extras,
					// Data set to nil to avoid saving raw bytes in SQLite
				})
			case *message.DocumentBlock:
				clonedContent = append(clonedContent, &message.DocumentBlock{
					MIMEType: block.MIMEType,
					URL:      block.URL,
					Extras:   block.Extras,
					// Data set to nil to avoid saving raw bytes in SQLite
				})
			default:
				clonedContent = append(clonedContent, b)
			}
		}

		msgToSave = &message.Tool{
			Base:              tMsg.Base,
			ToolCallID:        tMsg.ToolCallID,
			Name:              tMsg.Name,
			Content:           clonedContent,
			IsError:           tMsg.IsError,
			StructuredContent: tMsg.StructuredContent,
		}
	}

	if tMsg, ok := msgToSave.(*message.Tool); ok && tMsg.StructuredContent != nil {
		if overrider, ok := tMsg.StructuredContent.(interface{ OverrideForHistory() any }); ok {
			msgToSave = &message.Tool{
				Base:              tMsg.Base,
				ToolCallID:        tMsg.ToolCallID,
				Name:              tMsg.Name,
				Content:           tMsg.Content,
				IsError:           tMsg.IsError,
				StructuredContent: overrider.OverrideForHistory(),
			}
		}
	}

	// Serialize the message using Loom's serialization structure (as a single-element MessageList)
	list := message.MessageList{msgToSave}
	data, err := json.Marshal(list)
	if err != nil {
		return "", fmt.Errorf("failed to marshal message: %w", err)
	}

	// Strip outer brackets [ and ] to get the single object format
	if len(data) >= 2 && data[0] == '[' && data[len(data)-1] == ']' {
		data = data[1 : len(data)-1]
	}

	now := time.Now().UTC()
	md := MessageData{
		ID:        msgID,
		SessionID: sessionID,
		Role:      string(msg.Role()),
		Content:   string(data),
		CreatedAt: now,
	}

	if err := m.store.AppendMessage(ctx, md, now); err != nil {
		return "", err
	}

	return msgID, nil
}

// GetMessages retrieves all Loom messages for a session.
func (m *Manager) GetMessages(ctx context.Context, sessionID string) (message.MessageList, error) {
	mds, err := m.store.GetMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// Assemble all message JSON objects back into a JSON array, then use Loom's UnmarshalJSON
	var buf []byte
	buf = append(buf, '[')
	for i, md := range mds {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, md.Content...)
	}
	buf = append(buf, ']')

	var list message.MessageList
	if err := json.Unmarshal(buf, &list); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message list: %w", err)
	}

	// Re-hydrate raw binary block data from cached file for tool messages
	for i, m := range list {
		list[i] = tools.RehydrateMessage(m)
	}

	for i, md := range mds {
		if i < len(list) {
			meta := list[i].GetMetadata()
			if meta == nil {
				meta = make(map[string]any)
			}
			meta["created_at"] = md.CreatedAt.Format("15:04")
			list[i].SetMetadata(meta)
		}
	}

	// If there is an active running agent session, inject the in-progress stream text and thinking in-memory
	m.mu.RLock()
	sess, ok := m.activeSessions[sessionID]
	var streamText string
	var streamThinking string
	var status SessionStatus
	if ok {
		streamText = sess.CurrentStreamText
		streamThinking = sess.CurrentStreamThinking
		status = sess.Status
	}
	m.mu.RUnlock()

	if ok && status == StatusRunning && (streamText != "" || streamThinking != "") {
		var content message.Content
		if streamThinking != "" {
			content = append(content, &message.ThinkingBlock{Thinking: streamThinking})
		}
		if streamText != "" {
			content = append(content, &message.TextBlock{Text: streamText})
		}
		asst := &message.Assistant{
			Content: content,
		}
		meta := map[string]any{
			"created_at": time.Now().Format("15:04"),
		}
		asst.SetMetadata(meta)
		list = append(list, asst)
	}

	return list, nil
}

// GetQueuedMessages returns a copy of the in-memory queued messages for a session.
func (m *Manager) GetQueuedMessages(sessionID string) (message.MessageList, error) {
	m.mu.RLock()
	sess, ok := m.activeSessions[sessionID]
	var inboxMsgs []message.Message
	if ok {
		sess.InboxMu.Lock()
		if len(sess.Inbox) > 0 {
			inboxMsgs = make([]message.Message, len(sess.Inbox))
			copy(inboxMsgs, sess.Inbox)
		}
		sess.InboxMu.Unlock()
	}
	m.mu.RUnlock()
	return inboxMsgs, nil
}

type sessionInbox struct {
	sess *ActiveSession
	m    *Manager
}

func (si *sessionInbox) PopMessages() []message.Message {
	if si.sess == nil {
		return nil
	}
	si.sess.InboxMu.Lock()
	defer si.sess.InboxMu.Unlock()
	msgs := si.sess.Inbox
	if len(msgs) == 0 {
		return nil
	}
	si.sess.Inbox = nil

	// Save these messages to the database conversation history now that they are being processed
	ctx := context.Background()
	for _, msg := range msgs {
		// Regenerate message ID using a fresh UUID v7 to ensure it is sorted chronologically
		// after the messages that were generated while this message was queued.
		u, err := uuid.NewV7()
		if err == nil {
			msg.SetID(fmt.Sprintf("msg_%s", u.String()))
		}
		if _, err := si.m.AppendMessage(ctx, si.sess.ID, msg); err != nil {
			log.Error("Failed to save popped inbox message to database", log.Err(err))
		}
	}

	return msgs
}
