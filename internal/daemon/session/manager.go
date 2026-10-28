package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/harness"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// Config holds runtime dependencies required to construct a [Manager].
type Config struct {
	DB          *sql.DB
	Workspace   workspace.Workspace
	Gateway     gateway.Gateway
	Publisher   Publisher
	Logger      log.Interface
	Permissions permissions.Manager
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
	Get(ctx context.Context, id string) (*Session, error)
	List(ctx context.Context) ([]*Session, error)
	Update(ctx context.Context, id string, params UpdateParams) (*Session, error)
	Delete(ctx context.Context, id string) error
	Close() error
}

type manager struct {
	db        *sql.DB
	store     *store
	workspace workspace.Workspace
	gateway   gateway.Gateway
	publisher Publisher
	logger    log.Interface
	perms     permissions.Manager

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

	bgCtx, bgCancel := context.WithCancel(context.Background())
	return &manager{
		db:        cfg.DB,
		store:     st,
		workspace: cfg.Workspace,
		gateway:   cfg.Gateway,
		publisher: cfg.Publisher,
		logger:    cfg.Logger,
		perms:     cfg.Permissions,
		bgCtx:     bgCtx,
		bgCancel:  bgCancel,
		sessions:  make(map[string]*Session),
	}, nil
}

// validateSessionID requires a canonical lowercase UUIDv7.
func validateSessionID(id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 7 || parsed.String() != id {
		return fmt.Errorf("%w: %q", ErrInvalidSessionID, id)
	}
	return nil
}

func (m *manager) sessionDir(sessionID string) (string, error) {
	wsDir, err := xdg.WorkspaceDir(m.workspace.RootDir())
	if err != nil {
		return "", fmt.Errorf("session: resolve workspace data dir: %w", err)
	}
	return filepath.Join(wsDir, "sessions", filepath.Base(sessionID)), nil
}

func (m *manager) resolveModelName(model string) string {
	model = strings.TrimSpace(model)
	if model != "" && !strings.Contains(model, "/") {
		if models := m.workspace.ResolveModels(workspace.RoleCompletion, model); len(models) > 0 {
			return models[0]
		}
	}
	return model
}

