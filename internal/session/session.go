package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/llm"
	loomanthropic "github.com/masterkeysrd/loom/llm/anthropic"
	loomgenai "github.com/masterkeysrd/loom/llm/genai"
	loomollama "github.com/masterkeysrd/loom/llm/ollama"
	loomopenai "github.com/masterkeysrd/loom/llm/openai"
	"github.com/masterkeysrd/loom/message"
	agentgraph "github.com/masterkeysrd/tasksmith/internal/agent/graph"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/agent/prompt"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	coredb "github.com/masterkeysrd/tasksmith/internal/core/db"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/mcp"
	"github.com/masterkeysrd/tasksmith/internal/metrics"
	"github.com/masterkeysrd/tasksmith/internal/session/filetrack"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
	"github.com/masterkeysrd/warp"
	"path/filepath"
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
	ID              string          `json:"id"`
	Title           string          `json:"title"`
	AgentName       string          `json:"agent_name"`
	ProviderName    string          `json:"provider_name"`
	ModelName       string          `json:"model_name"`
	LastTurnMetrics *SessionMetrics `json:"last_turn_metrics,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// ActiveSession holds the in-memory execution status of a running Loom agent.
type ActiveSession struct {
	ID                    string
	Status                SessionStatus
	Error                 string
	CurrentStreamText     string
	CurrentStreamThinking string
	CurrentToolStreams    map[string]string // toolCallID -> accumulated stream text
	Cancel                context.CancelFunc
	Inbox                 []message.Message
	InboxMu               sync.Mutex
	ThinkingStart         time.Time
	ThinkingDuration      time.Duration
	CurrentStreamMetrics  *message.TokenMetrics
	PendingAuthorizations []permissions.AuthorizationRequest
	MessageSubscribers    []chan struct{}
}

// ManagerConfig defines configuration parameters and dependencies for Manager.
type ManagerConfig struct {
	Store        Store
	Workspace    *workspace.Workspace
	MetricsStore *metrics.Store
	LspManager   *lsp.Manager
	Context      context.Context
}

// Manager coordinates session business logic and delegates persistence to the Store interface.
type Manager struct {
	store Store
	ws    *workspace.Workspace

	mu             sync.RWMutex
	activeSessions map[string]*ActiveSession
	taskMgr        *tools.TaskManager
	metricsStore   *metrics.Store
	lspManager     *lsp.Manager
	mcpManager     *mcp.Manager
	wg             sync.WaitGroup
	ctx            context.Context
	cancel         context.CancelFunc
}

// FileTracker returns a session-scoped FileTracker instance.
func (m *Manager) FileTracker(sessionID string) (filetrack.FileTracker, error) {
	if m.ws == nil {
		return nil, fmt.Errorf("workspace not initialized")
	}
	wsDir, err := xdg.WorkspaceDir(m.ws.CWD())
	if err != nil {
		return nil, err
	}
	changesDir := filepath.Join(wsDir, "sessions", sessionID, "changes")
	return filetrack.NewTracker(m.store.ResourceStore(), sessionID, changesDir, m.ws.CWD()), nil
}

// McpManager returns the session-scoped MCP manager.
func (m *Manager) McpManager() *mcp.Manager {
	return m.mcpManager
}

// NewManager creates a new Manager using the provided configuration.
func NewManager(cfg ManagerConfig) *Manager {
	var mcps []*warp.MCP
	if cfg.Workspace != nil {
		mcps = cfg.Workspace.MCPs()
	}
	mcpMgr := mcp.NewManager(mcps)

	m := &Manager{
		store:          cfg.Store,
		ws:             cfg.Workspace,
		metricsStore:   cfg.MetricsStore,
		lspManager:     cfg.LspManager,
		activeSessions: make(map[string]*ActiveSession),
		mcpManager:     mcpMgr,
	}
	if cfg.Context != nil {
		m.ctx, m.cancel = context.WithCancel(cfg.Context)
	} else {
		m.ctx, m.cancel = context.WithCancel(context.Background())
	}

	var cwd string
	if cfg.Workspace != nil {
		cwd = cfg.Workspace.CWD()
	}

	m.taskMgr = tools.NewTaskManager(cwd, func(sessionID, taskID string, task *tools.Task) {
		if task.Status == tools.StatusRunning || task.Status == tools.StatusKilled {
			return // Ignore mid-run details/ports updates and killed tasks in the chat history
		}
		statusStr := string(task.Status)
		msgText := fmt.Sprintf("[System: Background task %s (\"%s\") completed with status %s (exit code %d).\nYou can inspect the command output/logs by calling the 'tasks' tool with action: 'status' and taskId: '%s'.]", taskID, task.Name, statusStr, task.ExitCode, taskID)
		if task.Error != "" {
			msgText += "\nError: " + task.Error
		}

		meta := map[string]any{
			"is_system_notification": true,
			"notification_type":      "task_completion",
			"task_id":                taskID,
			"task_name":              task.Name,
			"task_status":            statusStr,
			"exit_code":              task.ExitCode,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = m.SendSystemNotification(ctx, sessionID, msgText, meta)
	})

	return m
}

// CreateSession generates IDs, timestamps, and persists a session.
func (m *Manager) CreateSession(ctx context.Context, title string) (*Session, error) {
	u, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session UUID: %w", err)
	}
	id := fmt.Sprintf("sess_%s", u.String())
	now := time.Now().UTC()

	agentName, providerName, modelName, err := m.resolveDefaults(ctx)
	if err != nil {
		return nil, err
	}

	sd := SessionData{
		ID:           id,
		Title:        title,
		AgentName:    &agentName,
		ProviderName: &providerName,
		ModelName:    &modelName,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := m.store.CreateSession(ctx, sd); err != nil {
		return nil, err
	}

	return &Session{
		ID:           id,
		Title:        title,
		AgentName:    agentName,
		ProviderName: providerName,
		ModelName:    modelName,
		CreatedAt:    now,
		UpdatedAt:    now,
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
		agentName := ""
		if sd.AgentName != nil {
			agentName = *sd.AgentName
		}
		providerName := ""
		if sd.ProviderName != nil {
			providerName = *sd.ProviderName
		}
		modelName := ""
		if sd.ModelName != nil {
			modelName = *sd.ModelName
		}

		if agentName == "" || providerName == "" || modelName == "" {
			defAgent, defProvider, defModel, err := m.resolveDefaults(ctx)
			if err == nil {
				if agentName == "" {
					agentName = defAgent
				}
				if providerName == "" {
					providerName = defProvider
				}
				if modelName == "" {
					modelName = defModel
				}
			}
		}

		var metrics *SessionMetrics
		if sd.LastTurnMetrics != nil {
			var m SessionMetrics
			if err := json.Unmarshal([]byte(*sd.LastTurnMetrics), &m); err == nil {
				metrics = &m
			}
		}

		sessions[i] = Session{
			ID:              sd.ID,
			Title:           sd.Title,
			AgentName:       agentName,
			ProviderName:    providerName,
			ModelName:       modelName,
			LastTurnMetrics: metrics,
			CreatedAt:       sd.CreatedAt,
			UpdatedAt:       sd.UpdatedAt,
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

	agentName := ""
	if sd.AgentName != nil {
		agentName = *sd.AgentName
	}
	providerName := ""
	if sd.ProviderName != nil {
		providerName = *sd.ProviderName
	}
	modelName := ""
	if sd.ModelName != nil {
		modelName = *sd.ModelName
	}

	if agentName == "" || providerName == "" || modelName == "" {
		defAgent, defProvider, defModel, err := m.resolveDefaults(ctx)
		if err == nil {
			if agentName == "" {
				agentName = defAgent
			}
			if providerName == "" {
				providerName = defProvider
			}
			if modelName == "" {
				modelName = defModel
			}
		}
	}

	return &Session{
		ID:           sd.ID,
		Title:        sd.Title,
		AgentName:    agentName,
		ProviderName: providerName,
		ModelName:    modelName,
		CreatedAt:    sd.CreatedAt,
		UpdatedAt:    sd.UpdatedAt,
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

// UpdateSessionConfig updates the agent and model configurations of a session.
func (m *Manager) UpdateSessionConfig(ctx context.Context, id string, cfg SessionConfig) error {
	return m.store.UpdateSessionConfig(ctx, id, cfg)
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
func (m *Manager) GetSessionState(ctx context.Context, sessionID string) (SessionStatus, string, bool, []permissions.AuthorizationRequest, time.Duration) {
	m.mu.Lock()
	sess, ok := m.activeSessions[sessionID]
	if !ok {
		sess = &ActiveSession{
			ID:     sessionID,
			Status: StatusIdle,
		}
		m.activeSessions[sessionID] = sess
	}
	m.mu.Unlock()

	isGenerating := len(sess.CurrentStreamText) > 0 || len(sess.CurrentStreamThinking) > 0

	m.mu.Lock()
	if sess.Status == StatusIdle && sess.PendingAuthorizations == nil {
		m.mu.Unlock() // release lock before doing checkpointer load
		if cp, err := m.store.NewCheckpointer(); err == nil {
			storage := NewLocalFileStorage(m.ws.CWD(), sessionID)
			if ag, err := agentgraph.New(ctx, agentgraph.Options{
				Workspace:   m.ws,
				Storage:     storage,
				TaskManager: m.taskMgr,
				SessionID:   sessionID,
				LspManager:  m.lspManager,
				McpManager:  m.mcpManager,
			}); err == nil {
				if g, err := ag.Build(cp); err == nil {
					loc := &graph.Location{ThreadID: sessionID}
					if snap, err := g.Load(ctx, *loc); err == nil {
						m.mu.Lock()
						sess.PendingAuthorizations = snap.State.PendingAuthorizations
						m.mu.Unlock()
					}
				}
			}
		}
	} else {
		m.mu.Unlock()
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	var elapsed time.Duration
	if !sess.ThinkingStart.IsZero() {
		elapsed = time.Since(sess.ThinkingStart)
	}
	return sess.Status, sess.Error, isGenerating, sess.PendingAuthorizations, elapsed
}

// ListTasks retrieves all tasks for a session from the task manager.
func (m *Manager) ListTasks(sessionID string) []*tools.Task {
	if m.taskMgr == nil {
		return nil
	}
	return m.taskMgr.ListTasks(sessionID)
}

// SendMessage appends the user message and initiates the background Loom agent execution.
func (m *Manager) SendMessage(ctx context.Context, sessionID string, text string) error {
	return m.sendMessage(ctx, sessionID, text, nil)
}

// SendSystemNotification appends a system notification message with metadata and starts/queues execution.
func (m *Manager) SendSystemNotification(ctx context.Context, sessionID string, text string, meta map[string]any) error {
	return m.sendMessage(ctx, sessionID, text, meta)
}

func (m *Manager) sendMessage(ctx context.Context, sessionID string, text string, meta map[string]any) error {
	m.mu.Lock()
	sess, exists := m.activeSessions[sessionID]
	if !exists {
		sess = &ActiveSession{
			ID:     sessionID,
			Status: StatusIdle,
		}
		m.activeSessions[sessionID] = sess
	}

	msg := message.NewUserText(text)
	if len(meta) > 0 {
		msg.SetMetadata(meta)
	}

	if sess.Status == StatusRunning {
		u, err := uuid.NewV7()
		if err != nil {
			m.mu.Unlock()
			return fmt.Errorf("failed to generate message UUID: %w", err)
		}
		msg.SetID(fmt.Sprintf("msg_%s", u.String()))

		sess.InboxMu.Lock()
		sess.Inbox = append(sess.Inbox, msg)
		sess.InboxMu.Unlock()
		m.mu.Unlock()
		return nil
	}

	sess.Status = StatusRunning
	sess.Error = ""
	sess.CurrentStreamText = ""
	sess.CurrentStreamThinking = ""
	sess.CurrentToolStreams = make(map[string]string)
	sess.ThinkingStart = time.Now()
	sess.ThinkingDuration = 0
	sess.CurrentStreamMetrics = nil
	sess.PendingAuthorizations = nil

	runCtx, cancel := context.WithCancel(context.Background())
	sess.Cancel = cancel
	m.mu.Unlock()

	// 1. Append message to database
	if _, err := m.AppendMessage(ctx, sessionID, msg); err != nil {
		m.setSessionError(sessionID, err)
		cancel()
		return err
	}

	// Load existing todos from database to initialize graph state if empty
	existingTodos, _ := m.ListTodos(runCtx, sessionID)
	var cwd string
	if m.ws != nil {
		if cfg, err := m.ws.GetWorkspaceConfig(runCtx); err == nil {
			cwd = cfg.CWD
		}
	}

	// Setup input command to load current state and append new message
	inputCmd := graph.Update[agentgraph.AgentState](func(state agentgraph.AgentState) agentgraph.AgentState {
		if len(state.Todos) == 0 && len(existingTodos) > 0 {
			state.Todos = existingTodos
		}

		agentgraph.InjectReminders(runCtx, msg, state, m.lspManager, cwd)

		state.Messages = append(state.Messages, msg)
		return state
	})

	// 2. Start running Loom agent workflow asynchronously in background
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.runAgentLoop(runCtx, sessionID, sess, inputCmd, cancel)
	}()

	return nil
}

// SubmitAuthorizationDecision submits a user permission decision and resumes the agent.
func (m *Manager) SubmitAuthorizationDecision(ctx context.Context, sessionID string, decisions ...permissions.AuthorizationDecision) error {
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
		return fmt.Errorf("session is currently running")
	}

	sess.Status = StatusRunning
	sess.Error = ""
	sess.CurrentStreamText = ""
	sess.CurrentStreamThinking = ""
	sess.CurrentToolStreams = make(map[string]string)
	sess.ThinkingStart = time.Now()
	sess.ThinkingDuration = 0
	sess.CurrentStreamMetrics = nil
	sess.PendingAuthorizations = nil

	runCtx, cancel := context.WithCancel(context.Background())
	sess.Cancel = cancel
	m.mu.Unlock()

	// Setup input command to load current state and inject user's decisions
	inputCmd := graph.Update[agentgraph.AgentState](func(state agentgraph.AgentState) agentgraph.AgentState {
		state.Decisions = append(state.Decisions, decisions...)
		return state
	})

	// Start running Loom agent workflow asynchronously in background
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.runAgentLoop(runCtx, sessionID, sess, inputCmd, cancel)
	}()

	return nil
}

func (m *Manager) runAgentLoop(runCtx context.Context, sessionID string, sess *ActiveSession, inputCmd graph.Command[agentgraph.AgentState], cancel context.CancelFunc) {
	defer func() {
		m.mu.Lock()
		if sess.Status == StatusRunning {
			sess.Status = StatusIdle
		}
		if !sess.ThinkingStart.IsZero() {
			sess.ThinkingDuration = time.Since(sess.ThinkingStart)
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

	// Load session config
	sessData, err := m.store.GetSession(runCtx, sessionID)
	if err != nil {
		m.setSessionError(sessionID, fmt.Errorf("failed to load session data: %w", err))
		return
	}

	agentName := ""
	if sessData.AgentName != nil {
		agentName = *sessData.AgentName
	}
	providerName := ""
	if sessData.ProviderName != nil {
		providerName = *sessData.ProviderName
	}
	modelName := ""
	if sessData.ModelName != nil {
		modelName = *sessData.ModelName
	}

	if agentName == "" || providerName == "" || modelName == "" {
		defAgent, defProvider, defModel, err := m.resolveDefaults(runCtx)
		if err == nil {
			if agentName == "" {
				agentName = defAgent
			}
			if providerName == "" {
				providerName = defProvider
			}
			if modelName == "" {
				modelName = defModel
			}
		}
	}

	// Resolve agent system prompt
	var systemPrompt string
	if m.ws != nil && agentName != "" {
		resolvedAgent, err := m.ws.ResolveAgent(runCtx, agentName)
		if err != nil {
			m.setSessionError(sessionID, fmt.Errorf("failed to resolve agent %q: %w", agentName, err))
			return
		}

		var contextInstructions string
		var contextPath string
		contexts := m.ws.Contexts()
		if len(contexts) > 0 {
			var parts []string
			for _, ctxRes := range contexts {
				if inst := ctxRes.Spec.Instructions; inst != "" {
					parts = append(parts, inst)
				}
			}
			contextInstructions = strings.Join(parts, "\n\n")
			contextPath = contexts[0].Directory
		}

		rendered, err := prompt.RenderAgent(
			resolvedAgent,
			m.ws.WorkspaceSpec(),
			m.ws.Project(),
			map[string]any{
				"Context":     contextInstructions,
				"ContextPath": contextPath,
			},
		)
		if err != nil {
			m.setSessionError(sessionID, fmt.Errorf("failed to render system prompt template: %w", err))
			return
		}
		systemPrompt = rendered
	}

	// Resolve LLM Provider
	var provider llm.Provider
	if m.ws != nil && providerName != "" {
		var matchingProvider *warp.ModelProvider
		for _, p := range m.ws.Providers() {
			if p.GetName() == providerName {
				matchingProvider = p
				break
			}
		}
		if matchingProvider != nil {
			provider, err = createLoomProvider(runCtx, matchingProvider)
			if err != nil {
				m.setSessionError(sessionID, fmt.Errorf("failed to create provider %q: %w", providerName, err))
				return
			}
		}
	}

	// If no provider resolved, fallback to ollama default provider
	if provider == nil {
		provider, err = loomollama.NewDefaultProvider()
		if err != nil {
			m.setSessionError(sessionID, fmt.Errorf("failed to create default provider: %w", err))
			return
		}
	}

	// Instantiate Model
	if modelName == "" {
		modelName = "qwen3.6:35b-a3b-coding-nvfp4" // default fallback
	}
	model, err := llm.NewModel(provider, modelName, nil)
	if err != nil {
		m.setSessionError(sessionID, fmt.Errorf("failed to create model: %w", err))
		return
	}

	// Compile graph
	storage := NewLocalFileStorage(m.ws.CWD(), sessionID)
	ft, err := m.FileTracker(sessionID)
	if err != nil {
		m.setSessionError(sessionID, fmt.Errorf("failed to initialize file tracker: %w", err))
		return
	}
	ag, err := agentgraph.New(runCtx, agentgraph.Options{
		Model:        agentgraph.NewLoomModel(model),
		Workspace:    m.ws,
		Storage:      storage,
		Inbox:        &sessionInbox{sess: sess, m: m},
		TaskManager:  m.taskMgr,
		SessionID:    sessionID,
		SystemPrompt: systemPrompt,
		AgentName:    agentName,
		OnTodosUpdated: func(ctx context.Context, todos []tools.Todo) error {
			return m.UpdateTodos(ctx, sessionID, todos)
		},
		LspManager:  m.lspManager,
		FileTracker: ft,
		McpManager:  m.mcpManager,
	})
	if err != nil {
		m.setSessionError(sessionID, fmt.Errorf("failed to construct agent graph: %w", err))
		return
	}
	g, err := ag.Build(cp)
	if err != nil {
		m.setSessionError(sessionID, fmt.Errorf("failed to build agent graph: %w", err))
		return
	}

	// Run the graph streaming loop
	loc := &graph.Location{ThreadID: sessionID}
	seq, err := g.Stream(runCtx, inputCmd, loc)
	if err != nil {
		m.setSessionError(sessionID, fmt.Errorf("failed to start agent stream: %w", err))
		return
	}

	var asstMsg message.Message

	// Consume the stream, appending text chunks dynamically in memory
	for ev, err := range seq {
		if err != nil {
			m.setSessionError(sessionID, fmt.Errorf("stream execution error: %w", err))
			return
		}

		if ev.Event == graph.EventLLMChunk {
			m.handleLLMChunk(sess, ev)
		} else if ev.Event == "agent_message" {
			if err := m.handleAgentMessage(runCtx, sessionID, sess, ev, systemPrompt, agentName, providerName, modelName); err != nil {
				m.setSessionError(sessionID, err)
				return
			}
		} else if ev.Event == "user_message" {
			m.mu.Lock()
			sess.CurrentStreamText = ""
			sess.CurrentStreamThinking = ""
			m.mu.Unlock()
			m.notifySubscribers(sessionID)
		} else if ev.Event == "on_tool_chunk" {
			m.handleToolChunk(sess, ev)
		} else if ev.Event == "tool_message" {
			if err := m.handleToolMessage(runCtx, sessionID, sess, ev, agentName); err != nil {
				m.setSessionError(sessionID, err)
				return
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

	if asstMsg != nil {
		m.mu.Lock()
		if !sess.ThinkingStart.IsZero() {
			sess.ThinkingDuration = time.Since(sess.ThinkingStart)
		}
		durationSecs := int(sess.ThinkingDuration.Seconds())
		m.mu.Unlock()

		meta := asstMsg.GetMetadata()
		if meta == nil {
			meta = make(map[string]any)
		}
		if _, exists := meta["thinking_duration"]; !exists && durationSecs > 0 {
			meta["thinking_duration"] = durationSecs
			asstMsg.SetMetadata(meta)
		}
	}

	var pendingAuths []permissions.AuthorizationRequest
	if snap, err := g.Load(context.Background(), *loc); err == nil {
		pendingAuths = snap.State.PendingAuthorizations
	}

	// Clear the active stream state before persisting to database, so there is no duplication window
	m.mu.Lock()
	sess.CurrentStreamText = ""
	sess.CurrentStreamThinking = ""
	sess.CurrentToolStreams = nil
	sess.PendingAuthorizations = pendingAuths
	m.mu.Unlock()

	if _, err := m.AppendMessage(context.Background(), sessionID, asstMsg); err != nil {
		m.setSessionError(sessionID, fmt.Errorf("failed to save final assistant message: %w", err))
		return
	}
}

func (m *Manager) setSessionError(sessionID string, err error) {
	log.Error(fmt.Sprintf("Session error [%s]: %v", sessionID, err))
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

	m.notifySubscribers(sessionID)
	return msgID, nil
}

// SubscribeMessages registers a listener channel to receive a signal whenever session messages are updated.
// It returns the channel and a cleanup function to unregister the channel.
func (m *Manager) SubscribeMessages(sessionID string) (<-chan struct{}, func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.activeSessions[sessionID]
	if !ok {
		sess = &ActiveSession{
			ID:                 sessionID,
			Status:             StatusIdle,
			CurrentToolStreams: make(map[string]string),
		}
		m.activeSessions[sessionID] = sess
	}

	ch := make(chan struct{}, 1)
	// Seed with an initial update so the client fetches initially
	ch <- struct{}{}

	sess.MessageSubscribers = append(sess.MessageSubscribers, ch)

	cleanup := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		s, ok := m.activeSessions[sessionID]
		if !ok {
			return
		}
		for i, sub := range s.MessageSubscribers {
			if sub == ch {
				s.MessageSubscribers = append(s.MessageSubscribers[:i], s.MessageSubscribers[i+1:]...)
				break
			}
		}
	}

	return ch, cleanup
}

func (m *Manager) notifySubscribers(sessionID string) {
	m.mu.RLock()
	sess, ok := m.activeSessions[sessionID]
	if !ok || len(sess.MessageSubscribers) == 0 {
		m.mu.RUnlock()
		return
	}
	chans := make([]chan struct{}, len(sess.MessageSubscribers))
	copy(chans, sess.MessageSubscribers)
	m.mu.RUnlock()

	for _, ch := range chans {
		select {
		case ch <- struct{}{}:
		default:
			// Non-blocking if channel already flagged
		}
	}
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

	// If there is an active running agent session, inject the in-progress stream text and thinking in-memory,
	// as well as any active in-progress tool stream outputs.
	m.mu.RLock()
	sess, ok := m.activeSessions[sessionID]
	var streamText string
	var streamThinking string
	var status SessionStatus
	var toolStreams map[string]string
	if ok {
		streamText = sess.CurrentStreamText
		streamThinking = sess.CurrentStreamThinking
		status = sess.Status
		if len(sess.CurrentToolStreams) > 0 {
			toolStreams = make(map[string]string)
			for k, v := range sess.CurrentToolStreams {
				toolStreams[k] = v
			}
		}
	}
	m.mu.RUnlock()

	if ok && status == StatusRunning {
		// 1. Inject active tool streams
		completedTools := make(map[string]bool)
		for _, msg := range list {
			if msg.Role() == message.RoleTool {
				if tMsg, ok := msg.(*message.Tool); ok {
					completedTools[tMsg.ToolCallID] = true
				}
			}
		}

		for _, msg := range list {
			if msg.Role() == message.RoleAssistant {
				for _, block := range msg.GetContent() {
					if tc, ok := block.(*message.ToolCall); ok {
						if !completedTools[tc.ID] {
							tStreamText := ""
							if toolStreams != nil {
								tStreamText = toolStreams[tc.ID]
							}
							tempTool := &message.Tool{
								ToolCallID: tc.ID,
								Name:       tc.Name,
								Content:    message.Content{&message.TextBlock{Text: tStreamText}},
							}
							tempTool.SetMetadata(map[string]any{
								"status":     "running",
								"created_at": time.Now().Format("15:04"),
							})
							list = append(list, tempTool)
						}
					}
				}
			}
		}

		// 2. Inject active LLM text/thinking stream
		// 2. Inject active LLM text/thinking stream
		if streamText != "" || streamThinking != "" {
			var content message.Content
			var contentLen int
			if streamThinking != "" {
				content = append(content, &message.ThinkingBlock{Thinking: streamThinking})
				contentLen += len(streamThinking)
			}
			if streamText != "" {
				content = append(content, &message.TextBlock{Text: streamText})
				contentLen += len(streamText)
			}
			asst := &message.Assistant{
				Content: content,
			}

			m.mu.RLock()
			streamMetrics := sess.CurrentStreamMetrics
			m.mu.RUnlock()

			meta := map[string]any{
				"created_at": time.Now().Format("15:04"),
			}

			if streamMetrics != nil {
				meta["prompt_tokens"] = streamMetrics.Tokens.Input
				meta["completion_tokens"] = streamMetrics.Tokens.Output
				meta["total_tokens"] = streamMetrics.TotalTokens
			}

			asst.SetMetadata(meta)
			list = append(list, asst)
		}
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
	msgs := si.sess.Inbox
	if len(msgs) == 0 {
		si.sess.InboxMu.Unlock()
		return nil
	}
	si.sess.Inbox = nil
	si.sess.InboxMu.Unlock()

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

// SetToolStreamDebug sets active tool stream content for unit testing.
func (m *Manager) SetToolStreamDebug(sessionID string, toolCallID string, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.activeSessions[sessionID]
	if ok {
		if sess.CurrentToolStreams == nil {
			sess.CurrentToolStreams = make(map[string]string)
		}
		sess.CurrentToolStreams[toolCallID] = text
		sess.Status = StatusRunning // Force running status for injection test
	}
}

func (m *Manager) resolveDefaults(ctx context.Context) (agentName, providerName, modelName string, err error) {
	if m.ws == nil {
		return "main", "ollama", "qwen3.6:35b-a3b-coding-nvfp4", nil
	}
	return m.ws.ResolveDefaults(ctx)
}

func createLoomProvider(ctx context.Context, p *warp.ModelProvider) (llm.Provider, error) {
	// Set endpoints and auth env variables dynamically
	if p.Spec.Endpoint != "" {
		switch p.Spec.Type {
		case "ollama":
			os.Setenv("OLLAMA_HOST", p.Spec.Endpoint)
		case "openai":
			os.Setenv("OPENAI_BASE_URL", p.Spec.Endpoint)
		case "anthropic":
			os.Setenv("ANTHROPIC_BASE_URL", p.Spec.Endpoint)
		}
	}
	if p.Spec.Auth != nil && p.Spec.Auth.Env != "" {
		val := os.Getenv(p.Spec.Auth.Env)
		if val != "" {
			switch p.Spec.Type {
			case "openai":
				os.Setenv("OPENAI_API_KEY", val)
			case "anthropic":
				os.Setenv("ANTHROPIC_API_KEY", val)
			case "google-genai":
				os.Setenv("GEMINI_API_KEY", val)
			}
		}
	}

	switch p.Spec.Type {
	case "ollama":
		return loomollama.NewDefaultProvider()
	case "openai":
		return loomopenai.NewDefaultProvider()
	case "anthropic":
		return loomanthropic.NewDefaultProvider()
	case "google-genai":
		return loomgenai.NewDefaultProvider(ctx)
	default:
		return nil, fmt.Errorf("unknown provider type: %s", p.Spec.Type)
	}
}

// ListTodos retrieves the todos for a session from the SQLite store.
func (m *Manager) ListTodos(ctx context.Context, sessionID string) ([]tools.Todo, error) {
	sd, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sd.Todos == nil || *sd.Todos == "" {
		return []tools.Todo{}, nil
	}
	var todos []tools.Todo
	if err := json.Unmarshal([]byte(*sd.Todos), &todos); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session todos: %w", err)
	}
	return todos, nil
}

// UpdateTodos persists the updated list of todos as JSON.
func (m *Manager) UpdateTodos(ctx context.Context, sessionID string, todos []tools.Todo) error {
	data, err := json.Marshal(todos)
	if err != nil {
		return fmt.Errorf("failed to marshal todos: %w", err)
	}
	return m.store.UpdateSessionTodos(ctx, sessionID, string(data))
}

func (m *Manager) handleLLMChunk(sess *ActiveSession, ev graph.StreamEvent) {
	var textChunk string
	var thinkingChunk string
	var chunkMetrics *message.TokenMetrics
	hasTypedChunks := false
	switch d := ev.Data.(type) {
	case message.AssistantChunk:
		hasTypedChunks = true
		textChunk = message.Content(d.Content).Text()
		thinkingChunk = message.Content(d.Content).Thought()
		chunkMetrics = d.Metrics
	case *message.AssistantChunk:
		hasTypedChunks = true
		textChunk = message.Content(d.Content).Text()
		thinkingChunk = message.Content(d.Content).Thought()
		chunkMetrics = d.Metrics
	case string:
		if !hasTypedChunks {
			textChunk = d
		}
	}

	m.mu.Lock()
	if chunkMetrics != nil {
		sess.CurrentStreamMetrics = chunkMetrics
	}
	if textChunk != "" || thinkingChunk != "" {
		if thinkingChunk != "" {
			if sess.ThinkingStart.IsZero() {
				sess.ThinkingStart = time.Now()
			}
			sess.ThinkingDuration = time.Since(sess.ThinkingStart)
		}
		sess.CurrentStreamText += textChunk
		sess.CurrentStreamThinking += thinkingChunk
	}
	m.mu.Unlock()
	m.notifySubscribers(sess.ID)
}

func (m *Manager) countToolTokens(ctx context.Context, sessionID string, agentName string) int {
	var allowedTools map[string]bool
	if m.ws != nil {
		cfg, err := m.ws.GetWorkspaceConfig(ctx)
		if err == nil {
			allowedTools = cfg.AuthorizedTools
		}
	}

	allLoomTools, err := tools.Resources()
	if err != nil {
		return 0
	}

	var activeTools []any
	for _, lt := range allLoomTools {
		if allowedTools == nil || allowedTools[lt.Metadata.Name] {
			activeTools = append(activeTools, lt)
		}
	}

	b, err := json.Marshal(activeTools)
	if err != nil {
		return 0
	}
	return len(string(b)) / 4
}

func (m *Manager) handleAgentMessage(ctx context.Context, sessionID string, sess *ActiveSession, ev graph.StreamEvent, systemPrompt, agentName, providerName, modelName string) error {
	agentMsg, ok := ev.Data.(message.Message)
	if !ok {
		return nil
	}
	if asstMsg, ok := agentMsg.(*message.Assistant); ok {
		m.mu.Lock()
		durationSecs := int(sess.ThinkingDuration.Seconds())
		m.mu.Unlock()

		meta := asstMsg.GetMetadata()
		if meta == nil {
			meta = make(map[string]any)
		}
		if _, exists := meta["thinking_duration"]; !exists && durationSecs > 0 {
			meta["thinking_duration"] = durationSecs
			asstMsg.SetMetadata(meta)
		}
	}
	if _, err := m.AppendMessage(context.Background(), sessionID, agentMsg); err != nil {
		return fmt.Errorf("failed to save agent message: %w", err)
	}

	if asstMsg, ok := agentMsg.(*message.Assistant); ok && asstMsg.Metrics != nil && m.metricsStore != nil {
		var wsPath, projName string
		if m.ws != nil {
			wsPath = m.ws.CWD()
			if p := m.ws.Project(); p != nil {
				projName = p.Name
			}
		}

		sysTokens, _ := llm.ApproximateTokenCounter{}.CountTokens(ctx, message.MessageList{message.NewSystemText(systemPrompt)})
		toolsTokens := m.countToolTokens(ctx, sessionID, agentName)

		var toolResultTokens, workspaceFileTokens, chatTokens int
		if msgs, err := m.GetMessages(ctx, sessionID); err == nil {
			for _, msg := range msgs {
				toks, _ := llm.ApproximateTokenCounter{}.CountTokens(ctx, message.MessageList{msg})
				if tr, ok := msg.(*message.Tool); ok {
					switch tr.Name {
					case "view", "ls", "grep", "glob", "read_file", "list_dir", "grep_search":
						workspaceFileTokens += toks
					default:
						toolResultTokens += toks
					}
				} else {
					chatTokens += toks
				}
			}
		}

		event := coredb.MetricsEvent{
			SessionID:     sessionID,
			WorkspacePath: wsPath,
			ProjectName:   projName,
			AgentName:     agentName,
			NodeName:      func(s string) *string { return &s }("think"),
			CreatedAt:     time.Now(),
		}
		payload := coredb.LLMCallPayload{
			Provider:            providerName,
			Model:               modelName,
			SystemTokens:        sysTokens,
			PromptTokens:        asstMsg.Metrics.Tokens.Input,
			CompletionTokens:    asstMsg.Metrics.Tokens.Output,
			TotalTokens:         asstMsg.Metrics.TotalTokens,
			CacheCreationTokens: asstMsg.Metrics.Tokens.CacheWrite,
			CacheReadTokens:     asstMsg.Metrics.Tokens.CacheRead,
			EstimatedCostUSD:    asstMsg.Metrics.TotalCost.AsUSD(),
		}
		_ = m.metricsStore.LogLLMCall(event, payload)

		cumPromptTokens := asstMsg.Metrics.Tokens.Input
		cumCompletionTokens := asstMsg.Metrics.Tokens.Output
		cumTotalTokens := asstMsg.Metrics.TotalTokens
		cumCost := asstMsg.Metrics.TotalCost.AsUSD()
		if prevSession, err := m.store.GetSession(ctx, sessionID); err == nil && prevSession != nil && prevSession.LastTurnMetrics != nil {
			var prevMetrics SessionMetrics
			if err := json.Unmarshal([]byte(*prevSession.LastTurnMetrics), &prevMetrics); err == nil {
				cumPromptTokens += prevMetrics.CumulativePromptTokens
				cumCompletionTokens += prevMetrics.CumulativeCompletionTokens
				cumTotalTokens += prevMetrics.CumulativeTotalTokens
				cumCost += prevMetrics.CumulativeCostUSD
			}
		}

		// Persist the latest metrics to the session table for quick UI lookup
		m.store.UpdateSessionMetrics(ctx, sessionID, SessionMetrics{
			SystemTokens:               sysTokens,
			ToolsTokens:                toolsTokens,
			ToolResultTokens:           toolResultTokens,
			WorkspaceFileTokens:        workspaceFileTokens,
			ChatTokens:                 chatTokens,
			PromptTokens:               asstMsg.Metrics.Tokens.Input,
			CompletionTokens:           asstMsg.Metrics.Tokens.Output,
			TotalTokens:                asstMsg.Metrics.TotalTokens,
			EstimatedCostUSD:           asstMsg.Metrics.TotalCost.AsUSD(),
			CumulativePromptTokens:     cumPromptTokens,
			CumulativeCompletionTokens: cumCompletionTokens,
			CumulativeTotalTokens:      cumTotalTokens,
			CumulativeCostUSD:          cumCost,
		})
	}

	m.mu.Lock()
	sess.CurrentStreamText = ""
	sess.CurrentStreamThinking = ""
	m.mu.Unlock()
	m.notifySubscribers(sessionID)
	return nil
}

func (m *Manager) handleToolChunk(sess *ActiveSession, ev graph.StreamEvent) {
	var chunk message.ToolChunk
	switch d := ev.Data.(type) {
	case message.ToolChunk:
		chunk = d
	case *message.ToolChunk:
		if d != nil {
			chunk = *d
		}
	}
	toolCallID := chunk.ID
	if toolCallID == "" {
		toolCallID = ev.Source
	}
	text := chunk.Content.Text()
	if text != "" {
		m.mu.Lock()
		if sess.CurrentToolStreams == nil {
			sess.CurrentToolStreams = make(map[string]string)
		}
		sess.CurrentToolStreams[toolCallID] += text
		m.mu.Unlock()
		m.notifySubscribers(sess.ID)
	}
}

func (m *Manager) handleToolMessage(ctx context.Context, sessionID string, sess *ActiveSession, ev graph.StreamEvent, agentName string) error {
	toolMsg, ok := ev.Data.(message.Message)
	if !ok {
		return nil
	}
	if _, err := m.AppendMessage(context.Background(), sessionID, toolMsg); err != nil {
		return fmt.Errorf("failed to save tool message: %w", err)
	}
	if tMsg, ok := toolMsg.(*message.Tool); ok {
		m.mu.Lock()
		if sess.CurrentToolStreams != nil {
			delete(sess.CurrentToolStreams, tMsg.ToolCallID)
		}
		m.mu.Unlock()

		if m.metricsStore != nil {
			var wsPath, projName string
			if m.ws != nil {
				wsPath = m.ws.CWD()
				if p := m.ws.Project(); p != nil {
					projName = p.Name
				}
			}

			outputTokens := 0
			for _, b := range tMsg.Content {
				if txt, ok := b.(*message.TextBlock); ok {
					outputTokens += len(txt.Text) / 4
				}
			}

			status := "success"
			var errMsg *string
			if tMsg.IsError {
				status = "error"
				if len(tMsg.Content) > 0 {
					if txt, ok := tMsg.Content[0].(*message.TextBlock); ok {
						e := txt.Text
						errMsg = &e
					}
				}
			}

			var execTime int64
			if meta := tMsg.GetMetadata(); meta != nil {
				if t, ok := meta["execution_time_ms"].(int64); ok {
					execTime = t
				}
			}

			event := coredb.MetricsEvent{
				SessionID:     sessionID,
				WorkspacePath: wsPath,
				ProjectName:   projName,
				AgentName:     agentName,
				NodeName:      func(s string) *string { return &s }("execute_tools"),
				CreatedAt:     time.Now(),
			}
			payload := coredb.ToolCallPayload{
				ToolName:        tMsg.Name,
				ExecutionTimeMs: execTime,
				Status:          status,
				ErrorMessage:    errMsg,
				OutputTokens:    outputTokens,
			}
			_ = m.metricsStore.LogToolCall(event, payload)
		}
	}
	m.notifySubscribers(sessionID)
	return nil
}

// Done returns a channel that is closed when the Manager is closed.
func (m *Manager) Done() <-chan struct{} {
	return m.ctx.Done()
}

// Close cancels all active sessions and waits for them to terminate.
func (m *Manager) Close() error {
	if m.cancel != nil {
		m.cancel()
	}

	m.mu.Lock()
	for _, sess := range m.activeSessions {
		if sess.Cancel != nil {
			sess.Cancel()
		}
	}
	m.mu.Unlock()

	m.wg.Wait()
	return nil
}
