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

	"github.com/masterkeysrd/loom/tool"
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
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandboxcfg"
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
	ParentID    string
	WorkDir     string
	Parent      *Session
	Session     api.SessionConfig
	DB          *sql.DB
	Workspace   *workspace.Workspace
	Gateway     func() gateway.Gateway
	Catalog     func() *models.Catalog
	Permissions permissions.Manager
	// SandboxSettings optionally carries the workspace's sandbox.json manager
	// (nil keeps the built-in defaults: DefaultMode backend and the built-in
	// policy floor only). It supplies the effective default backend, the
	// failIfUnavailable lock, and user path overrides layered over the
	// built-in security floor.
	SandboxSettings sandboxcfg.Manager
	Publisher       Publisher
	Logger          log.Interface
	Metrics         metrics.Service
	ReloadLock      *sync.RWMutex
	Context         context.Context
	WaitGroup       *sync.WaitGroup
	OnUpdate        func(ctx context.Context, s *Session) error
	// MCPCaller resolves the MCP runtime for this session's workspace, or nil when
	// no MCP manager is attached. It is a function rather than a value because a
	// workspace reload replaces the runtime's context while sessions stay alive, so
	// each rebuild of the session's tool container must resolve the current one.
	MCPCaller func() tools.MCPCaller
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
	sess.sandboxCfg = cfg.SandboxSettings
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
	sess.mcpCaller = cfg.MCPCaller
	sess.db = cfg.DB
	sess.parent = cfg.Parent
	if sess.parent != nil {
		sess.ParentID = sess.parent.ID
	}
	if strings.TrimSpace(sess.WorkDir) == "" {
		sess.WorkDir = cfg.Workspace.RootDir()
	}
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

	if cfg.Session.Mode == "" && cfg.Permissions != nil {
		if eff, err := cfg.Permissions.Effective(); err == nil && eff.DefaultMode != "" {
			cfg.Session.Mode = eff.DefaultMode
		}
	}
	if cfg.Session.Sandbox == "" && cfg.SandboxSettings != nil {
		// A session that does not request a backend inherits the workspace's
		// configured default from sandbox.json (global then project scope).
		if eff, err := cfg.SandboxSettings.Effective(); err == nil && eff.Mode != "" {
			cfg.Session.Sandbox = eff.Mode
		}
	}

	resolvedConfig, err := resolveConfig(cfg.Workspace, cfg.Catalog(), cfg.Session)
	if err != nil {
		return nil, err
	}

	// A top-level human session (no parent) may only select agents that allow the
	// "human" trigger. Subagent sessions inherit the caller's context and are
	// exempt from this restriction.
	if cfg.Parent == nil && strings.TrimSpace(cfg.ParentID) == "" {
		if err := validateHumanAgentTrigger(cfg.Workspace, resolvedConfig.Agent); err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	sess := &Session{
		ID:        id,
		Title:     strings.TrimSpace(cfg.Title),
		Config:    resolvedConfig,
		ParentID:  cfg.ParentID,
		WorkDir:   cfg.WorkDir,
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
		ParentID:    dbSess.ParentID,
		WorkDir:     dbSess.WorkDir,
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
	ParentID    string                  `json:"parent_id,omitempty"`
	WorkDir     string                  `json:"work_dir,omitempty"`
	CreatedAt   time.Time               `json:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at"`

	parent   *Session   `json:"-"`
	children []*Session `json:"-"`
	db       *sql.DB    `json:"-"`

	Permissions api.Permissions            `json:"-"`
	PermManager permissions.SessionManager `json:"-"`
	permRoot    permissions.Manager        `json:"-"`

	sandboxCfg sandboxcfg.Manager `json:"-"`

	Root        string          `json:"-"` // Path to <WorkspaceDir>/sessions/<id>
	Sandbox     sandbox.Sandbox `json:"-"`
	sbxPolicy   sandbox.FilesystemPolicy
	Timeline    timeline.Timeline `json:"-"`
	Journal     journal.Journal   `json:"-"`
	Tools       tools.Container   `json:"-"`
	Tasks       tasks.Store       `json:"-"`
	Harness     harness.Harness   `json:"-"`
	Std         *std.Std          `json:"-"`
	FileIndex   *std.FileIndex    `json:"-"`
	attachments *harness.AttachmentResolver
	spiller     tools.Spiller `json:"-"`
	harnessErr  error

	// turnIDs and turnStart track the turn in progress: the user messages that
	// started it and when the first of them was recorded. Guarded by the
	// session lock.
	turnIDs   []string
	turnStart time.Time

	workspace  *workspace.Workspace   `json:"-"`
	gateway    func() gateway.Gateway `json:"-"`
	catalog    func() *models.Catalog `json:"-"`
	metrics    metrics.Service        `json:"-"`
	mcpCaller  func() tools.MCPCaller `json:"-"`
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
		ParentID:    s.ParentID,
		WorkDir:     s.WorkDir,
		parent:      s.parent,
		children:    slices.Clone(s.children),
		db:          s.db,
		Diagnostics: slices.Clone(s.Diagnostics),
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
		Permissions: s.Permissions.Clone(),
		PermManager: s.PermManager,
		permRoot:    s.permRoot,
		sandboxCfg:  s.sandboxCfg,
		Root:        s.Root,
		Sandbox:     s.Sandbox,
		sbxPolicy:   s.sbxPolicy,
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
	switch next {
	case api.StatusIdle:
		s.Activity = ""
	case api.StatusGenerating:
		s.Activity = "Generating"
	case api.StatusWaitingPermission:
		s.Activity = "Waiting for permission"
	case api.StatusWaitingQuestion:
		s.Activity = "Waiting for answer"
	case api.StatusThinking:
		if s.Activity == "" {
			s.Activity = "Thinking..."
		}
	}
	s.Unlock()

	s.publisher.Publish(api.SessionStreamTopic(s.ID), api.StatusChangedEvent{
		SessionID: s.ID,
		Status:    next,
	})
	s.notifyParentUpdate()
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
	var agentThinking *api.ThinkingSetting
	if agent, found := s.workspace.Catalog().GetAgent(s.Config.Agent); found {
		agentThinking = agent.Thinking
	}
	return m, m.EffectiveThinking(s.Config.Thinking, agentThinking), true
}

func isReadOnlyPolicy(p *warp.Policies) bool {
	return p != nil && p.Tools != nil && p.Tools.ReadOnly
}

// effectiveReadOnlyPolicy reports whether the workspace, the session's
// effective agent policies, or the parent session demand a read-only tool
// sandbox (policies.tools.readOnly). It is deliberately decoupled from the
// session's permission mode: an untrusted (restricted) workspace or a
// readOnly policy blocks all side effects while the session still runs in
// `default` mode.
func (s *Session) effectiveReadOnlyPolicy() bool {
	if s.workspace == nil {
		return false
	}
	if s.workspace.IsRestricted() {
		return true
	}
	// A subagent inherits its parent's kernel read-only ceiling: a child
	// spawned under a readOnly agent must not regain write access just
	// because its own manifest omits the policy.
	if s.parent != nil && s.parent.Sandbox != nil && s.parent.Sandbox.ReadOnly() {
		return true
	}
	cat := s.workspace.Catalog()
	if cat == nil {
		return false
	}
	if isReadOnlyPolicy(cat.Policies()) {
		return true
	}
	if agentName := strings.TrimSpace(s.Config.Agent); agentName != "" {
		// Evaluate the *resolved* agent so policies inherited through skills
		// and agent resolution (the same view the permission ceiling uses)
		// also gate the kernel sandbox.
		if resolved, err := cat.ResolveAgent(agentName); err == nil && resolved != nil {
			if resolved.WorkspacePolicy != nil && resolved.WorkspacePolicy.ReadOnly {
				return true
			}
			if resolved.Agent != nil && isReadOnlyPolicy(resolved.Agent.Spec.Policies) {
				return true
			}
		}
	}
	return false
}

// applyReadOnlyPolicy syncs the sandbox's read-only enforcement with the
// effective workspace/agent policy. Callers must not hold the session lock.
func (s *Session) applyReadOnlyPolicy() {
	if s.Sandbox == nil {
		return
	}
	s.Lock()
	readOnly := s.effectiveReadOnlyPolicy()
	s.Unlock()
	s.Sandbox.SetReadOnly(readOnly)
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

	// The effective posture resolves project-relative rules and the built-in
	// floor against this session's own work directory (a subagent's worktree
	// root, not just the main checkout), and is computed once for both the
	// backend resolution and the path overlay.
	var sandboxEff *sandboxcfg.Effective
	if s.sandboxCfg != nil {
		eff, err := s.sandboxCfg.EffectiveFor(s.WorkDir)
		if err != nil {
			return fmt.Errorf("session: resolve effective sandbox settings: %w", err)
		}
		sandboxEff = &eff
	}

	mode := sandbox.ResolveMode(s.Config.Sandbox)
	if s.parent != nil && s.parent.Sandbox != nil {
		// A subagent inherits its parent's enforced isolation backend.
		mode = s.parent.Sandbox.Mode()
	} else if sandboxEff != nil {
		// A top-level session resolves its backend through the workspace's
		// sandbox.json settings: the request still wins, then the configured
		// default, and failIfUnavailable turns native-unavailable into a hard
		// session-creation error instead of a silent local fallback.
		resolved, err := sandboxEff.ResolveMode(s.Config.Sandbox)
		if err != nil {
			return err
		}
		mode = resolved
	}
	sbPolicy := sandboxFilesystemPolicy(s.WorkDir)
	if sandboxEff != nil {
		sbPolicy = sandboxEff.Overlay(sbPolicy)
	}
	sb, err := sandbox.New(sandbox.Config{
		ID:         s.ID,
		WorkDir:    s.WorkDir,
		Mode:       mode,
		Env:        s.workspace.SubprocessEnv(),
		Filesystem: sbPolicy,
		Network: sandbox.NetworkPolicy{
			AllowLoopback: true,
		},
	})
	if err != nil {
		return fmt.Errorf("session: initialize sandbox: %w", err)
	}
	s.Sandbox = sb
	s.sbxPolicy = sbPolicy
	s.applyReadOnlyPolicy()

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

	if s.parent != nil && s.parent.PermManager != nil {
		s.PermManager = s.parent.PermManager.ForSubagent()
	} else {
		s.PermManager = s.permRoot.Attach(s.Config.Mode, s.Permissions, func(p api.Permissions) error {
			s.Lock()
			s.Permissions = p.Clone()
			s.Unlock()
			return s.checkpoint(s.bgCtx)
		})
	}

	attResolver, err := harness.NewAttachmentResolver(harness.AttachmentResolverConfig{
		AgentName:   s.Config.Agent,
		Workspace:   s.workspace,
		Tools:       s.Tools,
		Std:         s.Std,
		Sandbox:     s.Sandbox,
		Permissions: s.PermManager,
		WorkDir:     s.WorkDir,
		MCPCaller:   s.mcpCaller,
		Logger:      s.logger,
	})
	if err != nil {
		return fmt.Errorf("session: initialize attachment resolver: %w", err)
	}
	s.attachments = attResolver

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
	if wantSandbox := api.SandboxMode(strings.ToLower(strings.TrimSpace(string(req.Config.Sandbox)))); wantSandbox != "" && s.Config.Sandbox != wantSandbox {
		return ErrSessionConflict
	}
	return nil
}

// Refresh recalculates session diagnostics and reconfigures the harness
// (e.g. after a workspace reload), publishing a SessionUpdatedEvent.
func (s *Session) Refresh(ctx context.Context) {
	s.Lock()
	s.Diagnostics = s.modelDiagnostics(s.Config)
	if s.FileIndex != nil {
		s.FileIndex.SetAllowExec(s.workspace != nil && !s.workspace.IsRestricted())
		s.FileIndex.Invalidate()
	}
	s.Unlock()
	s.refreshSandboxPolicy(ctx)
	_ = s.configureHarness(ctx)
	s.publisher.Publish(api.TopicSessions, api.SessionUpdatedEvent{
		Workspace: s.workspace.RootDir(),
		Session:   ToAPISession(s),
	})
}

// refreshSandboxPolicy recomputes the kernel path policy (built-in floor plus
// the current sandbox.json overlay) and live-swaps it into the session's
// sandbox, so edits to sandbox.json apply to running sessions on the next
// workspace reload without recreating the sandbox. The backend itself is
// chosen at creation and never changes here.
func (s *Session) refreshSandboxPolicy(_ context.Context) {
	s.Lock()
	sbx := s.Sandbox
	cfgMgr := s.sandboxCfg
	workDir := s.WorkDir
	s.Unlock()
	if sbx == nil {
		return
	}
	policy := sandboxFilesystemPolicy(workDir)
	if cfgMgr != nil {
		if eff, err := cfgMgr.EffectiveFor(workDir); err == nil {
			policy = eff.Overlay(policy)
		} else {
			s.logger.Warn("session: refresh effective sandbox settings failed",
				log.String("session", s.ID), log.String("error", err.Error()))
		}
	}
	s.Lock()
	s.sbxPolicy = policy
	s.Unlock()
	sbx.SetFilesystemPolicy(policy)
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
	// The sandbox backend is chosen at session creation; swapping it would
	// mean replacing the live sandbox under std, tools, and running children.
	// A request that matches the effective backend (or the persisted request)
	// is an idempotent no-op, not a change.
	if wantSandbox := api.SandboxMode(strings.ToLower(strings.TrimSpace(string(req.Config.Sandbox)))); wantSandbox != "" {
		if !wantSandbox.IsValid() {
			s.Unlock()
			return fmt.Errorf("%w: unsupported sandbox backend %q", ErrInvalidConfig, req.Config.Sandbox)
		}
		var activeMode api.SandboxMode
		if s.Sandbox != nil {
			activeMode = s.Sandbox.Mode()
		}
		if wantSandbox != s.Config.Sandbox && wantSandbox != activeMode {
			s.Unlock()
			return fmt.Errorf("%w: a session's sandbox backend cannot be changed after creation", ErrInvalidConfig)
		}
	}
	oldConfig := s.Config
	newConfig := s.Config
	newTitle := s.Title
	thinkingCleared := false

	if title := strings.TrimSpace(req.Title); title != "" {
		newTitle = title
	}
	if agent := strings.TrimSpace(req.Config.Agent); agent != "" {
		newConfig.Agent = agent
		if s.ParentID == "" {
			if err := validateHumanAgentTrigger(s.workspace, agent); err != nil {
				s.Unlock()
				return err
			}
		}
	}
	cat := s.catalog()
	if model := models.ID(strings.TrimSpace(req.Config.Model)); model != "" {
		_, resolved := resolveAgentModel(s.workspace, cat, newConfig.Agent, model)
		if err := validateCompletionModel(cat, resolved); err != nil {
			s.Unlock()
			return err
		}
		newConfig.Model = string(resolved)
	} else if strings.TrimSpace(req.Config.Agent) != "" && s.Config.Model == "" {
		_, resolved := resolveAgentModel(s.workspace, cat, newConfig.Agent, "")
		if err := validateCompletionModel(cat, resolved); err != nil {
			s.Unlock()
			return err
		}
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
		if s.workspace != nil && s.workspace.IsRestricted() {
			newConfig.Mode = api.ModePlan
		} else {
			newConfig.Mode = req.Config.Mode
		}
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
		entries = append(entries, timeline.NewAgentEntry(api.AgentChange{From: oldConfig.Agent, To: newConfig.Agent}))
	}
	if oldConfig.Model != newConfig.Model {
		entries = append(entries, timeline.NewModelEntry(api.ModelChange{From: oldConfig.Model, To: newConfig.Model}))
	}
	if !reflect.DeepEqual(oldConfig.Thinking, newConfig.Thinking) {
		entries = append(entries, timeline.NewThinkingEntry(api.ThinkingChange{From: oldConfig.Thinking, To: newConfig.Thinking}))
	}
	if oldConfig.Mode != newConfig.Mode {
		entries = append(entries, timeline.NewModeEntry(api.ModeChange{From: oldConfig.Mode, To: newConfig.Mode}))
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
	for _, ch := range s.Children() {
		_ = ch.Close()
	}
	s.Lock()
	defer s.Unlock()
	if s.FileIndex != nil {
		_ = s.FileIndex.Close()
		s.FileIndex = nil
	}
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

// GetFileIndex returns the session's FileIndex, creating it lazily if needed.
func (s *Session) GetFileIndex() (*std.FileIndex, error) {
	s.Lock()
	defer s.Unlock()
	if s.FileIndex != nil {
		s.FileIndex.SetAllowExec(s.workspace != nil && !s.workspace.IsRestricted())
		return s.FileIndex, nil
	}
	if s.Std == nil {
		return nil, errors.New("session: std uninitialized")
	}
	idx, err := std.NewFileIndex(std.FileIndexOptions{
		Std:       s.Std,
		AllowExec: s.workspace != nil && !s.workspace.IsRestricted(),
		Lifecycle: s.bgCtx,
	})
	if err != nil {
		return nil, err
	}
	s.FileIndex = idx
	return idx, nil
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
	s.Context.Breakdown.Rules = staticCtx.Rules
	s.Unlock()

	return limits, staticCtx
}

// setConfigModel updates the session's configured model under the session lock.
// It is used to apply a per-turn model override and to restore the original
// model when the turn exits.
func (s *Session) setConfigModel(model string) {
	s.Lock()
	s.Config.Model = model
	s.Unlock()
}

func (s *Session) configureHarness(ctx context.Context) error {
	if s.Sandbox != nil && s.workspace != nil {
		s.Sandbox.SetEnv(s.workspace.SubprocessEnv())
		s.Sandbox.SetFilesystemPolicy(s.sbxPolicy)
		s.applyReadOnlyPolicy()
	}

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

	// Resolve the active agent once so both the permission ceiling and the rule
	// matcher can share it.
	cat := s.workspace.Catalog()
	var resolved *warp.ResolvedAgent
	if r, rerr := cat.ResolveAgent(agentName); rerr == nil && r != nil && r.Agent != nil {
		resolved = r
	}

	// Build the smart rule matcher for the active agent. A nil matcher (rule-free
	// agent) disables rule matching without a separate guard.
	var ruleMatcher *harness.RuleMatcher
	var renderGlobals map[string]any
	if resolved != nil {
		renderGlobals = cat.AgentRenderGlobals(resolved)
		ruleMatcher = harness.NewRuleMatcher(resolved, cat.WorkspaceSpec(), cat.Project(), renderGlobals)
	}

	// Propagate the active agent's tool policy ceiling to the permission gate so
	// that agent-level restrictions narrow the workspace ceiling. Reset the
	// ceiling unconditionally so that stale policies from a previously active
	// agent cannot leak across agent changes when ResolveAgent fails or returns
	// no agent.
	if s.PermManager != nil {
		var agentPolicies *warp.Policies
		var hasDeferred bool
		var hasConditionalRules bool
		var hasMCPResources bool
		if resolved != nil {
			agentPolicies = resolved.Agent.Spec.Policies
			if partition := cat.PartitionAgentTools(resolved); len(partition.Deferred) > 0 {
				hasDeferred = true
			}
			hasConditionalRules = cat.HasConditionalRules(resolved)
			hasMCPResources = cat.HasAllowedMCPResources()
		}
		// When deferred tools exist, tool_search is auto-activated as an eager
		// tool, and when conditional rules exist, load_rule is auto-activated as
		// an eager tool. Ensure the agent's policy ceiling permits each so the
		// permission gate allows them when the agent restricts tools via include.
		if agentPolicies != nil && agentPolicies.Tools != nil && len(agentPolicies.Tools.Include) > 0 {
			var sysTools []string
			if hasDeferred {
				sysTools = append(sysTools, builtin.ToolToolSearch)
			}
			if hasConditionalRules {
				sysTools = append(sysTools, builtin.ToolLoadRule)
			}
			// read_mcp_resource is likewise activated by the catalog once a server
			// offers a readable resource, which no agent author anticipated.
			if hasMCPResources {
				sysTools = append(sysTools, builtin.ToolReadMCPResource)
			}
			if len(sysTools) > 0 {
				agentPolicies = ensureSystemToolsAllowed(agentPolicies, sysTools...)
			}
		}
		s.PermManager.SetAgentPolicies(agentPolicies)
	}

	attachmentResolver, err := harness.NewAttachmentResolver(harness.AttachmentResolverConfig{
		AgentName:   agentName,
		Workspace:   s.workspace,
		Tools:       container,
		Rules:       ruleMatcher,
		Std:         s.Std,
		Sandbox:     s.Sandbox,
		Permissions: s.PermManager,
		WorkDir:     s.WorkDir,
		MCPCaller:   s.mcpCaller,
		Logger:      s.logger,
	})
	if err != nil {
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
		Rules:        ruleMatcher,
		Attachments:  attachmentResolver,
	}
	err = s.Harness.Configure(opts)

	s.Lock()
	s.Tools = container
	s.attachments = attachmentResolver
	s.harnessErr = err
	s.Unlock()

	return err
}

func (s *Session) outputsDir() string {
	return filepath.Join(s.Root, "outputs")
}

// ensureSystemToolsAllowed returns a deep copy of policies with the given system
// tool names added to the tool include ceiling (when the ceiling restricts tools
// via include), so that auto-activated system tools (tool_search, load_rule) are
// permitted by the permission gate.
func ensureSystemToolsAllowed(policies *warp.Policies, names ...string) *warp.Policies {
	clone := policies.DeepCopy()
	if clone.Tools == nil {
		clone.Tools = &warp.ToolPolicies{}
	}
	for _, want := range names {
		found := false
		for _, name := range clone.Tools.Include {
			if strings.EqualFold(name, want) {
				found = true
				break
			}
		}
		if !found {
			clone.Tools.Include = append(clone.Tools.Include, want)
		}
	}
	return clone
}

func (s *Session) buildToolsForAgent(agentName string) (tools.Container, error) {
	cat := s.workspace.Catalog()
	if agentName == "" {
		agentName = cat.DefaultAgentName()
	}

	var allowedToolNames []string
	var deferredToolNames []string
	var skills []warp.Skill
	var rules []warp.Rule
	var subagents []*warp.Agent
	var activeAgent *warp.Agent

	if agentName != "" {
		if resolved, err := cat.ResolveAgent(agentName); err == nil && resolved != nil {
			// An agent's resolved tool list is an explicit allowlist. A nil or
			// empty list (e.g. policies.tools deny ["*"] or allow ["nonexistent"])
			// grants zero tools rather than all tools.
			partition := cat.PartitionAgentTools(resolved)
			allowedToolNames = make([]string, 0, len(partition.Eager)+len(partition.Deferred))
			allowedToolNames = append(allowedToolNames, partition.Eager...)
			allowedToolNames = append(allowedToolNames, partition.Deferred...)
			deferredToolNames = partition.Deferred
			skills = resolved.Skills
			rules = resolved.Rules
			subagents = resolved.Subagents
			activeAgent = resolved.Agent
		} else {
			// No agent resolved (agentless workspace or unknown agent): fall back
			// to every effective workspace tool.
			for _, t := range cat.Tools() {
				allowedToolNames = append(allowedToolNames, t.Metadata.Name)
			}
		}
	}

	if s.permRoot != nil {
		if eff, err := s.permRoot.Effective(); err == nil {
			if len(eff.DisabledTools) > 0 || len(eff.DisabledMCPs) > 0 {
				disabledSet := make(map[string]struct{}, len(eff.DisabledTools))
				for _, dt := range eff.DisabledTools {
					disabledSet[dt] = struct{}{}
				}
				disabledMcps := make(map[string]struct{}, len(eff.DisabledMCPs))
				for _, dm := range eff.DisabledMCPs {
					disabledMcps[dm] = struct{}{}
				}
				filtered := allowedToolNames[:0]
				for _, name := range allowedToolNames {
					if _, ok := disabledSet[name]; ok {
						continue
					}
					parts := strings.SplitN(name, "__", 2)
					if len(parts) == 2 {
						if _, ok := disabledMcps[parts[0]]; ok {
							continue
						}
					}
					filtered = append(filtered, name)
				}
				allowedToolNames = filtered
			}
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
		WorkspaceDir: s.WorkDir,
		ReadDirs:     readDirs,
	})

	// The MCP runtime is resolved per build so a reload that replaced the manager is
	// picked up by the next container instead of leaving a session calling through a
	// closed one.
	var mcpCaller tools.MCPCaller
	if s.mcpCaller != nil {
		mcpCaller = s.mcpCaller()
	}

	builtinTools, err := builtin.New(builtin.Config{
		Std:           s.Std,
		Sandbox:       s.Sandbox,
		Guard:         guardInst,
		Tasks:         s.Tasks,
		Workspace:     s.workspace,
		AllowedTools:  allowedToolNames,
		DeferredTools: deferredToolNames,
		Skills:        skills,
		Rules:         rules,
		Subagents:     subagents,
		Agents:        s,
		AgentName:     agentName,
		Agent:         activeAgent,
		MCPCaller:     mcpCaller,
	})
	if err != nil {
		return nil, fmt.Errorf("session: initialize builtin tools: %w", err)
	}

	container, err := tools.NewContainer(tools.Config{
		Tools:         builtinTools,
		Sandbox:       s.Sandbox,
		Workspace:     s.workspace,
		Agent:         activeAgent,
		Logger:        s.logger,
		Spiller:       s.spiller,
		AllowedTools:  allowedToolNames,
		DeferredTools: deferredToolNames,
		MCPCaller:     mcpCaller,
		CommandRunner: s.Std,
		Tasks:         s.Tasks,
		ToolSearchFactory: func(defs []tool.Definition) tools.Tool {
			return builtin.NewToolSearch(defs, s.workspace)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("session: initialize tools container: %w", err)
	}

	return container, nil
}
