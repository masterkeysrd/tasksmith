package session

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/harness"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// Config holds runtime dependencies required to construct a [Manager].
type Config struct {
	DB          *sql.DB
	Workspace   *workspace.Workspace
	Gateway     func() gateway.Gateway
	Publisher   Publisher
	Logger      log.Interface
	Permissions permissions.Manager
	ReloadLock  *sync.RWMutex
	Catalog     func() *models.Catalog
	// Metrics receives tool call metrics from session harnesses and reports
	// per-turn usage for turn summaries. A nil service disables both.
	Metrics metrics.Service
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
	if c.Permissions == nil {
		return fmt.Errorf("%w: permissions is required", ErrInvalidConfig)
	}
	return nil
}

// Manager coordinates session collection lifecycle.
type Manager interface {
	Create(ctx context.Context, params CreateParams) (sess *Session, created bool, err error)
	Preview(ctx context.Context, config SessionConfig) (SessionConfig, models.Model, []api.SessionDiagnostic, bool, error)
	Get(ctx context.Context, id string) (*Session, error)
	List(ctx context.Context) ([]*Session, error)
	Update(ctx context.Context, id string, params UpdateParams) (*Session, error)
	Refresh()
	Delete(ctx context.Context, id string) error
	Close() error
}

type manager struct {
	db         *sql.DB
	store      *store
	workspace  *workspace.Workspace
	gateway    func() gateway.Gateway
	publisher  Publisher
	logger     log.Interface
	perms      permissions.Manager
	reloadLock *sync.RWMutex
	catalog    func() *models.Catalog
	metrics    metrics.Service

	bgCtx    context.Context
	bgCancel context.CancelFunc
	wg       sync.WaitGroup

	mu       sync.Mutex
	createMu sync.Mutex
	sessions map[string]*Session
}

var _ Manager = (*manager)(nil)

// New constructs a new [Manager] and automatically runs SQLite schema migrations
// for the sessions table.
func New(cfg Config) (Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	st, err := newStore(context.Background(), cfg.DB)
	if err != nil {
		return nil, err
	}

	if cfg.ReloadLock == nil {
		cfg.ReloadLock = &sync.RWMutex{}
	}
	if cfg.Catalog == nil {
		catalog, err := models.Build(cfg.Workspace.Catalog(), cfg.Gateway())
		if err != nil {
			return nil, fmt.Errorf("session: build model catalog: %w", err)
		}
		cfg.Catalog = func() *models.Catalog { return catalog }
	}
	bgCtx, bgCancel := context.WithCancel(context.Background())
	return &manager{
		db:         cfg.DB,
		store:      st,
		workspace:  cfg.Workspace,
		gateway:    cfg.Gateway,
		publisher:  cfg.Publisher,
		logger:     cfg.Logger,
		perms:      cfg.Permissions,
		reloadLock: cfg.ReloadLock,
		catalog:    cfg.Catalog,
		metrics:    cfg.Metrics,
		bgCtx:      bgCtx,
		bgCancel:   bgCancel,
		sessions:   make(map[string]*Session),
	}, nil
}

func (m *manager) sessionDir(sessionID string) (string, error) {
	wsDir, err := xdg.WorkspaceDir(m.workspace.RootDir())
	if err != nil {
		return "", fmt.Errorf("session: resolve workspace data dir: %w", err)
	}
	return filepath.Join(wsDir, "sessions", filepath.Base(sessionID)), nil
}

func (m *manager) resolveAgentModel(agent string, model models.ID) (string, models.ID) {
	cat := m.workspace.Catalog()
	if cat == nil {
		return agent, model
	}
	agent = strings.TrimSpace(agent)
	if agent == "" {
		agent = cat.DefaultAgentName()
	}
	model = models.ID(strings.TrimSpace(string(model)))
	if model == "" {
		if resolvedAgent, err := cat.ResolveAgent(agent); err == nil && resolvedAgent != nil && resolvedAgent.Agent != nil && len(resolvedAgent.Agent.Spec.Models) > 0 {
			if resolved, ok := m.catalog().Resolve(models.RoleCompletion, resolvedAgent.Agent.Spec.Models...); ok {
				return agent, resolved.ID
			}
		}
	}
	var explicit []string
	if model != "" {
		explicit = []string{string(model)}
	}
	if resolved, ok := m.catalog().Resolve(models.RoleCompletion, explicit...); ok {
		model = resolved.ID
	}
	return agent, model
}