func (m *manager) resolveAgentAndModel(agent, model string) (string, string) {
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

func (m *manager) matchesParams(s *Session, params CreateParams) error {
	s.Lock()
	defer s.Unlock()

	wantTitle := strings.TrimSpace(params.Title)
	if wantTitle != "" && s.Title != wantTitle {
		return ErrSessionConflict
	}
	wantAgent, wantModel := m.resolveAgentAndModel(params.Config.Agent, params.Config.Model)
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
		publisher:   m.publisher,
		logger:      m.logger,
		bgCtx:       m.bgCtx,
		wg:          &m.wg,
	}

	if err := sess.initResources(ctx); err != nil {
		return nil, err
	}

	if h, err := sess.harness(ctx); err == nil {
		if st, err := h.LoadState(ctx); err == nil {
			sess.Lock()
			sess.Context = st.Context
			if len(st.Todos) > 0 {
				sess.Todos = slices.Clone(st.Todos)
			}
			if len(st.PendingPermissions) > 0 && sess.Status == StatusIdle {
				sess.Status = StatusWaitingPermission
				sess.WaitingPermission = true
				perms := make([]tools.PermissionRequest, len(st.PendingPermissions))
				for i, p := range st.PendingPermissions {
					p.GrantPatterns = slices.Clone(p.GrantPatterns)
					perms[i] = p
				}
				sess.PendingPermissions = perms
			}
			if len(st.PendingQuestions) > 0 && sess.Status != StatusThinking {
				sess.WaitingQuestion = true
				sess.PendingQuestions = make([]tools.QuestionRequest, len(st.PendingQuestions))
				for i := range st.PendingQuestions {
					sess.PendingQuestions[i] = *st.PendingQuestions[i].Clone()
				}
				if !sess.WaitingPermission {
					sess.Status = StatusWaitingQuestion
				}
			}
			sess.Unlock()
		}
	}

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
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	id := strings.TrimSpace(params.ID)
	if id != "" {
		if err := validateSessionID(id); err != nil {
			return nil, false, err
		}
	}

	mode := params.Config.Mode
	if mode == "" {
		mode = tools.ModeDefault
	}
	if !mode.IsValid() {
		return nil, false, fmt.Errorf("%w: unsupported permission mode %q", ErrInvalidConfig, params.Config.Mode)
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

	agent, model := m.resolveAgentAndModel(params.Config.Agent, params.Config.Model)
	if params.Config.Thinking != nil {
		prof, _ := m.gateway.ModelProfile(model)
		if err := validateThinking(params.Config.Thinking, prof); err != nil {
			return nil, false, err
		}
		if params.Config.Thinking.Type == "default" {
			params.Config.Thinking = nil
		}
	}

	now := time.Now().UTC()
	if id == "" {
		generated, err := uuid.NewV7()
		if err != nil {
			return nil, false, fmt.Errorf("session: generate session id: %w", err)
		}
		id = generated.String()
	}

	sessDir, err := m.sessionDir(id)
	if err != nil {
		return nil, false, err
	}

	newSession := &Session{
		State: NewState(),
		ID:    id,
		Title: strings.TrimSpace(params.Title),
		Config: SessionConfig{
			Agent:    agent,
			Model:    model,
			Thinking: params.Config.Thinking,
			Mode:     mode,
		},
		CreatedAt:   now,
		UpdatedAt:   now,
		Permissions: permissions.Permissions{},
		permRoot:    m.perms,
		Root:        sessDir,
		db:          m.db,
		store:       m.store,
		workspace:   m.workspace,
		gateway:     m.gateway,
		publisher:   m.publisher,
		logger:      m.logger,
		bgCtx:       m.bgCtx,
		wg:          &m.wg,
	}

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

	m.publisher.Publish(api.TopicSessions, CreatedEvent{
		Workspace: m.workspace.RootDir(),
		Session:   newSession.Clone(),
	})

	return newSession, true, nil
}

func (m *manager) Get(ctx context.Context, id string) (*Session, error) {
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

	if title := strings.TrimSpace(params.Title); title != "" {
		newTitle = title
	}
	if agent := strings.TrimSpace(params.Config.Agent); agent != "" {
		newConfig.Agent = agent
	}
	if model := strings.TrimSpace(params.Config.Model); model != "" {
		newConfig.Model = m.resolveModelName(model)
	} else if strings.TrimSpace(params.Config.Agent) != "" && sess.Config.Model == "" {
		_, newConfig.Model = m.resolveAgentAndModel(newConfig.Agent, "")
	}
	if oldConfig.Model != newConfig.Model || oldConfig.Agent != newConfig.Agent {
		prof, _ := m.gateway.ModelProfile(newConfig.Model)
		if newConfig.Thinking != nil && validateThinking(newConfig.Thinking, prof) != nil {
			newConfig.Thinking = nil
		}
	}
	if params.Config.Thinking != nil {
		prof, _ := m.gateway.ModelProfile(newConfig.Model)
		if err := validateThinking(params.Config.Thinking, prof); err != nil {
			sess.Unlock()
			return nil, err
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
	sess.UpdatedAt = updatedAt
	if oldConfig.Agent != newConfig.Agent || oldConfig.Model != newConfig.Model || !reflect.DeepEqual(oldConfig.Thinking, newConfig.Thinking) {
		sess.Harness = nil
	}
	if oldConfig.Mode != newConfig.Mode && sess.PermManager != nil {
		sess.PermManager.SetMode(newConfig.Mode)
	}
	sess.Unlock()

	if oldConfig.Agent != newConfig.Agent {
		entry := timeline.NewAgentEntry(sess.ID, timeline.AgentChange{From: oldConfig.Agent, To: newConfig.Agent})
		if err := sess.Timeline.Append(ctx, entry); err != nil {
			return nil, err
		}
		m.publisher.Publish(api.SessionStreamTopic(sess.ID), EntryAddedEvent{
			SessionID: sess.ID,
			Entry:     entry.Clone(),
		})
	}
	if oldConfig.Model != newConfig.Model {
		entry := timeline.NewModelEntry(sess.ID, timeline.ModelChange{From: oldConfig.Model, To: newConfig.Model})
		if err := sess.Timeline.Append(ctx, entry); err != nil {
			return nil, err
		}
		m.publisher.Publish(api.SessionStreamTopic(sess.ID), EntryAddedEvent{
			SessionID: sess.ID,
			Entry:     entry.Clone(),
		})
	}
	if !reflect.DeepEqual(oldConfig.Thinking, newConfig.Thinking) {
		entry := &timeline.Entry{SessionID: sess.ID, Kind: timeline.KindThinking, Thinking: &api.ThinkingChange{From: oldConfig.Thinking, To: newConfig.Thinking}}
		if err := sess.Timeline.Append(ctx, entry); err != nil {
			return nil, err
		}
		m.publisher.Publish(api.SessionStreamTopic(sess.ID), EntryAddedEvent{SessionID: sess.ID, Entry: entry.Clone()})
	}
	if oldConfig.Mode != newConfig.Mode {
		entry := timeline.NewModeEntry(sess.ID, timeline.ModeChange{From: oldConfig.Mode, To: newConfig.Mode})
		if err := sess.Timeline.Append(ctx, entry); err != nil {
			return nil, err
		}
		m.publisher.Publish(api.SessionStreamTopic(sess.ID), EntryAddedEvent{
			SessionID: sess.ID,
			Entry:     entry.Clone(),
		})
	}

	if oldConfig.Model != newConfig.Model || oldConfig.Agent != newConfig.Agent {
		limits := m.workspace.ModelLimits(newConfig.Model)
		if prof, ok := m.gateway.ModelProfile(newConfig.Model); ok && prof != nil {
			if prof.Limits.Context > 0 {
				limits.Context = prof.Limits.Context
			}
			if prof.Limits.Output > 0 {
				limits.Output = prof.Limits.Output
			}
		}
		breakdown := m.workspace.AgentContext(newConfig.Agent)
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
		m.publisher.Publish(api.SessionStreamTopic(sess.ID), ContextUpdatedEvent{
			SessionID: sess.ID,
			Context:   ctxState,
		})
	}

	m.publisher.Publish(api.TopicSessions, UpdatedEvent{
		Workspace: m.workspace.RootDir(),
		Session:   sess.Clone(),
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

	m.publisher.Publish(api.TopicSessions, DeletedEvent{
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
