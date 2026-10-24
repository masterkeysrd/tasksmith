package session

import (
	"context"
	"database/sql"
	"errors"
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
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/harness"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// Config holds runtime dependencies required to construct a [Manager].
type Config struct {
	DB        *sql.DB
	Workspace *workspace.Workspace
	Gateway   gateway.Gateway
	Sandbox   sandbox.Sandbox
	Publisher Publisher
	Logger    log.Interface
	Timeline  timeline.Store
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
	if c.Timeline == nil {
		return fmt.Errorf("%w: timeline is required", ErrInvalidConfig)
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
	timeline  timeline.Store
	workspace *workspace.Workspace
	gateway   gateway.Gateway
	sandbox   sandbox.Sandbox
	publisher Publisher
	logger    log.Interface

	bgCtx    context.Context
	bgCancel context.CancelFunc
	wg       sync.WaitGroup

	mu       sync.Mutex
	createMu sync.Mutex
	sessions map[string]*Session
}

// New constructs a new [Manager] and automatically runs SQLite schema migrations
// for the sessions and timeline tables.
func New(cfg Config) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	st, err := newStore(context.Background(), cfg.DB)
	if err != nil {
		return nil, err
	}

	bgCtx, bgCancel := context.WithCancel(context.Background())

	return &Manager{
		db:        cfg.DB,
		store:     st,
		timeline:  cfg.Timeline,
		workspace: cfg.Workspace,
		gateway:   cfg.Gateway,
		sandbox:   cfg.Sandbox,
		publisher: cfg.Publisher,
		logger:    cfg.Logger.ForComponent("session"),
		bgCtx:     bgCtx,
		bgCancel:  bgCancel,
		sessions:  make(map[string]*Session),
	}, nil
}

// Close terminates in-flight background turns and waits for goroutines to exit.
func (m *Manager) Close() error {
	m.bgCancel()
	m.wg.Wait()
	return nil
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

func (m *Manager) loadOrCacheSession(ctx context.Context, dbSess *Session) (*Session, error) {
	if dbSess == nil {
		return nil, ErrSessionNotFound
	}

	m.mu.Lock()
	if existing, ok := m.sessions[dbSess.ID]; ok {
		m.mu.Unlock()
		existing.Lock()
		existing.Title = dbSess.Title
		existing.Agent = dbSess.Agent
		existing.Model = dbSess.Model
		existing.CreatedAt = dbSess.CreatedAt
		existing.UpdatedAt = dbSess.UpdatedAt
		existing.Unlock()
		return existing.Clone(), nil
	}

	root, err := m.sessionDir(dbSess.ID)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if dbSess.State == nil {
		dbSess.State = NewState()
	}
	dbSess.Root = root
	m.sessions[dbSess.ID] = dbSess
	m.mu.Unlock()

	// Check if session has unresolved permissions in checkpoint state
	if h, err := m.harness(ctx, dbSess); err == nil {
		if st, err := h.LoadState(ctx, dbSess.ID); err == nil && len(st.PendingPermissions) > 0 {
			dbSess.Lock()
			if dbSess.Status == StatusIdle {
				dbSess.Status = StatusWaitingPermission
				dbSess.WaitingPermission = true
			}
			dbSess.Unlock()
		}
	}

	return dbSess.Clone(), nil
}

func (m *Manager) getLiveSession(ctx context.Context, id string) (*Session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrEmptySessionID
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	if sess, ok := m.sessions[id]; ok {
		m.mu.Unlock()
		return sess, nil
	}
	m.mu.Unlock()

	dbSess, err := m.store.getSession(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, err := m.loadOrCacheSession(ctx, dbSess); err != nil {
		return nil, err
	}

	m.mu.Lock()
	sess := m.sessions[id]
	m.mu.Unlock()
	if sess == nil {
		return nil, ErrSessionNotFound
	}
	return sess, nil
}

// Create creates and persists a new [Session], resolving default `Agent` and
// `Model` values from the workspace when not explicitly provided in params. The
// identifier is a fresh UUIDv7 unless params.ID is set, in which case Create
// behaves like [Manager.Ensure] and discards whether the session was new.
func (m *Manager) Create(ctx context.Context, params CreateParams) (*Session, error) {
	sess, _, err := m.Ensure(ctx, params)
	return sess, err
}

// Ensure creates the session described by params, or returns it when params.ID
// names a session that already exists. created reports whether a new session
// was persisted.
//
// A caller-supplied ID must be a canonical lowercase UUIDv7 ([ErrInvalidSessionID]
// otherwise); this also keeps the on-disk session directory name safe. For an
// existing ID, every non-empty param must match the stored session, otherwise
// Ensure returns [ErrSessionConflict]. Empty params match anything, so a retry
// that omits optional fields is still accepted.
func (m *Manager) Ensure(ctx context.Context, params CreateParams) (sess *Session, created bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	id := strings.TrimSpace(params.ID)
	if id != "" {
		if err := validateSessionID(id); err != nil {
			return nil, false, err
		}
	}

	// Serialize creation by ID so two concurrent requests cannot both insert.
	if id != "" {
		m.createMu.Lock()
		defer m.createMu.Unlock()

		existing, err := m.store.getSession(ctx, id)
		switch {
		case err == nil:
			if err := m.matchesParams(existing, params); err != nil {
				return nil, false, err
			}
			cloned, err := m.loadOrCacheSession(ctx, existing)
			if err != nil {
				return nil, false, err
			}
			return cloned, false, nil
		case !errors.Is(err, ErrSessionNotFound):
			return nil, false, err
		}
	}

	agent, model := m.resolveAgentAndModel(params.Agent, params.Model)

	now := time.Now().UTC()
	if id == "" {
		generated, err := uuid.NewV7()
		if err != nil {
			return nil, false, fmt.Errorf("session: generate session id: %w", err)
		}
		id = generated.String()
	}
	created = true
	dir, err := m.sessionDir(id)
	if err != nil {
		return nil, false, err
	}

	newSession := &Session{
		State:     NewState(),
		ID:        id,
		Title:     strings.TrimSpace(params.Title),
		Agent:     agent,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
		Root:      dir,
	}

	if err := m.store.insertSession(ctx, newSession); err != nil {
		return nil, false, err
	}

	m.mu.Lock()
	m.sessions[id] = newSession
	m.mu.Unlock()

	return newSession.Clone(), created, nil
}

// validateSessionID requires a canonical lowercase UUIDv7.
func validateSessionID(id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 7 || parsed.String() != id {
		return fmt.Errorf("%w: %q", ErrInvalidSessionID, id)
	}
	return nil
}

// matchesParams reports [ErrSessionConflict] when a non-empty param differs
// from the stored session. Agent and model are compared after the same
// resolution Create applies, so aliases match their resolved names.
func (m *Manager) matchesParams(existing *Session, params CreateParams) error {
	if title := strings.TrimSpace(params.Title); title != "" && title != existing.Title {
		return fmt.Errorf("%w: title differs", ErrSessionConflict)
	}
	agent, model := m.resolveAgentAndModel(params.Agent, params.Model)
	if strings.TrimSpace(params.Agent) != "" && agent != existing.Agent {
		return fmt.Errorf("%w: agent differs", ErrSessionConflict)
	}
	if strings.TrimSpace(params.Model) != "" && model != existing.Model {
		return fmt.Errorf("%w: model differs", ErrSessionConflict)
	}
	return nil
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

	sess, err := m.getLiveSession(ctx, id)
	if err != nil {
		return nil, err
	}

	sess.Lock()
	oldAgent := sess.Agent
	oldModel := sess.Model

	newTitle := sess.Title
	if title := strings.TrimSpace(params.Title); title != "" {
		newTitle = title
	}
	newAgent := sess.Agent
	if agent := strings.TrimSpace(params.Agent); agent != "" {
		newAgent = agent
	}
	newModel := sess.Model
	if model := strings.TrimSpace(params.Model); model != "" {
		newModel = m.resolveModelName(model)
	} else if strings.TrimSpace(params.Agent) != "" && sess.Model == "" {
		_, newModel = m.resolveAgentAndModel(newAgent, "")
	}

	updatedAt := time.Now().UTC()

	dbSess := &Session{
		ID:        sess.ID,
		Title:     newTitle,
		Agent:     newAgent,
		Model:     newModel,
		CreatedAt: sess.CreatedAt,
		UpdatedAt: updatedAt,
	}

	if err := m.store.updateSession(ctx, dbSess); err != nil {
		sess.Unlock()
		return nil, err
	}

	sess.Title = newTitle
	sess.Agent = newAgent
	sess.Model = newModel
	sess.UpdatedAt = updatedAt
	if oldAgent != sess.Agent || oldModel != sess.Model {
		sess.Harness = nil
	}
	sess.Unlock()

	if oldAgent != newAgent {
		entry := timeline.NewAgentEntry(sess.ID, timeline.AgentChange{From: oldAgent, To: newAgent})
		if err := m.timeline.Append(ctx, entry); err != nil {
			return nil, err
		}
		m.publisher.Publish(api.SessionStreamTopic(sess.ID), EntryAddedEvent{
			SessionID: sess.ID,
			Entry:     entry.Clone(),
		})
	}
	if oldModel != newModel {
		entry := timeline.NewModelEntry(sess.ID, timeline.ModelChange{From: oldModel, To: newModel})
		if err := m.timeline.Append(ctx, entry); err != nil {
			return nil, err
		}
		m.publisher.Publish(api.SessionStreamTopic(sess.ID), EntryAddedEvent{
			SessionID: sess.ID,
			Entry:     entry.Clone(),
		})
	}

	return sess.Clone(), nil
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
	dbSess, err := m.store.getSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return m.loadOrCacheSession(ctx, dbSess)
}

// List returns all persisted sessions ordered by `updated_at DESC, id DESC`.
func (m *Manager) List(ctx context.Context) ([]Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	list, err := m.store.listSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Session, len(list))
	for i := range list {
		cloned, err := m.loadOrCacheSession(ctx, &list[i])
		if err != nil {
			return nil, err
		}
		out[i] = *cloned
	}
	return out, nil
}

// Delete removes the specified session, all of its persisted timeline entries, and
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

	_ = m.Cancel(ctx, id)

	if err := m.store.deleteSession(ctx, id); err != nil {
		return err
	}
	if err := m.timeline.DeleteSession(ctx, id); err != nil {
		return err
	}

	m.mu.Lock()
	sess := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()

	sessionDir, err := m.sessionDir(id)
	if err == nil {
		_ = os.RemoveAll(sessionDir)
	} else if sess != nil && sess.Root != "" {
		_ = os.RemoveAll(sess.Root)
	}
	return nil
}

// Tracker returns the persistent per-session [*FileTracker] rooted at
// `<WorkspaceDir>/sessions/<sessionID>/changes`, replaying any existing
// `changes/*.jsonl` journals from disk.
func (m *Manager) Tracker(ctx context.Context, sessionID string) (*FileTracker, error) {
	sess, err := m.getLiveSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	sess.Lock()
	defer sess.Unlock()

	if sess.Tracker != nil {
		return sess.Tracker, nil
	}

	if sess.Root == "" {
		dir, err := m.sessionDir(sess.ID)
		if err != nil {
			return nil, err
		}
		sess.Root = dir
	}

	changesDir := filepath.Join(sess.Root, "changes")
	tr, err := newFileTracker(changesDir)
	if err != nil {
		return nil, err
	}
	sess.Tracker = tr
	return tr, nil
}

// Tools returns the cached per-session [*tools.Container], constructing it on
// first access with the session's [*FileTracker], sandbox, workspace, and
// shellguard.
func (m *Manager) Tools(ctx context.Context, sessionID string) (*tools.Container, error) {
	sess, err := m.getLiveSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	tr, err := m.Tracker(ctx, sess.ID)
	if err != nil {
		return nil, err
	}

	sess.Lock()
	defer sess.Unlock()

	if sess.Tools != nil {
		return sess.Tools, nil
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

	sess.Tools = container
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

	sess, err := m.getLiveSession(ctx, sessionID)
	if err != nil {
		return err
	}

	c, err := m.Tools(ctx, sess.ID)
	if err != nil {
		return err
	}
	prevMode := c.Mode()
	c.SetMode(mode)
	if prevMode != mode {
		entry := timeline.NewModeEntry(sess.ID, timeline.ModeChange{From: prevMode, To: mode})
		if err := m.timeline.Append(ctx, entry); err != nil {
			return err
		}
		if err := m.store.touchSession(ctx, sess.ID, entry.CreatedAt); err != nil {
			return err
		}
		sess.Lock()
		sess.UpdatedAt = entry.CreatedAt
		sess.Unlock()
		m.publisher.Publish(api.SessionStreamTopic(sess.ID), EntryAddedEvent{
			SessionID: sess.ID,
			Entry:     entry.Clone(),
		})
	}
	return nil
}

func (m *Manager) sessionDir(sessionID string) (string, error) {
	wsDir, err := xdg.WorkspaceDir(m.workspace.RootDir())
	if err != nil {
		return "", fmt.Errorf("session: resolve workspace data dir: %w", err)
	}
	return filepath.Join(wsDir, "sessions", filepath.Base(sessionID)), nil
}

// Timeline returns the ordered timeline entries for sessionID from the
// session timeline. It returns [ErrEmptySessionID] if sessionID is blank and
// [ErrSessionNotFound] if the session does not exist.
func (m *Manager) Timeline(ctx context.Context, sessionID string) ([]timeline.Entry, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, ErrEmptySessionID
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sess, err := m.store.getSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return m.timeline.List(ctx, sess.ID)
}

// History returns the ordered UI message history (`id ASC`) for sessionID from
// the session timeline.
func (m *Manager) History(ctx context.Context, sessionID string) (message.MessageList, error) {
	sess, err := m.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return m.timeline.Messages(ctx, sess.ID)
}

// AppendMessage appends a message directly to the session's timeline, updates
// the session's updated_at timestamp, and publishes an [EventEntryAdded]
// notification without modifying the underlying [harness.Harness] graph
// checkpoint. It is used for system-emitted UI history messages.
func (m *Manager) AppendMessage(ctx context.Context, sessionID string, msg message.Message) error {
	sess, err := m.getLiveSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if msg == nil {
		return ErrNilMessage
	}

	entry, err := m.timeline.AppendMessage(ctx, sess.ID, msg)
	if err != nil {
		return err
	}

	if err := m.store.touchSession(ctx, sess.ID, entry.CreatedAt); err != nil {
		return err
	}
	sess.Lock()
	sess.UpdatedAt = entry.CreatedAt
	sess.Unlock()

	m.publisher.Publish(api.SessionStreamTopic(sess.ID), EntryAddedEvent{
		SessionID: sess.ID,
		Entry:     entry.Clone(),
	})

	return nil
}

func (m *Manager) appendMessage(ctx context.Context, sessionID string, msg message.Message) error {
	return m.AppendMessage(ctx, sessionID, msg)
}

// PendingPermissions returns all unresolved [tools.PermissionRequest] items from
// the session's checkpointed harness state that do not yet have a buffered
// decision in the session manager.
func (m *Manager) PendingPermissions(ctx context.Context, sessionID string) ([]tools.PermissionRequest, error) {
	sess, err := m.getLiveSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	h, err := m.harness(ctx, sess)
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

	sess.Lock()
	decMap := sess.decisions
	var out []tools.PermissionRequest
	for _, req := range state.PendingPermissions {
		if _, resolved := decMap[req.ID]; !resolved {
			req.GrantPatterns = slices.Clone(req.GrantPatterns)
			out = append(out, req)
		}
	}
	sess.Unlock()

	return out, nil
}

// RecordDecision validates and buffers a user's [tools.PermissionDecision] for
// a pending permission request without invoking the workflow graph. It returns
// true once all pending permission requests for the session have been resolved
// and the session is ready for [Manager.Resume].
func (m *Manager) RecordDecision(ctx context.Context, sessionID string, decision tools.PermissionDecision) (bool, error) {
	sess, err := m.getLiveSession(ctx, sessionID)
	if err != nil {
		return false, err
	}

	switch decision.Kind {
	case tools.DecisionAllowOnce, tools.DecisionAllowSession, tools.DecisionReject:
	default:
		return false, fmt.Errorf("%w: unsupported decision kind %q", ErrInvalidDecision, decision.Kind)
	}

	h, err := m.harness(ctx, sess)
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

	sess.Lock()
	if sess.decisions == nil {
		sess.decisions = make(map[string]tools.PermissionDecision, len(state.PendingPermissions))
	}
	sess.decisions[matched.ID] = decision

	allResolved := true
	for _, req := range state.PendingPermissions {
		if _, ok := sess.decisions[req.ID]; !ok {
			allResolved = false
			break
		}
	}
	sess.Unlock()

	matchedReq := *matched
	matchedReq.GrantPatterns = slices.Clone(matched.GrantPatterns)
	fullDec := decision
	entry := timeline.NewPermissionEntry(sess.ID, timeline.PermissionRecord{
		Request:  matchedReq,
		Decision: fullDec,
	})
	if err := m.timeline.Append(ctx, entry); err != nil {
		sess.Lock()
		delete(sess.decisions, matched.ID)
		sess.Unlock()
		return false, err
	}
	if err := m.store.touchSession(ctx, sess.ID, entry.CreatedAt); err != nil {
		sess.Lock()
		delete(sess.decisions, matched.ID)
		sess.Unlock()
		return false, err
	}
	sess.Lock()
	sess.UpdatedAt = entry.CreatedAt
	sess.Unlock()

	m.publisher.Publish(api.SessionStreamTopic(sess.ID), EntryAddedEvent{
		SessionID: sess.ID,
		Entry:     entry.Clone(),
	})

	return allResolved, nil
}

// Resume consumes the buffered permission decisions for sessionID and resumes
// the interrupted harness workflow graph.
func (m *Manager) Resume(ctx context.Context, sessionID string) (*message.Assistant, error) {
	sess, err := m.getLiveSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	sess.Lock()
	if len(sess.decisions) == 0 {
		sess.Unlock()
		return nil, ErrNoPendingPermissions
	}
	fullDecisions := maps.Clone(sess.decisions)
	clear(sess.decisions)
	sess.Unlock()

	h, err := m.harness(ctx, sess)
	if err != nil {
		return nil, err
	}

	return m.send(ctx, sess, h.Resume(ctx, sess.ID, fullDecisions))
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
	sess, err := m.getLiveSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if input == nil {
		return nil, ErrNilMessage
	}

	h, err := m.harness(ctx, sess)
	if err != nil {
		return nil, err
	}

	sess.Lock()
	clear(sess.decisions)
	sess.Unlock()

	return m.send(ctx, sess, h.Stream(ctx, sess.ID, input))
}

func (m *Manager) send(ctx context.Context, sess *Session, stream iter.Seq2[harness.Event, error]) (*message.Assistant, error) {
	var reply *message.Assistant
	var interrupted bool

	for hev, err := range stream {
		if err != nil {
			return nil, err
		}
		switch hev.Type {
		case harness.EventChunk:
			if hev.Chunk != nil {
				m.publisher.Publish(api.SessionStreamTopic(sess.ID), ChunkEvent{
					SessionID: sess.ID,
					Chunk:     message.CloneAssistantChunk(*hev.Chunk),
				})
			}

		case harness.EventMessage:
			if hev.Message != nil {
				entry, err := m.timeline.AppendMessage(ctx, sess.ID, hev.Message)
				if err != nil {
					return nil, err
				}
				if err := m.store.touchSession(ctx, sess.ID, entry.CreatedAt); err != nil {
					return nil, err
				}
				sess.Lock()
				sess.UpdatedAt = entry.CreatedAt
				sess.Unlock()

				m.publisher.Publish(api.SessionStreamTopic(sess.ID), EntryAddedEvent{
					SessionID: sess.ID,
					Entry:     entry.Clone(),
				})
			}

		case harness.EventInterrupted:
			interrupted = true
			perms := make([]tools.PermissionRequest, len(hev.Permissions))
			for i, p := range hev.Permissions {
				p.GrantPatterns = slices.Clone(p.GrantPatterns)
				perms[i] = p
			}

			sess.Lock()
			sess.Status = StatusWaitingPermission
			sess.Running = false
			sess.WaitingPermission = true
			sess.FinishedAt = time.Now().UTC()
			sess.Unlock()

			m.publisher.Publish(api.SessionStreamTopic(sess.ID), PermissionRequestedEvent{
				SessionID:   sess.ID,
				Permissions: perms,
			})

		case harness.EventCompleted:
			if hev.Message != nil {
				if asst, ok := message.CloneMessage(hev.Message).(*message.Assistant); ok {
					reply = asst
				}
			}

			sess.Lock()
			sess.Status = StatusIdle
			sess.Running = false
			sess.WaitingPermission = false
			sess.FinishedAt = time.Now().UTC()
			sess.Unlock()

			m.publisher.Publish(api.SessionStreamTopic(sess.ID), CompletedEvent{
				SessionID: sess.ID,
			})
		}
	}

	if interrupted {
		return nil, nil
	}
	if reply == nil {
		return nil, fmt.Errorf("session: harness returned nil assistant reply for session %q", sess.ID)
	}
	return reply, nil
}

// StartTurn initiates an asynchronous conversational turn for sessionID on the
// manager's background worker pool. If wait is true, StartTurn blocks until the
// turn completes, pauses for permissions, or ctx is canceled.
func (m *Manager) StartTurn(ctx context.Context, sessionID string, input message.Message, wait bool) error {
	if input == nil {
		return ErrNilMessage
	}

	sess, err := m.getLiveSession(ctx, sessionID)
	if err != nil {
		return err
	}

	sess.Lock()
	if sess.Running {
		sess.Unlock()
		return ErrSessionRunning
	}

	runCtx, cancel := context.WithCancel(m.bgCtx)
	done := make(chan struct{})

	sess.Status = StatusRunning
	sess.Running = true
	sess.WaitingPermission = false
	sess.Error = ""
	sess.err = nil
	sess.StartedAt = time.Now().UTC()
	sess.cancel = cancel
	sess.done = done
	clear(sess.decisions)
	sess.Unlock()

	m.wg.Add(1)
	go func() {
		defer func() {
			sess.Lock()
			if sess.done == done {
				sess.Running = false
				if sess.Status == StatusRunning {
					sess.Status = StatusIdle
				}
				sess.cancel = nil
				sess.done = nil
			}
			sess.Unlock()
			cancel()
			close(done)
			m.wg.Done()
		}()

		if _, err := m.Send(runCtx, sess.ID, input); err != nil {
			sess.Lock()
			sess.Running = false
			sess.WaitingPermission = false
			sess.Status = StatusIdle
			sess.FinishedAt = time.Now().UTC()
			sess.err = err
			if !errors.Is(err, context.Canceled) {
				sess.Error = err.Error()
			}
			sess.Unlock()

			if !errors.Is(err, context.Canceled) {
				m.publisher.Publish(api.SessionStreamTopic(sess.ID), ErrorEvent{
					SessionID: sess.ID,
					Error:     err.Error(),
				})
			}
		}
	}()

	if !wait {
		return nil
	}
	select {
	case <-done:
		sess.Lock()
		turnErr := sess.err
		sess.Unlock()
		if turnErr != nil && !errors.Is(turnErr, context.Canceled) {
			return turnErr
		}
		return nil
	case <-ctx.Done():
		return nil
	}
}

// RespondPermissionAsync records a permission decision and resumes an interrupted
// conversational turn in the background once all pending requests are resolved.
func (m *Manager) RespondPermissionAsync(ctx context.Context, sessionID string, decision tools.PermissionDecision, wait bool) error {
	sess, err := m.getLiveSession(ctx, sessionID)
	if err != nil {
		return err
	}

	sess.Lock()
	if sess.Running {
		sess.Unlock()
		return ErrSessionRunning
	}
	sess.Unlock()

	ready, err := m.RecordDecision(ctx, sess.ID, decision)
	if err != nil || !ready {
		return err
	}

	sess.Lock()
	if sess.Running {
		sess.Unlock()
		return ErrSessionRunning
	}

	runCtx, cancel := context.WithCancel(m.bgCtx)
	done := make(chan struct{})

	sess.Status = StatusRunning
	sess.Running = true
	sess.WaitingPermission = false
	sess.Error = ""
	sess.err = nil
	sess.StartedAt = time.Now().UTC()
	sess.cancel = cancel
	sess.done = done
	sess.Unlock()

	m.wg.Add(1)
	go func() {
		defer func() {
			sess.Lock()
			if sess.done == done {
				sess.Running = false
				if sess.Status == StatusRunning {
					sess.Status = StatusIdle
				}
				sess.cancel = nil
				sess.done = nil
			}
			sess.Unlock()
			cancel()
			close(done)
			m.wg.Done()
		}()

		if _, err := m.Resume(runCtx, sess.ID); err != nil {
			sess.Lock()
			sess.Running = false
			sess.WaitingPermission = false
			sess.Status = StatusIdle
			sess.FinishedAt = time.Now().UTC()
			sess.err = err
			if !errors.Is(err, context.Canceled) {
				sess.Error = err.Error()
			}
			sess.Unlock()

			if !errors.Is(err, context.Canceled) {
				m.publisher.Publish(api.SessionStreamTopic(sess.ID), ErrorEvent{
					SessionID: sess.ID,
					Error:     err.Error(),
				})
			}
		}
	}()

	if !wait {
		return nil
	}
	select {
	case <-done:
		sess.Lock()
		turnErr := sess.err
		sess.Unlock()
		if turnErr != nil && !errors.Is(turnErr, context.Canceled) {
			return turnErr
		}
		return nil
	case <-ctx.Done():
		return nil
	}
}

// Cancel interrupts any active conversational turn executing on sessionID.
func (m *Manager) Cancel(ctx context.Context, sessionID string) error {
	sess, err := m.getLiveSession(ctx, sessionID)
	if err != nil {
		return err
	}

	sess.Lock()
	cancel := sess.cancel
	done := sess.done
	sess.Unlock()

	if done == nil {
		return nil
	}

	cancel()
	select {
	case <-done:
	case <-ctx.Done():
	}
	return nil
}

func (m *Manager) harness(ctx context.Context, sess *Session) (*harness.Harness, error) {
	reg, err := m.Tools(ctx, sess.ID)
	if err != nil {
		return nil, err
	}

	sess.Lock()
	defer sess.Unlock()

	if sess.Harness != nil {
		return sess.Harness, nil
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

	sess.Harness = h
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
				if slices.Contains(ag.Tools, toolName) {
					return true
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