func (m *manager) validateThinking(model models.ID, setting *api.ThinkingSetting) error {
	resolved, ok := m.catalog().Get(model)
	if !ok {
		return fmt.Errorf("%w: model %q unavailable", ErrInvalidConfig, model)
	}
	_, _, err := resolved.Configure(models.Settings{Thinking: setting, StrictThinking: true})
	return err
}

func (m *manager) matchesParams(s *Session, params CreateParams) error {
	s.Lock()
	defer s.Unlock()

	wantTitle := strings.TrimSpace(params.Title)
	if wantTitle != "" && s.Title != wantTitle {
		return ErrSessionConflict
	}
	wantAgent, wantModel := m.resolveAgentModel(params.Config.Agent, params.Config.Model)
	if wantAgent != "" && s.Config.Agent != wantAgent {
		return ErrSessionConflict
	}
	if wantModel != "" && s.Config.Model != wantModel {
		return ErrSessionConflict
	}
	if params.Config.Thinking != nil && !reflect.DeepEqual(s.Config.Thinking, params.Config.Thinking) {
		return ErrSessionConflict
	}
	wantMode := params.Config.Mode
	if wantMode != "" && s.Config.Mode != wantMode {
		return ErrSessionConflict
	}
	return nil
}

func (m *manager) loadOrCacheSession(ctx context.Context, dbSess *Session) (*Session, error) {
	m.mu.Lock()
	if sess, ok := m.sessions[dbSess.ID]; ok {
		m.mu.Unlock()
		return sess, nil
	}
	m.mu.Unlock()

	if !dbSess.Config.Model.Valid() {
		if model, ok := m.catalog().Resolve(models.RoleCompletion, string(dbSess.Config.Model)); ok {
			dbSess.Config.Model = model.ID
			if err := m.store.updateSessionModel(ctx, dbSess); err != nil {
				return nil, err
			}
		}
	}
	dir, err := m.sessionDir(dbSess.ID)
	if err != nil {
		return nil, err
	}

	sess := &Session{
		State:       NewState(),
		ID:          dbSess.ID,
		Title:       dbSess.Title,
		Config:      dbSess.Config,
		CreatedAt:   dbSess.CreatedAt,
		UpdatedAt:   dbSess.UpdatedAt,
		Permissions: dbSess.Permissions.Clone(),
		permRoot:    m.perms,
		Root:        dir,
		db:          m.db,
		store:       m.store,
		workspace:   m.workspace,
		gateway:     m.gateway,
		catalog:     m.catalog,
		metrics:     m.metrics,
		reloadLock:  m.reloadLock,
		publisher:   m.publisher,
		logger:      m.logger,
		bgCtx:       m.bgCtx,
		wg:          &m.wg,
	}

	sess.Diagnostics = sess.modelDiagnostics(sess.Config)

	if err := sess.initResources(ctx); err != nil {
		return nil, err
	}

	if st, err := sess.Harness.LoadState(ctx); err == nil {
		sess.Lock()
		sess.Context = st.Context
		if len(st.Todos) > 0 {
			sess.Todos = slices.Clone(st.Todos)
		}
		if len(st.PendingPermissions) > 0 && sess.Status == StatusIdle {
			sess.Status = StatusWaitingPermission
			sess.WaitingPermission = true
			perms := make([]api.PermissionRequest, len(st.PendingPermissions))
			for i, p := range st.PendingPermissions {
				p.GrantPatterns = slices.Clone(p.GrantPatterns)
				perms[i] = p
			}
			sess.PendingPermissions = perms
		}
		if len(st.PendingQuestions) > 0 && sess.Status != StatusThinking {
			sess.WaitingQuestion = true
			sess.PendingQuestions = make([]api.QuestionRequest, len(st.PendingQuestions))
			for i := range st.PendingQuestions {
				sess.PendingQuestions[i] = *st.PendingQuestions[i].Clone()
			}
			if !sess.WaitingPermission {
				sess.Status = StatusWaitingQuestion
			}
		}
		sess.Unlock()
	}
	_ = sess.configureHarness(ctx)

	m.mu.Lock()
	if existing, ok := m.sessions[sess.ID]; ok {
		m.mu.Unlock()
		_ = sess.Close()
		return existing, nil
	}
	m.sessions[sess.ID] = sess
	m.mu.Unlock()

	return sess, nil
}

