package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/ptr"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/harness"
	"github.com/masterkeysrd/tasksmith/internal/daemon/journal"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

// Config holds runtime dependencies and parameters for initializing a [Session].
type Config struct {
	ID          string
	Title       string
	Session     api.SessionConfig
	DB          *sql.DB
	Workspace   *workspace.Workspace
	Gateway     func() gateway.Gateway
	Catalog     func() *models.Catalog
	Permissions permissions.Manager
	Publisher   Publisher
	Logger      log.Interface
	Metrics     metrics.Service
	ReloadLock  *sync.RWMutex
	Context     context.Context
	WaitGroup   *sync.WaitGroup
	OnUpdate    func(ctx context.Context, s *Session) error
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

func (c *Config) applyDefaults() error {
	if c.ReloadLock == nil {
		c.ReloadLock = &sync.RWMutex{}
	}
	if c.Catalog == nil {
		catalog, err := models.Build(c.Workspace.Catalog(), c.Gateway())
		if err != nil {
			return fmt.Errorf("session: build model catalog: %w", err)
		}
		c.Catalog = func() *models.Catalog { return catalog }
	}
	if c.Context == nil {
		c.Context = context.Background()
	}
	if c.WaitGroup == nil {
		c.WaitGroup = &sync.WaitGroup{}
	}
	return nil
}

func initSession(ctx context.Context, sess *Session, cfg Config) error {
	dir, err := SessionDir(cfg.Workspace.RootDir(), sess.ID)
	if err != nil {
		return err
	}
	sess.State = NewState()
	sess.permRoot = cfg.Permissions
	sess.Root = dir
	sess.workspace = cfg.Workspace
	sess.gateway = cfg.Gateway
	sess.catalog = cfg.Catalog
	sess.metrics = cfg.Metrics
	sess.reloadLock = cfg.ReloadLock
	sess.publisher = cfg.Publisher
	sess.logger = cfg.Logger
	sess.bgCtx = cfg.Context
	sess.wg = cfg.WaitGroup
	sess.onUpdate = cfg.OnUpdate
	sess.Diagnostics = sess.modelDiagnostics(sess.Config)
	return sess.initSubsystems(ctx, cfg.DB)
}

// New constructs and initializes a new [Session].
func New(ctx context.Context, cfg Config) (*Session, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	id := strings.TrimSpace(cfg.ID)
	if id != "" {
		if !uuid.Validate(id) {
			return nil, fmt.Errorf("%w: invalid session id %q", ErrInvalidSessionID, id)
		}
	} else {
		id = uuid.NewString()
	}

	if err := cfg.applyDefaults(); err != nil {
		return nil, err
	}

	resolvedConfig, err := resolveConfig(cfg.Workspace, cfg.Catalog(), cfg.Session)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	sess := &Session{
		ID:        id,
		Title:     strings.TrimSpace(cfg.Title),
		Config:    resolvedConfig,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := initSession(ctx, sess, cfg); err != nil {
		return nil, err
	}
	_ = sess.configureHarness(ctx)

	return sess, nil
}

// Open restores and initializes an existing [Session] from persisted state.
func Open(ctx context.Context, dbSess *Session, cfg Config) (*Session, error) {
	if dbSess == nil {
		return nil, fmt.Errorf("%w: session is nil", ErrInvalidConfig)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if err := cfg.applyDefaults(); err != nil {
		return nil, err
	}

	if !models.ID(dbSess.Config.Model).Valid() {
		if model, ok := cfg.Catalog().Resolve(models.RoleCompletion, dbSess.Config.Model); ok {
			dbSess.Config.Model = string(model.ID)
			if cfg.OnUpdate != nil {
				if err := cfg.OnUpdate(ctx, dbSess); err != nil {
					return nil, err
				}
			}
		}
	}

	sess := &Session{
		ID:          dbSess.ID,
		Title:       dbSess.Title,
		Config:      dbSess.Config,
		CreatedAt:   dbSess.CreatedAt,
		UpdatedAt:   dbSess.UpdatedAt,
		Permissions: dbSess.Permissions.Clone(),
	}

	if err := initSession(ctx, sess, cfg); err != nil {
		return nil, err
	}

	if st, err := sess.Harness.LoadState(ctx); err == nil {
		sess.Lock()
		sess.Context = st.Context
		if len(st.Todos) > 0 {
			sess.Todos = slices.Clone(st.Todos)
		}
		if len(st.PendingPermissions) > 0 && sess.Status == api.StatusIdle {
			sess.Status = api.StatusWaitingPermission
			perms := make([]api.PermissionRequest, len(st.PendingPermissions))
			for i, p := range st.PendingPermissions {
				p.GrantPatterns = slices.Clone(p.GrantPatterns)
				perms[i] = p
			}
			sess.PendingPermissions = perms
		}
		if len(st.PendingQuestions) > 0 && sess.Status != api.StatusThinking {
			sess.PendingQuestions = make([]api.QuestionRequest, len(st.PendingQuestions))
			for i := range st.PendingQuestions {
				sess.PendingQuestions[i] = *st.PendingQuestions[i].Clone()
			}
			if len(st.PendingPermissions) == 0 {
				sess.Status = api.StatusWaitingQuestion
			}
		}
		sess.Unlock()
	}
	_ = sess.configureHarness(ctx)

	return sess, nil
}

// Session represents a persistent user-facing chat thread and its runtime state.
type Session struct {
	// We embed [State] here so that Session supports state fields and locking
	// directly, but marshal it as a struct in JSON.
	//
	// State also provides a [sync.Mutex] which is used as lock for both
	// the Session and State.
	*State `json:"state"`

	ID          string                  `json:"id"`
	Title       string                  `json:"title"`
	Config      api.SessionConfig       `json:"config"`
	Diagnostics []api.SessionDiagnostic `json:"diagnostics,omitempty"`
	CreatedAt   time.Time               `json:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at"`

	Permissions permissions.Permissions    `json:"-"`
	PermManager permissions.SessionManager `json:"-"`
	permRoot    permissions.Manager        `json:"-"`

	Root       string            `json:"-"` // Path to <WorkspaceDir>/sessions/<id>
	Sandbox    sandbox.Sandbox   `json:"-"`
	Timeline   timeline.Timeline `json:"-"`
	Journal    journal.Journal   `json:"-"`
	Tools      tools.Container   `json:"-"`
	Tasks      tasks.Store       `json:"-"`
	Harness    harness.Harness   `json:"-"`
	Std        *std.Std          `json:"-"`
	spiller    tools.Spiller     `json:"-"`
	harnessErr error

	// turnIDs and turnStart track the turn in progress: the user messages that
	// started it and when the first of them was recorded. Guarded by the
	// session lock.
	turnIDs   []string
	turnStart time.Time

	workspace  *workspace.Workspace   `json:"-"`
	gateway    func() gateway.Gateway `json:"-"`
	catalog    func() *models.Catalog `json:"-"`
	metrics    metrics.Service        `json:"-"`
	reloadLock *sync.RWMutex          `json:"-"`
	publisher  Publisher              `json:"-"`
	logger     log.Interface          `json:"-"`
	bgCtx      context.Context        `json:"-"`
	wg         *sync.WaitGroup        `json:"-"`

	onUpdate func(ctx context.Context, s *Session) error
}

// Clone returns a deep copy of the session and its exported execution state,
// synchronizing on the session's embedded mutex.
func (s *Session) Clone() *Session {
	if s == nil {
		return nil
	}
	if s.State != nil {
		s.Lock()
		defer s.Unlock()
	}
	cpConfig := s.Config
	cpConfig.Thinking = ptr.Clone(s.Config.Thinking)
	cp := &Session{
		ID:          s.ID,
		Title:       s.Title,
		Config:      cpConfig,
		Diagnostics: slices.Clone(s.Diagnostics),
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
		Permissions: s.Permissions.Clone(),
		PermManager: s.PermManager,
		permRoot:    s.permRoot,
		Root:        s.Root,
		Sandbox:     s.Sandbox,
		Timeline:    s.Timeline,
		Journal:     s.Journal,
		Tools:       s.Tools,
		Tasks:       s.Tasks,
		Harness:     s.Harness,
		Std:         s.Std,
		spiller:     s.spiller,
		workspace:   s.workspace,
		gateway:     s.gateway,
		catalog:     s.catalog,
		metrics:     s.metrics,
		reloadLock:  s.reloadLock,
		publisher:   s.publisher,
		logger:      s.logger,
		bgCtx:       s.bgCtx,
		wg:          s.wg,
		onUpdate:    s.onUpdate,
	}
	if s.State != nil {
		cp.State = s.State.Clone()
	} else {
		cp.State = NewState()
	}
	return cp
}

// CheckpointTo persists a snapshot of the session to store.
func (s *Session) CheckpointTo(ctx context.Context, store Store) error {
	return store.Save(ctx, s.Clone())
}

func (s *Session) checkpoint(ctx context.Context) error {
	if s.onUpdate == nil {
		return nil
	}
	return s.onUpdate(ctx, s)
}

func (s *Session) setStatus(next api.SessionStatus) {
	s.Lock()
	if s.Status == next {
		s.Unlock()
		return
	}
	s.Status = next
	s.Running = (next == api.StatusThinking || next == api.StatusGenerating || next == api.StatusExecuting || next == api.StatusCompacting)
	s.Unlock()

	s.publisher.Publish(api.SessionStreamTopic(s.ID), api.StatusChangedEvent{
		SessionID: s.ID,
		Status:    next,
	})
}

func (s *Session) ResolvedModel() (models.Model, *api.ThinkingSetting, bool) {
	if s.catalog == nil {
		return models.Model{}, nil, false
	}
	modelID := models.ID(s.Config.Model)
	if !modelID.Valid() {
		return models.Model{}, nil, false
	}
	m, ok := s.catalog().Get(modelID)
	if !ok {
		return models.Model{}, nil, false
	}
	thinking := s.Config.Thinking
	if thinking == nil {
		if agent, found := s.workspace.Catalog().GetAgent(s.Config.Agent); found {
			thinking = agent.Thinking
		}
	}
	return m, thinking, true
}

func (s *Session) initSubsystems(ctx context.Context, db *sql.DB) error {
	tl, err := timeline.New(ctx, db, s.ID)
	if err != nil {
		return fmt.Errorf("session: initialize timeline: %w", err)
	}
	s.Timeline = tl

	blobsDir := filepath.Join(s.Root, "blobs")
	jr, err := journal.New(ctx, db, s.ID, blobsDir, journal.WithMetrics(s.metrics))
	if err != nil {
		return fmt.Errorf("session: initialize journal: %w", err)
	}
	s.Journal = jr

	sb, err := sandbox.New(sandbox.Config{
		ID:      s.ID,
		WorkDir: s.workspace.RootDir(),
		Mode:    sandbox.ModeLocal,
		Env:     s.workspace.Env(),
		Network: sandbox.NetworkPolicy{
			AllowLoopback: true,
		},
	})
	if err != nil {
		return fmt.Errorf("session: initialize sandbox: %w", err)
	}
	s.Sandbox = sb

	s.Tasks = tasks.New(tasks.Config{
		Context:    s.bgCtx,
		OutputsDir: s.outputsDir(),
		OnUpdate:   s.onTaskUpdate,
	})

	stdInst, err := std.New(std.Config{
		Sandbox: s.Sandbox,
		Tracker: s.Journal,
		Logger:  s.logger,
	})
	if err != nil {
		return fmt.Errorf("session: initialize std tools: %w", err)
	}
	s.Std = stdInst

	sp, err := tools.NewSpiller(tools.SpillerConfig{Workspace: s.workspace})
	if err != nil {
		return fmt.Errorf("session: initialize spiller: %w", err)
	}
	s.spiller = sp

	initialAgent := s.Config.Agent
	if initialAgent == "" {
		initialAgent = s.workspace.Catalog().DefaultAgentName()
	}
	container, err := s.buildToolsForAgent(initialAgent)
	if err != nil {
		return err
	}
	s.Tools = container

	s.PermManager = s.permRoot.Attach(s.Config.Mode, s.Permissions, func(p permissions.Permissions) error {
		s.Lock()
		s.Permissions = p.Clone()
		s.Unlock()
		return s.checkpoint(s.bgCtx)
	})

	h, err := harness.New(harness.Config{
		ID:          s.ID,
		DB:          db,
		Bash:        s.Std,
		Spiller:     s.spiller,
		Permissions: s.PermManager,
		Queue:       s,
		Logger:      s.logger,
		Metrics:     s.metrics,
	})
	if err != nil {
		return fmt.Errorf("session: initialize harness: %w", err)
	}
	s.Harness = h
	return nil
}

// Matches returns nil if the session matches the parameters of req,
// or ErrSessionConflict if any parameter conflicts.
func (s *Session) Matches(req api.CreateSessionRequest) error {
	s.Lock()
	defer s.Unlock()

	wantTitle := strings.TrimSpace(req.Title)
	if wantTitle != "" && s.Title != wantTitle {
		return ErrSessionConflict
	}
	wantAgent, wantModel := resolveAgentModel(s.workspace, s.catalog(), req.Config.Agent, models.ID(req.Config.Model))
	if wantAgent != "" && s.Config.Agent != wantAgent {
		return ErrSessionConflict
	}
	if wantModel != "" && s.Config.Model != string(wantModel) {
		return ErrSessionConflict
	}
	if req.Config.Thinking != nil && !reflect.DeepEqual(s.Config.Thinking, req.Config.Thinking) {
		return ErrSessionConflict
	}
	wantMode := req.Config.Mode
	if wantMode != "" && s.Config.Mode != wantMode {
		return ErrSessionConflict
	}
	return nil
}

// Refresh recalculates session diagnostics and reconfigures the harness
// (e.g. after a workspace reload), publishing a SessionUpdatedEvent.
func (s *Session) Refresh(ctx context.Context) {
	s.Lock()
	s.Diagnostics = s.modelDiagnostics(s.Config)
	s.Unlock()
	_ = s.configureHarness(ctx)
	s.publisher.Publish(api.TopicSessions, api.SessionUpdatedEvent{
		Workspace: s.workspace.RootDir(),
		Session:   ToAPISession(s),
	})
}

// Update updates the session title or configuration, persisting changes and reconfiguring runtime harnesses.
func (s *Session) Update(ctx context.Context, req api.UpdateSessionRequest) error {
	s.reloadLock.RLock()
	defer s.reloadLock.RUnlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	if req.Config.Mode != "" && !req.Config.Mode.IsValid() {
		return fmt.Errorf("%w: unsupported permission mode %q", ErrInvalidConfig, req.Config.Mode)
	}

	s.Lock()
	oldConfig := s.Config
	newConfig := s.Config
	newTitle := s.Title
	thinkingCleared := false

	if title := strings.TrimSpace(req.Title); title != "" {
		newTitle = title
	}
	if agent := strings.TrimSpace(req.Config.Agent); agent != "" {
		newConfig.Agent = agent
	}
	cat := s.catalog()
	if model := models.ID(strings.TrimSpace(req.Config.Model)); model != "" {
		_, resolved := resolveAgentModel(s.workspace, cat, newConfig.Agent, model)
		newConfig.Model = string(resolved)
	} else if strings.TrimSpace(req.Config.Agent) != "" && s.Config.Model == "" {
		_, resolved := resolveAgentModel(s.workspace, cat, newConfig.Agent, "")
		newConfig.Model = string(resolved)
	}
	if (oldConfig.Model != newConfig.Model || oldConfig.Agent != newConfig.Agent) && newConfig.Thinking != nil {
		if err := validateThinking(cat, models.ID(newConfig.Model), newConfig.Thinking); errors.Is(err, models.ErrInvalidConfig) {
			newConfig.Thinking = nil
			thinkingCleared = true
		}
	}
	if req.Config.Thinking != nil {
		thinkingCleared = false
		if err := validateThinking(cat, models.ID(newConfig.Model), req.Config.Thinking); err != nil {
			s.Unlock()
			return fmt.Errorf("%w: %v", ErrInvalidConfig, err)
		}
		if req.Config.Thinking.Type == "default" {
			newConfig.Thinking = nil
		} else {
			value := *req.Config.Thinking
			newConfig.Thinking = &value
		}
	}
	if req.Config.Mode != "" {
		newConfig.Mode = req.Config.Mode
	}

	updatedAt := time.Now().UTC()
	prevTitle := s.Title
	prevConfig := s.Config
	prevDiagnostics := s.Diagnostics
	prevUpdatedAt := s.UpdatedAt

	s.Title = newTitle
	s.Config = newConfig
	configChanged := oldConfig.Agent != newConfig.Agent || oldConfig.Model != newConfig.Model || !reflect.DeepEqual(oldConfig.Thinking, newConfig.Thinking)
	if configChanged {
		s.Diagnostics = s.modelDiagnostics(newConfig)
		if thinkingCleared {
			s.Diagnostics = append(s.Diagnostics, api.SessionDiagnostic{Code: "thinking_override_cleared", Setting: "thinking", Message: fmt.Sprintf("The thinking override was cleared because it is unsupported by model %q.", newConfig.Model)})
		}
	}
	s.UpdatedAt = updatedAt
	if oldConfig.Mode != newConfig.Mode {
		s.PermManager.SetMode(newConfig.Mode)
	}
	s.Unlock()

	if err := s.checkpoint(ctx); err != nil {
		s.Lock()
		s.Title = prevTitle
		s.Config = prevConfig
		s.Diagnostics = prevDiagnostics
		s.UpdatedAt = prevUpdatedAt
		if oldConfig.Mode != newConfig.Mode {
			s.PermManager.SetMode(oldConfig.Mode)
		}
		s.Unlock()
		return err
	}

	if configChanged {
		_ = s.configureHarness(ctx)
	}

	var entries []*timeline.Entry
	if oldConfig.Agent != newConfig.Agent {
		entries = append(entries, timeline.NewAgentEntry(s.ID, api.AgentChange{From: oldConfig.Agent, To: newConfig.Agent}))
	}
	if oldConfig.Model != newConfig.Model {
		entries = append(entries, timeline.NewModelEntry(s.ID, api.ModelChange{From: oldConfig.Model, To: newConfig.Model}))
	}
	if !reflect.DeepEqual(oldConfig.Thinking, newConfig.Thinking) {
		entries = append(entries, &timeline.Entry{SessionID: s.ID, Kind: api.TimelineKindThinking, Thinking: &api.ThinkingChange{From: oldConfig.Thinking, To: newConfig.Thinking}})
	}
	if oldConfig.Mode != newConfig.Mode {
		entries = append(entries, timeline.NewModeEntry(s.ID, api.ModeChange{From: oldConfig.Mode, To: newConfig.Mode}))
	}
	for _, entry := range entries {
		if err := s.Timeline.Append(ctx, entry); err != nil {
			return err
		}
		publishEntryAdded(s.publisher, s.ID, entry)
	}

	if oldConfig.Model != newConfig.Model || oldConfig.Agent != newConfig.Agent {
		s.Lock()
		ctxState := s.Context
		s.Unlock()
		s.publisher.Publish(api.SessionStreamTopic(s.ID), api.ContextUpdatedEvent{
			SessionID: s.ID,
			Context:   ctxState,
		})
	}

	s.publisher.Publish(api.TopicSessions, api.SessionUpdatedEvent{
		Workspace: s.workspace.RootDir(),
		Session:   ToAPISession(s),
	})

	return nil
}

// Close closes session-scoped runtime resources like the Sandbox.
func (s *Session) Close() error {
	s.Lock()
	defer s.Unlock()
	if s.Tasks != nil {
		_ = s.Tasks.Close()
		s.Tasks = nil
	}
	if s.Sandbox != nil {
		err := s.Sandbox.Close()
		s.Sandbox = nil
		return err
	}
	return nil
}

func (s *Session) updateContextLimits(agentName, modelName string) (models.Limits, harness.StaticContext) {
	var limits models.Limits
	if model, ok := s.catalog().Get(models.ID(modelName)); ok {
		limits = model.Limits
	}
	breakdown := s.workspace.Catalog().AgentContext(agentName)
	staticCtx := harness.StaticContextFromBreakdown(breakdown)

	s.Lock()
	res, trig := harness.CompactionTriggerTokens(limits)
	s.Context.MaxTokens = limits.Context
	s.Context.OutputLimit = limits.Output
	s.Context.ReserveTokens = res
	s.Context.TriggerTokens = trig
	s.Context.FreeTokens = max(0, trig-s.Context.UsedTokens)
	s.Context.Breakdown.SystemPrompt = staticCtx.SystemPrompt
	s.Context.Breakdown.ContextFiles = staticCtx.ContextFiles
	s.Context.Breakdown.SystemTools = staticCtx.SystemTools
	s.Context.Breakdown.CustomTools = staticCtx.CustomTools
	s.Context.Breakdown.MCPTools = staticCtx.MCPTools
	s.Context.Breakdown.Skills = staticCtx.Skills
	s.Context.Breakdown.Subagents = staticCtx.Subagents
	s.Unlock()

	return limits, staticCtx
}

func (s *Session) configureHarness(ctx context.Context) error {
	s.Lock()
	config := s.Config
	agentName := config.Agent
	modelName := config.Model
	s.Unlock()

	limits, staticCtx := s.updateContextLimits(agentName, modelName)

	model, found := s.catalog().Get(models.ID(modelName))
	if !found {
		err := fmt.Errorf("session: model %q unavailable", modelName)
		s.Lock()
		s.harnessErr = err
		s.Unlock()
		return err
	}
	settings := s.modelSettings(config)
	cfg, _, err := model.Configure(settings)
	if err != nil {
		err = fmt.Errorf("session: configure model %q: %w", modelName, err)
		s.Lock()
		s.harnessErr = err
		s.Unlock()
		return err
	}
	llmModel, err := s.gateway().LLM(ctx, model, cfg)
	if err != nil {
		err = fmt.Errorf("session: resolve model %q: %w", modelName, err)
		s.Lock()
		s.harnessErr = err
		s.Unlock()
		return err
	}

	systemPrompt := s.renderSystemPrompt(agentName)

	container, err := s.buildToolsForAgent(agentName)
	if err != nil {
		err = fmt.Errorf("session: build tools for agent %q: %w", agentName, err)
		s.Lock()
		s.harnessErr = err
		s.Unlock()
		return err
	}

	opts := harness.NodeOptions{
		Model:        llmModel,
		Tools:        container,
		SystemPrompt: systemPrompt,
		Limits:       limits,
		Static:       staticCtx,
	}
	err = s.Harness.Configure(opts)

	s.Lock()
	s.Tools = container
	s.harnessErr = err
	s.Unlock()

	return err
}

func (s *Session) outputsDir() string {
	return filepath.Join(s.Root, "outputs")
}

func (s *Session) buildToolsForAgent(agentName string) (tools.Container, error) {
	cat := s.workspace.Catalog()
	if agentName == "" {
		agentName = cat.DefaultAgentName()
	}

	var allowedToolNames []string
	var skills []warp.Skill

	if agentName != "" {
		if resolved, err := cat.ResolveAgent(agentName); err == nil && resolved != nil {
			if resolved.Tools != nil {
				allowedToolNames = make([]string, 0, len(resolved.Tools))
				for _, t := range resolved.Tools {
					allowedToolNames = append(allowedToolNames, t.Metadata.Name)
				}
			}
			skills = resolved.Skills
		}
	}

	var readDirs []string
	if outputsDir := s.outputsDir(); outputsDir != "" {
		readDirs = append(readDirs, outputsDir)
	}
	if builtinDir, err := xdg.BuiltinDir(); err == nil && builtinDir != "" {
		readDirs = append(readDirs, builtinDir)
	}

	guardInst := shellguard.New(shellguard.Config{
		WorkspaceDir: s.workspace.RootDir(),
		ReadDirs:     readDirs,
	})

	builtinTools, err := builtin.New(builtin.Config{
		Std:          s.Std,
		Sandbox:      s.Sandbox,
		Guard:        guardInst,
		Tasks:        s.Tasks,
		Workspace:    s.workspace,
		AllowedTools: allowedToolNames,
		Skills:       skills,
	})
	if err != nil {
		return nil, fmt.Errorf("session: initialize builtin tools: %w", err)
	}

	container, err := tools.NewContainer(tools.Config{
		Tools:        builtinTools,
		Sandbox:      s.Sandbox,
		Workspace:    s.workspace,
		Logger:       s.logger,
		Spiller:      s.spiller,
		AllowedTools: allowedToolNames,
	})
	if err != nil {
		return nil, fmt.Errorf("session: initialize tools container: %w", err)
	}

	return container, nil
}

