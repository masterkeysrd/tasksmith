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
	ID                string
	Status            SessionStatus
	Error             string
	CurrentStreamText string
	Cancel            context.CancelFunc
}

// Manager coordinates session business logic and delegates persistence to the Store interface.
type Manager struct {
	store Store

	mu             sync.RWMutex
	activeSessions map[string]*ActiveSession
}

// NewManager creates a new Manager backed by the provided Store.
func NewManager(store Store) *Manager {
	return &Manager{
		store:          store,
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
		m.mu.Unlock()
		return fmt.Errorf("session is already running an agent execution")
	}

	sess.Status = StatusRunning
	sess.Error = ""
	sess.CurrentStreamText = ""

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
		ollamaCaller := func(c context.Context, msgs []message.Message) (*message.Assistant, error) {
			provider, err := loomollama.NewDefaultProvider()
			if err != nil {
				return nil, fmt.Errorf("failed to create default provider: %w", err)
			}
			model, err := llm.NewModel(provider, "qwen3.6:35b-a3b-coding-nvfp4", nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create model: %w", err)
			}
			return model.Invoke(c, msgs)
		}

		// Compile graph
		ag := agentgraph.New(ollamaCaller)
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

		// Consume the stream, appending text chunks dynamically in memory
		for ev, err := range seq {
			if err != nil {
				m.setSessionError(sessionID, fmt.Errorf("stream execution error: %w", err))
				return
			}

			if ev.Event == graph.EventLLMChunk {
				var chunk string
				switch d := ev.Data.(type) {
				case message.AssistantChunk:
					chunk = message.Content(d.Content).Text()
				case *message.AssistantChunk:
					chunk = message.Content(d.Content).Text()
				case string:
					chunk = d
				}

				m.mu.Lock()
				sess.CurrentStreamText += chunk
				m.mu.Unlock()
			}
		}

		// Persist the finalized assistant message to the database
		m.mu.RLock()
		finalText := sess.CurrentStreamText
		m.mu.RUnlock()

		asstMsg := message.NewAssistantText(finalText)
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

	// Serialize the message using Loom's serialization structure (as a single-element MessageList)
	list := message.MessageList{msg}
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

	// If there is an active running agent session, inject the in-progress stream text in-memory
	m.mu.RLock()
	sess, ok := m.activeSessions[sessionID]
	var streamText string
	var status SessionStatus
	if ok {
		streamText = sess.CurrentStreamText
		status = sess.Status
	}
	m.mu.RUnlock()

	if ok && status == StatusRunning && streamText != "" {
		asst := message.NewAssistantText(streamText)
		meta := map[string]any{
			"created_at": time.Now().Format("15:04"),
		}
		asst.SetMetadata(meta)
		list = append(list, asst)
	}

	return list, nil
}