func (m *manager) getLiveSession(ctx context.Context, id string) (*Session, error) {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	m.mu.Unlock()
	if ok {
		return sess, nil
	}

	dbSess, err := m.store.getSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return m.loadOrCacheSession(ctx, dbSess)
}

func (m *manager) Create(ctx context.Context, params CreateParams) (sess *Session, created bool, err error) {
	m.reloadLock.RLock()
	defer m.reloadLock.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	id := strings.TrimSpace(params.ID)
	if id != "" && !uuid.Validate(id) {
		return nil, false, fmt.Errorf("%w: invalid session id %q", ErrInvalidSessionID, id)
	}

	resolvedConfig, err := m.resolveConfig(params.Config)
	if err != nil {
		return nil, false, err
	}

	if id != "" {
		m.createMu.Lock()
		defer m.createMu.Unlock()

		existing, err := m.getLiveSession(ctx, id)
		switch {
		case err == nil:
			if err := m.matchesParams(existing, params); err != nil {
				return nil, false, err
			}
			return existing, false, nil
		case !errors.Is(err, ErrSessionNotFound):
			return nil, false, err
		}
	}

	if id == "" {
		id = uuid.NewString()
	}

	sessDir, err := m.sessionDir(id)
	if err != nil {
		return nil, false, err
	}

	now := time.Now().UTC()
	newSession := &Session{
		State:       NewState(),
		ID:          id,
		Title:       strings.TrimSpace(params.Title),
		Config:      resolvedConfig,
		CreatedAt:   now,
		UpdatedAt:   now,
		Permissions: permissions.Permissions{},
		permRoot:    m.perms,
		Root:        sessDir,
		db:          m.db,
		store:       m.store,
		workspace:   m.workspace,
		gateway:     m.gateway,
		catalog:     m.catalog,
		metrics:     m.metrics,
		reloadLock:  m.reloadLock,
		publisher:   m.publisher,
		logger:      m.logger,
		bgCtx:       m.bgCtx,
		wg:          &m.wg,
	}

	newSession.Diagnostics = newSession.modelDiagnostics(newSession.Config)
	if err := newSession.initResources(ctx); err != nil {
		return nil, false, err
	}

	if err := m.store.insertSession(ctx, newSession); err != nil {
		_ = newSession.Close()
		return nil, false, err
	}

	m.mu.Lock()
	m.sessions[id] = newSession
	m.mu.Unlock()

	m.publisher.Publish(api.TopicSessions, api.SessionCreatedEvent{
		Workspace: m.workspace.RootDir(),
		Session:   ToAPISession(newSession),
	})

	return newSession, true, nil
}

// resolveConfig validates config and resolves its agent and model, defaulting
// the permission mode. A "default" thinking setting is normalized to nil.
func (m *manager) resolveConfig(config SessionConfig) (SessionConfig, error) {
	mode := cmp.Or(config.Mode, api.ModeDefault)
	if !mode.IsValid() {
		return SessionConfig{}, fmt.Errorf("%w: unsupported permission mode %q", ErrInvalidConfig, config.Mode)
	}

	agent, modelID := m.resolveAgentModel(config.Agent, config.Model)
	if config.Thinking != nil {
		if err := m.validateThinking(modelID, config.Thinking); err != nil {
			return SessionConfig{}, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
		}
		if config.Thinking.Type == "default" {
			config.Thinking = nil
		}
	}
	return SessionConfig{Agent: agent, Model: modelID, Thinking: config.Thinking, Mode: mode}, nil
}

func (m *manager) Preview(ctx context.Context, config SessionConfig) (SessionConfig, models.Model, []api.SessionDiagnostic, bool, error) {
	m.reloadLock.RLock()
	defer m.reloadLock.RUnlock()
	if err := ctx.Err(); err != nil {
		return SessionConfig{}, models.Model{}, nil, false, err
	}
	resolvedConfig, err := m.resolveConfig(config)
	if err != nil {
		return SessionConfig{}, models.Model{}, nil, false, err
	}
	modelID := resolvedConfig.Model

	sess := &Session{
		workspace: m.workspace,
		catalog:   m.catalog,
	}
	diagnostics := sess.modelDiagnostics(resolvedConfig)

	var resolvedModel models.Model
	var found bool
	if modelID.Valid() {
		resolvedModel, found = m.catalog().Get(modelID)
	}
	return resolvedConfig, resolvedModel, diagnostics, found, nil
}

