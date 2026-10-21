package session

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/gateway"
	"github.com/masterkeysrd/tasksmith/internal/harness"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
)

// Config holds the mandatory runtime dependencies required to construct a [Manager].
type Config struct {
	DB        *sql.DB
	Workspace *workspace.Workspace
	Gateway   gateway.Gateway
	Publisher Publisher
	Logger    log.Interface
}

// Validate checks that all required configuration dependencies are non-nil.
func (c Config) Validate() error {
	if c.DB == nil {
		return fmt.Errorf("%w: database is required", ErrInvalidConfig)
	}
	if c.Workspace == nil {
		return fmt.Errorf("%w: workspace is required", ErrInvalidConfig)
	}
	if c.Gateway == nil {
		return fmt.Errorf("%w: gateway is required", ErrInvalidConfig)
	}
	if c.Publisher == nil {
		return fmt.Errorf("%w: publisher is required", ErrInvalidConfig)
	}
	if c.Logger == nil {
		return fmt.Errorf("%w: logger is required", ErrInvalidConfig)
	}
	return nil
}

// Manager coordinates session lifecycle, UI message history persistence,
// agent/model/prompt resolution via [workspace.Workspace] and [gateway.Gateway],
// event publishing via [Publisher], and multi-turn conversational execution
// through [harness.Harness].
type Manager struct {
	db        *sql.DB
	store     *store
	workspace *workspace.Workspace
	gateway   gateway.Gateway
	publisher Publisher
	logger    log.Interface

	mu       sync.Mutex
	trackers map[string]*FileTracker
}

// New constructs a new [Manager] and automatically runs SQLite schema migrations
// for the `sessions` and `messages` tables.
func New(cfg Config) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	st, err := newStore(context.Background(), cfg.DB)
	if err != nil {
		return nil, err
	}

	return &Manager{
		db:        cfg.DB,
		store:     st,
		workspace: cfg.Workspace,
		gateway:   cfg.Gateway,
		publisher: cfg.Publisher,
		logger:    cfg.Logger.ForComponent("session"),
		trackers:  make(map[string]*FileTracker),
	}, nil
}

// Create creates and persists a new [Session] with a fresh UUIDv7 identifier,
// resolving default `Agent` and `Model` values from the workspace when not
// explicitly provided in params.
func (m *Manager) Create(ctx context.Context, params CreateParams) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	agent := strings.TrimSpace(params.Agent)
	if agent == "" {
		agent = m.workspace.DefaultAgentName()
	}

	model := strings.TrimSpace(params.Model)
	if model != "" && !strings.Contains(model, "/") {
		if models := m.workspace.ResolveModels(workspace.RoleCompletion, model); len(models) > 0 {
			model = models[0]
		}
	} else if model == "" {
		var explicit []string
		if ag, ok := m.workspace.GetAgent(agent); ok && len(ag.Models) > 0 {
			explicit = ag.Models
		}
		if models := m.workspace.ResolveModels(workspace.RoleCompletion, explicit...); len(models) > 0 {
			model = models[0]
		} else if len(explicit) > 0 {
			model = explicit[0]
		}
	}

	now := time.Now().UTC()
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("session: generate session id: %w", err)
	}
	sess := &Session{
		ID:        id.String(),
		Title:     strings.TrimSpace(params.Title),
		Agent:     agent,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := m.store.insertSession(ctx, sess); err != nil {
		return nil, err
	}

	cp := *sess
	return &cp, nil
}

// Get retrieves a [Session] by its identifier.
// It returns [ErrEmptySessionID] if id is blank and [ErrSessionNotFound] if no
// matching session exists.
func (m *Manager) Get(ctx context.Context, id string) (*Session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrEmptySessionID
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.store.getSession(ctx, id)
}

// List returns all persisted sessions ordered by `updated_at DESC, id DESC`.
func (m *Manager) List(ctx context.Context) ([]Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.store.listSessions(ctx)
}

// Delete removes the specified session, all of its persisted UI messages, and
// its on-disk `<WorkspaceDir>/sessions/<id>` directory (including `changes/`).
// It returns [ErrEmptySessionID] if id is blank and [ErrSessionNotFound] if the
// session did not exist.
func (m *Manager) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrEmptySessionID
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.store.deleteSession(ctx, id); err != nil {
		return err
	}

	m.mu.Lock()
	delete(m.trackers, id)
	m.mu.Unlock()

	if sessionDir, err := m.sessionDir(id); err == nil {
		_ = os.RemoveAll(sessionDir)
	}
	return nil
}

// Tracker returns the persistent per-session [*FileTracker] rooted at
// `<WorkspaceDir>/sessions/<sessionID>/changes`, replaying any existing
// `changes/*.jsonl` journals from disk.
func (m *Manager) Tracker(ctx context.Context, sessionID string) (*FileTracker, error) {
	sess, err := m.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if tr, ok := m.trackers[sess.ID]; ok {
		return tr, nil
	}

	sessionDir, err := m.sessionDir(sess.ID)
	if err != nil {
		return nil, err
	}
	changesDir := filepath.Join(sessionDir, "changes")

	tr, err := newFileTracker(changesDir)
	if err != nil {
		return nil, err
	}
	m.trackers[sess.ID] = tr
	return tr, nil
}

