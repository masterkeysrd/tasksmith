package session

import (
	"context"
	"database/sql"
	"fmt"
	"iter"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/gateway"
	"github.com/masterkeysrd/tasksmith/internal/harness"
	"github.com/masterkeysrd/tasksmith/internal/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/std"
	"github.com/masterkeysrd/tasksmith/internal/tools"
	"github.com/masterkeysrd/tasksmith/internal/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
)

// Config holds the mandatory runtime dependencies required to construct a [Manager].
type Config struct {
	DB        *sql.DB
	Workspace *workspace.Workspace
	Gateway   gateway.Gateway
	Sandbox   sandbox.Sandbox
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
	if c.Sandbox == nil {
		return fmt.Errorf("%w: sandbox is required", ErrInvalidConfig)
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
// per-session tool containers and permission gates, agent/model/prompt
// resolution via [workspace.Workspace] and [gateway.Gateway], event publishing
// via [Publisher], and multi-turn conversational execution through
// [harness.Harness].
type Manager struct {
	db        *sql.DB
	store     *store
	workspace *workspace.Workspace
	gateway   gateway.Gateway
	sandbox   sandbox.Sandbox
	publisher Publisher
	logger    log.Interface

	mu         sync.Mutex
	trackers   map[string]*FileTracker
	containers map[string]*tools.Container
	decisions  map[string]map[string]tools.PermissionDecision
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
		db:         cfg.DB,
		store:      st,
		workspace:  cfg.Workspace,
		gateway:    cfg.Gateway,
		sandbox:    cfg.Sandbox,
		publisher:  cfg.Publisher,
		logger:     cfg.Logger.ForComponent("session"),
		trackers:   make(map[string]*FileTracker),
		containers: make(map[string]*tools.Container),
		decisions:  make(map[string]map[string]tools.PermissionDecision),
	}, nil
}

// resolveModelName resolves a bare model name (e.g. "sonnet") against the workspace providers.
func (m *Manager) resolveModelName(model string) string {
	model = strings.TrimSpace(model)
	if model != "" && !strings.Contains(model, "/") {
		if models := m.workspace.ResolveModels(workspace.RoleCompletion, model); len(models) > 0 {
			return models[0]
		}
	}
	return model
}

// resolveAgentAndModel determines effective agent and model names using workspace defaults.
func (m *Manager) resolveAgentAndModel(agent, model string) (string, string) {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		agent = m.workspace.DefaultAgentName()
	}

	model = strings.TrimSpace(model)
	if model != "" {
		model = m.resolveModelName(model)
	} else {
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
	return agent, model
}

// Create creates and persists a new [Session] with a fresh UUIDv7 identifier,
// resolving default `Agent` and `Model` values from the workspace when not
// explicitly provided in params.
func (m *Manager) Create(ctx context.Context, params CreateParams) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	agent, model := m.resolveAgentAndModel(params.Agent, params.Model)

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

// Update modifies fields on an existing [Session], resolving the model against the
// workspace when updated. It returns [ErrEmptySessionID] if id is blank and
// [ErrSessionNotFound] if no matching session exists.
func (m *Manager) Update(ctx context.Context, id string, params UpdateParams) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrEmptySessionID
	}

	sess, err := m.store.getSession(ctx, id)
	if err != nil {
		return nil, err
	}

	if title := strings.TrimSpace(params.Title); title != "" {
		sess.Title = title
	}
	if agent := strings.TrimSpace(params.Agent); agent != "" {
		sess.Agent = agent
	}
	if model := strings.TrimSpace(params.Model); model != "" {
		sess.Model = m.resolveModelName(model)
	} else if strings.TrimSpace(params.Agent) != "" && sess.Model == "" {
		_, sess.Model = m.resolveAgentAndModel(sess.Agent, "")
	}

	sess.UpdatedAt = time.Now().UTC()
	if err := m.store.updateSession(ctx, sess); err != nil {
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
	delete(m.containers, id)
	delete(m.decisions, id)
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

// Tools returns the cached per-session [*tools.Container], constructing it on
// first access with the session's [*FileTracker], sandbox, workspace, and
// shellguard.
func (m *Manager) Tools(ctx context.Context, sessionID string) (*tools.Container, error) {
	sess, err := m.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	tr, err := m.Tracker(ctx, sess.ID)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if c, ok := m.containers[sess.ID]; ok {
		return c, nil
	}

	stdInst, err := std.New(std.Config{
		Sandbox: m.sandbox,
		Tracker: tr,
		Logger:  m.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("session: initialize std tools: %w", err)
	}

	guardInst := shellguard.New(shellguard.Config{
		WorkspaceDir: m.sandbox.Platform().WorkDir,
	})

	builtinTools, err := builtin.New(builtin.Config{
		Std:     stdInst,
		Sandbox: m.sandbox,
		Guard:   guardInst,
	})
	if err != nil {
		return nil, fmt.Errorf("session: initialize builtin tools: %w", err)
	}

	container, err := tools.NewContainer(tools.Config{
		Tools:     builtinTools,
		Sandbox:   m.sandbox,
		Workspace: m.workspace,
		Logger:    m.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("session: initialize tools container: %w", err)
	}

	m.containers[sess.ID] = container
	return container, nil
}

// PermissionMode returns the active [tools.PermissionMode] configured on the
// session's tool gate.
func (m *Manager) PermissionMode(ctx context.Context, sessionID string) (tools.PermissionMode, error) {
	c, err := m.Tools(ctx, sessionID)
	if err != nil {
		return "", err
	}
	return c.Gate().Mode(), nil
}

// SetPermissionMode updates the active [tools.PermissionMode] configured on the
// session's tool gate.
func (m *Manager) SetPermissionMode(ctx context.Context, sessionID string, mode tools.PermissionMode) error {
	switch mode {
	case tools.ModeDefault, tools.ModeAcceptEdits, tools.ModePlan, tools.ModeAsk, tools.ModeAuto:
	default:
		return fmt.Errorf("%w: unsupported permission mode %q", ErrInvalidConfig, mode)
	}

	c, err := m.Tools(ctx, sessionID)
	if err != nil {
		return err
	}
	c.Gate().SetMode(mode)
	return nil
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

// appendMessage is an internal helper that appends a message directly to the
// session's UI thread (`messages` table), updates the session's `updated_at`
// timestamp, and publishes an [EventMessageAdded] notification without
// modifying the underlying [harness.Harness] graph checkpoint. It is used for
// system-emitted UI history messages.
func (m *Manager) appendMessage(ctx context.Context, sessionID string, msg message.Message) error {
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

// PendingPermissions returns all unresolved [tools.PermissionRequest] items from
// the session's checkpointed harness state that do not yet have a buffered
// decision in the session manager.
func (m *Manager) PendingPermissions(ctx context.Context, sessionID string) ([]tools.PermissionRequest, error) {
	sess, err := m.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	h, err := m.buildHarness(ctx, sess)
	if err != nil {
		return nil, err
	}

	state, err := h.LoadState(ctx, sess.ID)
	if err != nil {
		return nil, err
	}
	if len(state.PendingPermissions) == 0 {
		return nil, nil
	}

	m.mu.Lock()
	decMap := m.decisions[sess.ID]
	var out []tools.PermissionRequest
	for _, req := range state.PendingPermissions {
		if _, resolved := decMap[req.ID]; !resolved {
			req.GrantPatterns = slices.Clone(req.GrantPatterns)
			out = append(out, req)
		}
	}
	m.mu.Unlock()

	return out, nil
}

// RecordDecision validates and buffers a user's [tools.PermissionDecision] for
// a pending permission request without invoking the workflow graph. It returns
// true once all pending permission requests for the session have been resolved
// and the session is ready for [Manager.Resume].
func (m *Manager) RecordDecision(ctx context.Context, sessionID string, decision tools.PermissionDecision) (bool, error) {
	sess, err := m.Get(ctx, sessionID)
	if err != nil {
		return false, err
	}

	switch decision.Kind {
	case tools.DecisionAllowOnce, tools.DecisionAllowSession, tools.DecisionReject:
	default:
		return false, fmt.Errorf("%w: unsupported decision kind %q", ErrInvalidDecision, decision.Kind)
	}

	h, err := m.buildHarness(ctx, sess)
	if err != nil {
		return false, err
	}

	state, err := h.LoadState(ctx, sess.ID)
	if err != nil {
		return false, err
	}
	if len(state.PendingPermissions) == 0 {
		return false, ErrNoPendingPermissions
	}

	var matched *tools.PermissionRequest
	for i := range state.PendingPermissions {
		req := &state.PendingPermissions[i]
		if (decision.RequestID != "" && req.ID == decision.RequestID) ||
			(decision.ToolCallID != "" && req.ToolCallID == decision.ToolCallID) {
			matched = req
			break
		}
	}
	if matched == nil {
		return false, ErrPermissionNotFound
	}

	decision.RequestID = matched.ID
	decision.ToolCallID = matched.ToolCallID

	m.mu.Lock()
	decMap := m.decisions[sess.ID]
	if decMap == nil {
		decMap = make(map[string]tools.PermissionDecision, len(state.PendingPermissions))
		m.decisions[sess.ID] = decMap
	}
	decMap[matched.ID] = decision

	allResolved := true
	for _, req := range state.PendingPermissions {
		if _, ok := decMap[req.ID]; !ok {
			allResolved = false
			break
		}
	}
	m.mu.Unlock()

	return allResolved, nil
}

// Resume consumes the buffered permission decisions for sessionID and resumes
// the interrupted harness workflow graph.
func (m *Manager) Resume(ctx context.Context, sessionID string) (*message.Assistant, error) {
	sess, err := m.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	decMap := m.decisions[sess.ID]
	if len(decMap) == 0 {
		m.mu.Unlock()
		return nil, ErrNoPendingPermissions
	}
	fullDecisions := maps.Clone(decMap)
	delete(m.decisions, sess.ID)
	m.mu.Unlock()

	h, err := m.buildHarness(ctx, sess)
	if err != nil {
		return nil, err
	}

	return m.consumeHarnessStream(ctx, sess.ID, h.Resume(ctx, sess.ID, fullDecisions))
}

// RespondPermission records a user's [tools.PermissionDecision] for a pending
// permission request. If additional permission requests remain unresolved for
// the session, RespondPermission buffers the decision and returns `(nil, nil)`
// without invoking the workflow graph. Once all pending permission requests for
// the session have been resolved, RespondPermission resumes the harness graph
// with the complete decision set and streams the remaining turn.
func (m *Manager) RespondPermission(ctx context.Context, sessionID string, decision tools.PermissionDecision) (*message.Assistant, error) {
	ready, err := m.RecordDecision(ctx, sessionID, decision)
	if err != nil || !ready {
		return nil, err
	}
	return m.Resume(ctx, sessionID)
}

// Send executes a conversational turn via [harness.Harness.Stream], persisting
// all emitted messages (including any canceled tool calls, the incoming user
// message, intermediate tool-calling assistant turns, tool execution results,
// and the final assistant reply) in the session's UI history and publishing
// real-time [Event] notifications via [Publisher]. If execution pauses awaiting
// tool permissions, Send publishes [EventPermissionRequested] and returns
// `(nil, nil)`.
func (m *Manager) Send(ctx context.Context, sessionID string, input message.Message) (*message.Assistant, error) {
	sess, err := m.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if input == nil {
		return nil, ErrNilMessage
	}

	h, err := m.buildHarness(ctx, sess)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	delete(m.decisions, sess.ID)
	m.mu.Unlock()

	return m.consumeHarnessStream(ctx, sess.ID, h.Stream(ctx, sess.ID, input))
}

func (m *Manager) consumeHarnessStream(ctx context.Context, sessID string, stream iter.Seq2[harness.Event, error]) (*message.Assistant, error) {
	var reply *message.Assistant
	var interrupted bool

	for hev, err := range stream {
		if err != nil {
			return nil, err
		}
		switch hev.Type {
		case harness.EventChunk:
			if hev.Chunk != nil {
				chunkCopy := message.CloneAssistantChunk(*hev.Chunk)
				m.publisher.Publish(ctx, Event{
					Type:      EventChunk,
					SessionID: sessID,
					Chunk:     &chunkCopy,
				})
			}

		case harness.EventMessage:
			if hev.Message != nil {
				if err := m.store.insertMessage(ctx, sessID, hev.Message, time.Now().UTC()); err != nil {
					return nil, err
				}
				m.publisher.Publish(ctx, Event{
					Type:      EventMessageAdded,
					SessionID: sessID,
					Message:   message.CloneMessage(hev.Message),
				})
			}

		case harness.EventInterrupted:
			interrupted = true
			perms := make([]tools.PermissionRequest, len(hev.Permissions))
			for i, p := range hev.Permissions {
				p.GrantPatterns = slices.Clone(p.GrantPatterns)
				perms[i] = p
			}
			m.publisher.Publish(ctx, Event{
				Type:        EventPermissionRequested,
				SessionID:   sessID,
				Permissions: perms,
			})

		case harness.EventCompleted:
			if hev.Message != nil {
				if asst, ok := message.CloneMessage(hev.Message).(*message.Assistant); ok {
					reply = asst
				}
				m.publisher.Publish(ctx, Event{
					Type:      EventCompleted,
					SessionID: sessID,
					Message:   message.CloneMessage(hev.Message),
				})
			}
		}
	}

	if interrupted {
		return nil, nil
	}
	if reply == nil {
		return nil, fmt.Errorf("session: harness returned nil assistant reply for session %q", sessID)
	}
	return reply, nil
}

func (m *Manager) buildHarness(ctx context.Context, sess *Session) (*harness.Harness, error) {
	reg, err := m.Tools(ctx, sess.ID)
	if err != nil {
		return nil, err
	}

	systemPrompt := m.renderSystemPrompt(sess.Agent)

	llmModel, err := m.gateway.ResolveLLM(ctx, sess.Model)
	if err != nil {
		return nil, fmt.Errorf("session: resolve llm model %q: %w", sess.Model, err)
	}

	h, err := harness.New(harness.Config{
		DB:           m.db,
		Model:        llmModel,
		Tools:        reg,
		SystemPrompt: systemPrompt,
		Logger:       m.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("session: initialize harness: %w", err)
	}
	return h, nil
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