func (m *manager) Refresh() {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, sess := range m.sessions {
		sessions = append(sessions, sess)
	}
	m.mu.Unlock()
	for _, sess := range sessions {
		sess.Lock()
		sess.Diagnostics = sess.modelDiagnostics(sess.Config)
		sess.Unlock()
		_ = sess.configureHarness(context.Background())
		m.publisher.Publish(api.TopicSessions, api.SessionUpdatedEvent{Workspace: m.workspace.RootDir(), Session: ToAPISession(sess)})
	}
}

func (m *manager) Get(ctx context.Context, id string) (*Session, error) {
	m.reloadLock.RLock()
	defer m.reloadLock.RUnlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrEmptySessionID
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.getLiveSession(ctx, id)
}

func (m *manager) List(ctx context.Context) ([]*Session, error) {
	m.reloadLock.RLock()
	defer m.reloadLock.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	list, err := m.store.listSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*Session, len(list))
	for i, dbSess := range list {
		live, err := m.getLiveSession(ctx, dbSess.ID)
		if err != nil {
			return nil, err
		}
		out[i] = live
	}
	return out, nil
}

func (m *manager) Update(ctx context.Context, id string, params UpdateParams) (*Session, error) {
	m.reloadLock.RLock()
	defer m.reloadLock.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrEmptySessionID
	}

	if params.Config.Mode != "" && !params.Config.Mode.IsValid() {
		return nil, fmt.Errorf("%w: unsupported permission mode %q", ErrInvalidConfig, params.Config.Mode)
	}

	sess, err := m.getLiveSession(ctx, id)
	if err != nil {
		return nil, err
	}

	sess.Lock()
	oldConfig := sess.Config
	newConfig := sess.Config
	newTitle := sess.Title
	thinkingCleared := false

	if title := strings.TrimSpace(params.Title); title != "" {
		newTitle = title
	}
	if agent := strings.TrimSpace(params.Config.Agent); agent != "" {
		newConfig.Agent = agent
	}
	if model := models.ID(strings.TrimSpace(string(params.Config.Model))); model != "" {
		_, newConfig.Model = m.resolveAgentModel(newConfig.Agent, model)
	} else if strings.TrimSpace(params.Config.Agent) != "" && sess.Config.Model == "" {
		_, newConfig.Model = m.resolveAgentModel(newConfig.Agent, "")
	}
	if (oldConfig.Model != newConfig.Model || oldConfig.Agent != newConfig.Agent) && newConfig.Thinking != nil {
		if err := m.validateThinking(newConfig.Model, newConfig.Thinking); errors.Is(err, models.ErrInvalidConfig) {
			newConfig.Thinking = nil
			thinkingCleared = true
		}
	}
	if params.Config.Thinking != nil {
		thinkingCleared = false
		if err := m.validateThinking(newConfig.Model, params.Config.Thinking); err != nil {
			sess.Unlock()
			return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
		}
		if params.Config.Thinking.Type == "default" {
			newConfig.Thinking = nil
		} else {
			value := *params.Config.Thinking
			newConfig.Thinking = &value
		}
	}
	if params.Config.Mode != "" {
		newConfig.Mode = params.Config.Mode
	}

	updatedAt := time.Now().UTC()

	dbSess := &Session{
		ID:        sess.ID,
		Title:     newTitle,
		Config:    newConfig,
		CreatedAt: sess.CreatedAt,
		UpdatedAt: updatedAt,
	}

	if err := m.store.updateSession(ctx, dbSess); err != nil {
		sess.Unlock()
		return nil, err
	}

	sess.Title = newTitle
	sess.Config = newConfig
	configChanged := oldConfig.Agent != newConfig.Agent || oldConfig.Model != newConfig.Model || !reflect.DeepEqual(oldConfig.Thinking, newConfig.Thinking)
	if configChanged {
		sess.Diagnostics = sess.modelDiagnostics(newConfig)
		if thinkingCleared {
			sess.Diagnostics = append(sess.Diagnostics, api.SessionDiagnostic{Code: "thinking_override_cleared", Setting: "thinking", Message: fmt.Sprintf("The thinking override was cleared because it is unsupported by model %q.", newConfig.Model)})
		}
	}
	sess.UpdatedAt = updatedAt
	if oldConfig.Mode != newConfig.Mode && sess.PermManager != nil {
		sess.PermManager.SetMode(newConfig.Mode)
	}
	sess.Unlock()

	if configChanged {
		_ = sess.configureHarness(ctx)
	}

	if oldConfig.Agent != newConfig.Agent {
		entry := timeline.NewAgentEntry(sess.ID, api.AgentChange{From: oldConfig.Agent, To: newConfig.Agent})
		if err := sess.Timeline.Append(ctx, entry); err != nil {
			return nil, err
		}
		publishEntryAdded(m.publisher, sess.ID, entry)
	}
	if oldConfig.Model != newConfig.Model {
		entry := timeline.NewModelEntry(sess.ID, api.ModelChange{From: string(oldConfig.Model), To: string(newConfig.Model)})
		if err := sess.Timeline.Append(ctx, entry); err != nil {
			return nil, err
		}
		publishEntryAdded(m.publisher, sess.ID, entry)
	}
	if !reflect.DeepEqual(oldConfig.Thinking, newConfig.Thinking) {
		entry := &timeline.Entry{SessionID: sess.ID, Kind: api.TimelineKindThinking, Thinking: &api.ThinkingChange{From: oldConfig.Thinking, To: newConfig.Thinking}}
		if err := sess.Timeline.Append(ctx, entry); err != nil {
			return nil, err
		}
		publishEntryAdded(m.publisher, sess.ID, entry)
	}
	if oldConfig.Mode != newConfig.Mode {
		entry := timeline.NewModeEntry(sess.ID, api.ModeChange{From: oldConfig.Mode, To: newConfig.Mode})
		if err := sess.Timeline.Append(ctx, entry); err != nil {
			return nil, err
		}
		publishEntryAdded(m.publisher, sess.ID, entry)
	}

	if oldConfig.Model != newConfig.Model || oldConfig.Agent != newConfig.Agent {
		var limits models.Limits
		if model, ok := m.catalog().Get(newConfig.Model); ok {
			limits = model.Limits
		}
		breakdown := m.workspace.Catalog().AgentContext(newConfig.Agent)
		staticCtx := harness.StaticContextFromBreakdown(breakdown)
		sess.Lock()
		res, trig := harness.CompactionTriggerTokens(limits)
		sess.Context.MaxTokens = limits.Context
		sess.Context.OutputLimit = limits.Output
		sess.Context.ReserveTokens = res
		sess.Context.TriggerTokens = trig
		sess.Context.FreeTokens = max(0, trig-sess.Context.UsedTokens)
		sess.Context.Breakdown.SystemPrompt = staticCtx.SystemPrompt
		sess.Context.Breakdown.ContextFiles = staticCtx.ContextFiles
		sess.Context.Breakdown.SystemTools = staticCtx.SystemTools
		sess.Context.Breakdown.CustomTools = staticCtx.CustomTools
		sess.Context.Breakdown.MCPTools = staticCtx.MCPTools
		sess.Context.Breakdown.Skills = staticCtx.Skills
		sess.Context.Breakdown.Subagents = staticCtx.Subagents
		ctxState := sess.Context
		sess.Unlock()
		m.publisher.Publish(api.SessionStreamTopic(sess.ID), api.ContextUpdatedEvent{
			SessionID: sess.ID,
			Context:   ctxState,
		})
	}

	m.publisher.Publish(api.TopicSessions, api.SessionUpdatedEvent{
		Workspace: m.workspace.RootDir(),
		Session:   ToAPISession(sess),
	})

	return sess, nil
}

func (m *manager) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrEmptySessionID
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	sess := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()

	if sess == nil {
		if live, err := m.getLiveSession(ctx, id); err == nil {
			sess = live
			m.mu.Lock()
			delete(m.sessions, id)
			m.mu.Unlock()
		}
	}

	if sess != nil {
		_ = sess.Cancel(ctx)
		_ = sess.Close()
		if sess.Timeline != nil {
			_ = sess.Timeline.Delete(ctx)
		}
		if sess.Journal != nil {
			_ = sess.Journal.Delete(ctx)
		}
	}

	if err := m.store.deleteSession(ctx, id); err != nil {
		return err
	}

	sessDir, err := m.sessionDir(id)
	if err == nil {
		_ = os.RemoveAll(sessDir)
	}

	m.publisher.Publish(api.TopicSessions, api.SessionDeletedEvent{
		Workspace: m.workspace.RootDir(),
		SessionID: id,
	})
	m.publisher.DeleteTopic(api.SessionStreamTopic(id))

	return nil
}

func (m *manager) Close() error {
	m.bgCancel()
	m.mu.Lock()
	for _, sess := range m.sessions {
		_ = sess.Close()
	}
	m.mu.Unlock()
	m.wg.Wait()
	return nil
}