func (m *Manager) sessionDir(sessionID string) (string, error) {
	wsDir, err := xdg.WorkspaceDir(m.workspace.RootDir())
	if err != nil {
		return "", fmt.Errorf("session: resolve workspace data dir: %w", err)
	}
	return filepath.Join(wsDir, "sessions", filepath.Base(sessionID)), nil
}

// History returns the ordered UI message history (`id ASC`) for sessionID from
// the `messages` table.
func (m *Manager) History(ctx context.Context, sessionID string) (message.MessageList, error) {
	sess, err := m.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return m.store.listMessages(ctx, sess.ID)
}

// AppendMessage appends a message directly to the session's UI thread (`messages`
// table), updates the session's `updated_at` timestamp, and publishes an
// [EventMessageAdded] notification without modifying the underlying
// [harness.Harness] graph checkpoint.
func (m *Manager) AppendMessage(ctx context.Context, sessionID string, msg message.Message) error {
	sess, err := m.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	if msg == nil {
		return ErrNilMessage
	}

	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("session: generate message id: %w", err)
	}
	cloned := message.CloneMessage(msg)
	cloned.SetID(id.String())

	if err := m.store.insertMessage(ctx, sess.ID, cloned, time.Now().UTC()); err != nil {
		return err
	}

	m.publisher.Publish(ctx, Event{
		Type:      EventMessageAdded,
		SessionID: sess.ID,
		Message:   message.CloneMessage(cloned),
	})

	return nil
}

// Send records the user input message in the session's UI history, renders the
// session's system prompt and resolves its LLM via the gateway, executes a
// conversational turn via [harness.Harness.Stream], publishes real-time [Event]
// notifications via [Publisher], persists the completed assistant reply, and
// returns it.
func (m *Manager) Send(ctx context.Context, sessionID string, input message.Message) (*message.Assistant, error) {
	sess, err := m.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if input == nil {
		return nil, ErrNilMessage
	}

	systemPrompt := m.renderSystemPrompt(sess.Agent)

	llmModel, err := m.gateway.ResolveLLM(ctx, sess.Model)
	if err != nil {
		return nil, fmt.Errorf("session: resolve llm model %q: %w", sess.Model, err)
	}

	h, err := harness.New(harness.Config{
		DB:           m.db,
		Model:        llmModel,
		SystemPrompt: systemPrompt,
		Logger:       m.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("session: initialize harness: %w", err)
	}

	msgID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("session: generate message id: %w", err)
	}
	in := message.CloneMessage(input)
	in.SetID(msgID.String())

	if err := m.store.insertMessage(ctx, sess.ID, in, time.Now().UTC()); err != nil {
		return nil, err
	}

	m.publisher.Publish(ctx, Event{
		Type:      EventMessageAdded,
		SessionID: sess.ID,
		Message:   message.CloneMessage(in),
	})

	var reply *message.Assistant
	for hev, err := range h.Stream(ctx, sess.ID, in) {
		if err != nil {
			return nil, err
		}
		switch hev.Type {
		case harness.EventChunk:
			if hev.Chunk != nil {
				chunkCopy := message.CloneAssistantChunk(*hev.Chunk)
				m.publisher.Publish(ctx, Event{
					Type:      EventChunk,
					SessionID: sess.ID,
					Chunk:     &chunkCopy,
				})
			}
		case harness.EventCompleted:
			if hev.Message != nil {
				reply = hev.Message
			}
		}
	}
	if reply == nil {
		return nil, fmt.Errorf("session: harness returned nil assistant reply for session %q", sess.ID)
	}

	if err := m.store.insertMessage(ctx, sess.ID, reply, time.Now().UTC()); err != nil {
		return nil, err
	}

	m.publisher.Publish(ctx, Event{
		Type:      EventMessageAdded,
		SessionID: sess.ID,
		Message:   message.CloneMessage(reply),
	})
	m.publisher.Publish(ctx, Event{
		Type:      EventCompleted,
		SessionID: sess.ID,
		Message:   message.CloneMessage(reply),
	})

	out, _ := message.CloneMessage(reply).(*message.Assistant)
	return out, nil
}

func (m *Manager) renderSystemPrompt(agentName string) string {
	wsPrompt, _ := m.workspace.RenderWorkspace(nil)

	if agentName == "" {
		agentName = m.workspace.DefaultAgentName()
	}

	globals := map[string]any{
		"HasTool": func(toolName string) bool {
			if ag, ok := m.workspace.GetAgent(agentName); ok {
				for _, t := range ag.Tools {
					if t == toolName {
						return true
					}
				}
			}
			return false
		},
	}

	agentPrompt, _ := m.workspace.RenderAgent(agentName, nil, globals)

	var parts []string
	if s := strings.TrimSpace(wsPrompt); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(agentPrompt); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n\n")
}
