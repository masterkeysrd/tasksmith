package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/loom/auth"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/env"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
	"github.com/masterkeysrd/tasksmith/internal/core/sys"
	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/server"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

// Sentinel errors returned by the daemon package.
var (
	ErrInvalidConfig  = errors.New("daemon: invalid configuration")
	ErrAlreadyRunning = errors.New("daemon: server is already running")
	ErrSessionRunning = session.ErrSessionRunning
	ErrEmptyWorkspace = errors.New("daemon: workspace parameter is required")
	ErrEmptyMessage   = session.ErrEmptyMessage
)

// DefaultSocketPath returns the default XDG runtime Unix domain socket path ("brain.sock").
func DefaultSocketPath() (string, error) {
	return xdg.SocketPath("brain.sock")
}

// DefaultLockPath returns the default XDG runtime lockfile path ("brain.lock").
func DefaultLockPath() (string, error) {
	return xdg.SocketPath("brain.lock")
}

// Config holds the mandatory configuration parameters for starting a [Daemon].
type Config struct {
	SocketPath  string
	LockPath    string
	GatewayPool gateway.Pool
	Logger      log.Interface
	// DisableMCPAutoConnect keeps a workspace's MCP servers disconnected until an
	// MCP operation asks for them. It exists for tests, which must not launch the
	// commands or reach the endpoints a workspace's manifests declare.
	DisableMCPAutoConnect bool
}

// Validate checks that all required fields in [Config] are provided with zero
// silent fallbacks.
func (c Config) Validate() error {
	if strings.TrimSpace(c.SocketPath) == "" {
		return fmt.Errorf("%w: socket path is required", ErrInvalidConfig)
	}
	if strings.TrimSpace(c.LockPath) == "" {
		return fmt.Errorf("%w: lock path is required", ErrInvalidConfig)
	}
	if c.GatewayPool == nil {
		return fmt.Errorf("%w: gateway pool is required", ErrInvalidConfig)
	}
	if c.Logger == nil {
		return fmt.Errorf("%w: logger is required", ErrInvalidConfig)
	}
	return nil
}

// Daemon is the single-instance TaskSmith Brain Daemon HTTP+SSE server.
type Daemon struct {
	cfg        Config
	logger     log.Interface
	startedAt  time.Time
	broker     *broker.Broker
	srv        *server.Server
	httpServer *http.Server

	mu           sync.Mutex
	lockFile     *os.File
	listener     net.Listener
	started      bool
	closed       bool
	wsMu         sync.Mutex
	workspaces   map[string]*workspaceRuntime
	dirToRoot    map[string]string
	db           *sql.DB
	metricsStore *metrics.Store
	usage        *metrics.Live
	shutdownCh   chan struct{}
	shutdownOnce sync.Once
	closeOnce    sync.Once
	closeErr     error
	serveErrCh   chan error

	providerAuthMu sync.Mutex
	providerAuths  map[string]*providerAuthFlow
}

type providerAuthFlow struct {
	mu       sync.Mutex
	provider string
	status   string
	authURL  string
	account  string
	err      string
	cancel   context.CancelFunc
}

var _ server.Backend = (*Daemon)(nil)

// New validates cfg, initializes internal subsystems, creates the HTTP multiplexer,
// and constructs a new [Daemon].
func New(cfg Config) (*Daemon, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cfg.SocketPath = filepath.Clean(strings.TrimSpace(cfg.SocketPath))
	cfg.LockPath = filepath.Clean(strings.TrimSpace(cfg.LockPath))

	daemonLogger := cfg.Logger.ForComponent("daemon")
	serverLogger := cfg.Logger.ForComponent("server")

	b := broker.New(broker.WithTopicBufferSize(func(topic string) int {
		if topic == api.TopicSessions {
			return 128
		}
		if strings.HasSuffix(topic, ".stream") {
			return 1024
		}
		return 128
	}))

	dbPath, err := xdg.SubDataDir("daemon.db")
	if err != nil {
		return nil, fmt.Errorf("daemon: resolve database path: %w", err)
	}
	db, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		return nil, fmt.Errorf("daemon: open database: %w", err)
	}
	store, err := metrics.NewStore(context.Background(), db, daemonLogger)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	usage := metrics.NewLive(store, func(sessionID string, u api.SessionUsage) {
		b.Publish(api.SessionStreamTopic(sessionID), api.UsageUpdatedEvent{SessionID: sessionID, Usage: u})
	})

	d := &Daemon{
		db:            db,
		metricsStore:  store,
		usage:         usage,
		cfg:           cfg,
		logger:        daemonLogger,
		startedAt:     time.Now().UTC(),
		broker:        b,
		srv:           server.NewServer(serverLogger),
		workspaces:    make(map[string]*workspaceRuntime),
		dirToRoot:     make(map[string]string),
		providerAuths: make(map[string]*providerAuthFlow),
		shutdownCh:    make(chan struct{}),
		serveErrCh:    make(chan error, 1),
	}

	mux := d.srv.CreateMux(
		context.Background(),
		server.NewSystemRouter(d),
		server.NewEventRouter(d),
		server.NewWorkspaceRouter(d),
		server.NewMCPRouter(d),
		server.NewSessionRouter(d),
		server.NewMetricsRouter(d),
	)

	d.httpServer = &http.Server{
		Handler: mux,
	}

	return d, nil
}

// SocketPath returns the Unix domain socket path configured on the daemon.
func (d *Daemon) SocketPath() string {
	return d.cfg.SocketPath
}

// Start acquires the non-blocking exclusive flock on [Config.LockPath], binds
// the Unix domain socket at [Config.SocketPath], and begins serving HTTP+SSE
// requests in a background goroutine.
func (d *Daemon) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	if d.closed {
		return errors.New("daemon: server is closed")
	}
	if d.started {
		return nil
	}

	lockFile, err := sys.AcquireLock(d.cfg.LockPath)
	if err != nil {
		if errors.Is(err, sys.ErrLocked) {
			return ErrAlreadyRunning
		}
		return fmt.Errorf("%w: %v", ErrAlreadyRunning, err)
	}

	ln, err := sys.ListenUnix(ctx, d.cfg.SocketPath)
	if err != nil {
		_ = sys.ReleaseLock(lockFile)
		return err
	}

	d.lockFile = lockFile
	d.listener = ln
	d.started = true

	go func() {
		err := d.httpServer.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			d.serveErrCh <- err
			return
		}
		d.serveErrCh <- nil
	}()

	return nil
}

// Serve starts the daemon (if not already started) and blocks until ctx is
// canceled or a shutdown is requested via `/v1/shutdown` or [Daemon.Close].
func (d *Daemon) Serve(ctx context.Context) error {
	if err := d.Start(ctx); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return d.Close(context.Background())
	case <-d.shutdownCh:
		return d.Close(context.Background())
	case err := <-d.serveErrCh:
		_ = d.Close(context.Background())
		return err
	}
}

func (d *Daemon) signalShutdown() {
	d.shutdownOnce.Do(func() {
		close(d.shutdownCh)
	})
}

// Close gracefully shuts down the HTTP server, waits for any in-flight
// background turns to abort, closes all workspace runtimes and the gateway pool,
// removes the Unix socket, and releases the lockfile. Concurrent calls to Close
// block until teardown completes and return the same result.
func (d *Daemon) Close(ctx context.Context) error {
	d.signalShutdown()
	d.closeOnce.Do(func() {
		d.mu.Lock()
		d.closed = true

		ln := d.listener
		lockFile := d.lockFile
		d.listener = nil
		d.lockFile = nil
		d.mu.Unlock()

		var errs []error
		if ln != nil {
			shutCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			if err := d.httpServer.Shutdown(shutCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errs = append(errs, err)
			}
			cancel()
			_ = os.Remove(d.cfg.SocketPath)
		}

		if err := d.closeWorkspaces(); err != nil {
			errs = append(errs, err)
		}

		if err := d.cfg.GatewayPool.Close(); err != nil {
			errs = append(errs, err)
		}

		// Flush queued metrics before the database is closed.
		if err := d.metricsStore.Close(); err != nil {
			errs = append(errs, err)
		}
		if err := d.db.Close(); err != nil {
			errs = append(errs, err)
		}

		if lockFile != nil {
			if err := sys.ReleaseLock(lockFile); err != nil {
				errs = append(errs, err)
			}
		}

		d.closeErr = errors.Join(errs...)
	})
	return d.closeErr
}

// SystemBackend implementation

func (d *Daemon) Health(_ context.Context) (*api.HealthResponse, error) {
	return &api.HealthResponse{
		PID:        os.Getpid(),
		StartedAt:  d.startedAt,
		Workspaces: d.Workspaces(),
	}, nil
}

func (d *Daemon) Shutdown(_ context.Context) error {
	d.signalShutdown()
	return nil
}

// EventBackend implementation

func (d *Daemon) AddSubscriber(_ context.Context, subscriberID, since string, topics ...string) (*broker.Subscriber, []string, error) {
	subID := strings.TrimSpace(subscriberID)
	if subID == "" {
		subID = uuid.NewString()
	}

	var cleanTopics []string
	for _, t := range topics {
		t = strings.TrimSpace(t)
		if t != "" {
			cleanTopics = append(cleanTopics, t)
		}
	}

	sub := broker.NewSubscriber(subID)
	if err := d.broker.Add(sub); err != nil {
		return nil, nil, err
	}

	if len(cleanTopics) > 0 {
		if _, err := sub.SubscribeSince(since, cleanTopics...); err != nil {
			d.broker.Remove(sub)
			return nil, nil, err
		}
	}

	return sub, sub.Topics(), nil
}

func (d *Daemon) RemoveSubscriber(sub *broker.Subscriber) {
	if sub != nil {
		d.broker.Remove(sub)
	}
}

func (d *Daemon) Subscribe(_ context.Context, subscriberID, since string, topics ...string) ([]string, error) {
	subID := strings.TrimSpace(subscriberID)
	if subID == "" {
		return nil, &api.APIError{StatusCode: http.StatusBadRequest, Code: "bad_request", Message: "subscriber ID must not be empty"}
	}
	sub, ok := d.broker.Get(subID)
	if !ok {
		return nil, &api.APIError{StatusCode: http.StatusNotFound, Code: "not_found", Message: "subscriber not found"}
	}
	if _, err := sub.SubscribeSince(since, topics...); err != nil {
		return nil, err
	}
	return sub.Topics(), nil
}

func (d *Daemon) Unsubscribe(_ context.Context, subscriberID string, topics ...string) ([]string, error) {
	subID := strings.TrimSpace(subscriberID)
	if subID == "" {
		return nil, &api.APIError{StatusCode: http.StatusBadRequest, Code: "bad_request", Message: "subscriber ID must not be empty"}
	}
	sub, ok := d.broker.Get(subID)
	if !ok {
		return nil, &api.APIError{StatusCode: http.StatusNotFound, Code: "not_found", Message: "subscriber not found"}
	}
	sub.Unsubscribe(topics...)
	return sub.Topics(), nil
}

func (d *Daemon) Done() <-chan struct{} {
	return d.shutdownCh
}

// WorkspaceBackend implementation

func (d *Daemon) GetWorkspace(ctx context.Context, rawWS string) (*api.Workspace, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	owner := rt.SessionRuntime()
	var resolvedDefault *api.Model
	cat := owner.Workspace().Catalog()
	defaultAgent := cat.DefaultAgentName()
	var candidates []string
	if resolvedAgent, err := cat.ResolveAgent(defaultAgent); err == nil && resolvedAgent != nil && resolvedAgent.Agent != nil {
		candidates = resolvedAgent.Agent.Spec.Models
	}
	if model, ok := owner.Catalog().Resolve(models.RoleCompletion, candidates...); ok {
		converted := model.ToAPI()
		var agentThinking *api.ThinkingSetting
		if ag, ok := cat.GetAgent(defaultAgent); ok {
			agentThinking = ag.Thinking
		}
		converted.ThinkingDefault = model.EffectiveThinking(nil, agentThinking)
		resolvedDefault = &converted
	}

	cleanDir, _ := fsutils.CleanPath(rawWS)

	// Workspace warnings are the durable, startup-time problems a user has to fix
	// before the session is useful. MCP server health is not one of them: it changes
	// while the session runs, so the CLI reports it in the timeline as the listing is
	// polled. See store.SetWorkspaceMCPServers.
	var warnings []string
	if wsWarnings := rt.Workspace().Catalog().Warnings(); len(wsWarnings) > 0 {
		warnings = append(warnings, wsWarnings...)
	}
	if gw := rt.Gateway(); gw != nil {
		for _, p := range gw.ListProviders() {
			if p.Warning != "" {
				warnings = append(warnings, p.Warning)
			}
		}
	}

	return &api.Workspace{
		Name:         rt.Workspace().Name(),
		RootDir:      rt.Workspace().RootDir(),
		CWD:          cleanDir,
		DefaultAgent: owner.Workspace().Catalog().DefaultAgentName(),
		DefaultModel: resolvedDefault,
		Warnings:     warnings,
		IsRestricted: rt.Workspace().IsRestricted(),
	}, nil
}

func (d *Daemon) ReloadWorkspace(ctx context.Context, rawWS string) error {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return err
	}
	return rt.Reload(ctx)
}

func (d *Daemon) ListAgents(ctx context.Context, rawWS string) ([]api.Agent, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	cat := rt.Workspace().Catalog()
	agents := cat.Agents()
	out := make([]api.Agent, len(agents))
	for i, ag := range agents {
		out[i] = ag.Agent
	}
	return out, nil
}

func (d *Daemon) GetAgent(ctx context.Context, rawWS, name string) (*api.Agent, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	name = strings.TrimSpace(name)
	cat := rt.Workspace().Catalog()
	ag, ok := cat.GetAgent(name)
	if !ok {
		return nil, &api.APIError{
			StatusCode: http.StatusNotFound,
			Code:       "not_found",
			Message:    fmt.Sprintf("agent %q not found", name),
		}
	}

	res := ag.Agent
	return &res, nil
}

func (d *Daemon) ListCommands(ctx context.Context, rawWS string) ([]api.Command, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	commands := rt.Workspace().Catalog().Commands()
	out := make([]api.Command, len(commands))
	for i, cmd := range commands {
		out[i] = cmd.Command
	}
	return out, nil
}

func (d *Daemon) GetCommand(ctx context.Context, rawWS, name string) (*api.Command, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	name = strings.TrimSpace(name)
	cmd, ok := rt.Workspace().Catalog().GetCommand(name)
	if !ok {
		return nil, &api.APIError{
			StatusCode: http.StatusNotFound,
			Code:       "not_found",
			Message:    fmt.Sprintf("command %q not found", name),
		}
	}

	res := cmd.Command
	return &res, nil
}

func (d *Daemon) ListSkills(ctx context.Context, rawWS string) ([]api.Skill, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	cat := rt.Workspace().Catalog()
	skills := cat.Skills()
	out := make([]api.Skill, len(skills))
	for i, sk := range skills {
		out[i] = sk.Skill
	}
	return out, nil
}

func (d *Daemon) GetSkill(ctx context.Context, rawWS, name string) (*api.Skill, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	name = strings.TrimSpace(name)
	cat := rt.Workspace().Catalog()
	sk, ok := cat.GetSkill(name)
	if !ok {
		return nil, &api.APIError{
			StatusCode: http.StatusNotFound,
			Code:       "not_found",
			Message:    fmt.Sprintf("skill %q not found", name),
		}
	}

	res := sk.Skill
	return &res, nil
}

func (d *Daemon) ListRules(ctx context.Context, rawWS string) ([]api.Rule, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	ws := rt.Workspace()
	rules := ws.Catalog().Rules()
	out := make([]api.Rule, len(rules))
	for i, rl := range rules {
		r := rl.Rule
		r.Path = ws.PortablePath(rl.Path)
		out[i] = r
	}
	return out, nil
}

// ListTools returns every effective tool in the workspace. When agent is not
// empty, each tool is also classified with its availability for that agent
// (eager, deferred, denied, or inactive).
func (d *Daemon) ListTools(ctx context.Context, rawWS, agent string) ([]api.Tool, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	cat := rt.Workspace().Catalog()
	snapshots := cat.ToolSnapshots()
	// Tools discovered on an MCP server but prohibited by its policy are never
	// registered as callable, so they are listed separately with a denied status.
	denied := cat.DeniedMCPToolSnapshots()
	out := make([]api.Tool, 0, len(snapshots)+len(denied))
	for _, snap := range snapshots {
		out = append(out, snap.Tool)
	}
	for _, snap := range denied {
		out = append(out, snap.Tool)
	}
	slices.SortFunc(out, func(a, b api.Tool) int { return strings.Compare(a.Name, b.Name) })

	agent = strings.TrimSpace(agent)
	if agent == "" {
		return out, nil
	}
	resolved, err := cat.ResolveAgent(agent)
	if err != nil {
		return nil, &api.APIError{
			StatusCode: http.StatusNotFound,
			Code:       "not_found",
			Message:    fmt.Sprintf("agent %q not found", agent),
		}
	}
	statuses := cat.ToolStatuses(resolved)
	for i := range out {
		if status, ok := statuses[out[i].Name]; ok {
			out[i].Status = status
		}
	}
	return out, nil
}

// ListWorkspaceFiles searches and ranks workspace files and directories for context mentions.
func (d *Daemon) ListWorkspaceFiles(ctx context.Context, rawWS string, req api.ListFilesRequest) ([]api.WorkspaceFile, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	var idx *std.FileIndex
	var recentPaths []string
	var sessPerms permissions.SessionManager

	workDir := rt.Dir()

	if strings.TrimSpace(req.SessionID) != "" {
		sess, err := rt.getLiveSession(ctx, req.SessionID)
		if err == nil && sess != nil {
			idx, err = sess.GetFileIndex()
			if err != nil {
				return nil, err
			}
			if sess.Journal != nil {
				recentPaths = sess.Journal.RecentPaths()
			}
			sessPerms = sess.PermManager
			if sess.WorkDir != "" {
				workDir = sess.WorkDir
			}
		}
	}

	if idx == nil {
		idx, err = rt.getFileIndex(ctx)
		if err != nil {
			return nil, err
		}
	}

	files, err := idx.Search(ctx, std.SearchOptions{
		Query:         req.Query,
		Limit:         req.Limit,
		RecentPaths:   recentPaths,
		IncludeHidden: req.IncludeHidden,
	})
	if err != nil {
		return nil, err
	}

	checker := permissions.NewFileAccessChecker(permissions.FileAccessConfig{
		WorkDir:   workDir,
		Workspace: rt.Workspace(),
		Manager:   rt.perms,
		Session:   sessPerms,
	})
	for i := range files {
		files[i].Allowed = checker.Check(files[i].Path, files[i].Path, files[i].IsDir) == nil
	}

	return files, nil
}

func (d *Daemon) ListModels(ctx context.Context, rawWS string) ([]api.Model, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	listed := rt.Catalog().List()
	out := make([]api.Model, 0, len(listed))
	for _, m := range listed {
		out = append(out, m.ToAPI())
	}
	return out, nil
}

func (d *Daemon) GetModel(ctx context.Context, rawWS, id string) (*api.Model, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	id = strings.TrimSpace(id)
	if id == "" {
		return nil, &api.APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "bad_request",
			Message:    "model ID must not be empty",
		}
	}

	var model models.Model
	var ok bool
	if models.ID(id).Valid() {
		model, ok = rt.Catalog().Get(models.ID(id))
	} else {
		model, ok = rt.Catalog().Resolve(models.RoleCompletion, id)
	}
	if !ok {
		return &api.Model{ID: id, Name: id, Label: id}, nil
	}
	m := model.ToAPI()
	return &m, nil
}

func (d *Daemon) GetPermissions(ctx context.Context, rawWS string) (*api.PermissionsResponse, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	proj, err := rt.Permissions().Get(api.ScopeProject)
	if err != nil {
		return nil, err
	}
	glob, err := rt.Permissions().Get(api.ScopeGlobal)
	if err != nil {
		return nil, err
	}

	return &api.PermissionsResponse{
		Project: proj,
		Global:  glob,
	}, nil
}

func (d *Daemon) AddPermissionRule(ctx context.Context, rawWS string, req api.PermissionRuleRequest) (*api.PermissionsResponse, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	if err := rt.Permissions().AddRule(req.Scope, req.Effect, req.Rule); err != nil {
		return nil, err
	}

	return d.GetPermissions(ctx, rawWS)
}

func (d *Daemon) RemovePermissionRule(ctx context.Context, rawWS string, req api.PermissionRuleRequest) (*api.PermissionsResponse, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	if err := rt.Permissions().RemoveRule(req.Scope, req.Effect, req.Rule); err != nil {
		return nil, err
	}

	return d.GetPermissions(ctx, rawWS)
}

func (d *Daemon) GetTrustStatus(ctx context.Context, rawWS string) (*api.TrustStatus, error) {
	cleanDir, err := fsutils.CleanPath(rawWS)
	if err != nil {
		return nil, err
	}

	isTrusted, fpChanged, err := workspace.IsTrusted(cleanDir)
	if err != nil {
		return nil, err
	}

	summary, err := workspace.ScanExecutableConfig(cleanDir)
	if err != nil {
		return nil, err
	}

	return &api.TrustStatus{
		Path:               cleanDir,
		IsTrusted:          isTrusted,
		FingerprintChanged: fpChanged,
		Executables: api.ExecutableConfigSummary{
			MCPServers:  summary.MCPServers,
			CustomTools: summary.CustomTools,
			HasEnv:      summary.HasEnv,
		},
	}, nil
}

func (d *Daemon) TrustWorkspace(ctx context.Context, req api.TrustFolderRequest) error {
	cleanDir, err := fsutils.CleanPath(req.Path)
	if err != nil {
		return err
	}
	scope := workspace.TrustScope(req.Scope)
	if scope == "" {
		scope = workspace.TrustScopeFolder
	}
	return workspace.TrustFolder(cleanDir, scope)
}

func (d *Daemon) RevokeTrust(ctx context.Context, rawWS string) error {
	cleanDir, err := fsutils.CleanPath(rawWS)
	if err != nil {
		return err
	}
	return workspace.RevokeTrust(cleanDir)
}

func (d *Daemon) GetWorkspaceStatus(ctx context.Context, rawWS string) (*api.WorkspaceStatus, error) {
	cleanDir, err := fsutils.CleanPath(rawWS)
	if err != nil {
		return nil, err
	}

	isTrusted, fpChanged, err := workspace.IsTrusted(cleanDir)
	if err != nil {
		return nil, err
	}

	summary, err := workspace.ScanExecutableConfig(cleanDir)
	if err != nil {
		return nil, err
	}

	isConfigured := workspace.IsConfigured(cleanDir)
	_, errAgents := os.Stat(filepath.Join(cleanDir, "AGENTS.md"))
	hasAgentsMD := (errAgents == nil)

	globalProviders := d.scanGlobalProviders()
	globalMCPs := d.scanGlobalMCPs()

	var missingCredentials []string
	if isConfigured {
		missingCredentials = d.checkMissingCredentials(cleanDir)
	}

	needsOnboarding := (!isTrusted || fpChanged || !isConfigured || len(missingCredentials) > 0)

	return &api.WorkspaceStatus{
		Path:               cleanDir,
		IsTrusted:          isTrusted,
		FingerprintChanged: fpChanged,
		IsConfigured:       isConfigured,
		HasAgentsMD:        hasAgentsMD,
		Executables: api.ExecutableConfigSummary{
			MCPServers:  summary.MCPServers,
			CustomTools: summary.CustomTools,
			HasEnv:      summary.HasEnv,
		},
		GlobalProviders:    globalProviders,
		GlobalMCPs:         globalMCPs,
		MissingCredentials: missingCredentials,
		NeedsOnboarding:    needsOnboarding,
	}, nil
}

func (d *Daemon) scanGlobalProviders() []api.ProviderSummary {
	var userEnv map[string]string
	if cfgDir, err := xdg.SubConfigDir(); err == nil {
		userEnvPath := filepath.Join(cfgDir, ".env")
		userEnv, _ = env.LoadFile(userEnvPath)
	}

	presets := workspace.Presets()
	var summaries []api.ProviderSummary

	for _, p := range presets {
		authEnv := ""
		if p.Spec.Auth != nil && p.Spec.Auth.Env != "" {
			authEnv = p.Spec.Auth.Env
		}

		authType := "env"
		if p.Spec.Auth != nil && p.Spec.Auth.Type != "" {
			authType = string(p.Spec.Auth.Type)
		}

		hasKey := false
		account := ""
		status := ""

		if authType == "oauth2" {
			if tok, accountEmail, err := gateway.LoadProviderToken(p.Metadata.Name); err == nil && tok != nil {
				hasKey = true
				account = accountEmail
				if accountEmail != "" {
					status = fmt.Sprintf("✓ %s (OAuth)", accountEmail)
				} else {
					status = "✓ OAuth authenticated"
				}
			}
		}

		if !hasKey && authEnv != "" {
			val := os.Getenv(authEnv)
			if strings.TrimSpace(val) == "" && userEnv != nil {
				val = userEnv[authEnv]
			}
			hasKey = strings.TrimSpace(val) != ""
			if hasKey {
				status = fmt.Sprintf("✓ %s detected", authEnv)
			}
		}

		if p.Metadata.Name == "ollama" {
			status = "local"
		}

		var modelNames []string
		for _, m := range p.Spec.Models {
			modelNames = append(modelNames, m.ID)
		}

		defaultModel := ""
		if p.Spec.DefaultModels != nil {
			defaultModel = p.Spec.DefaultModels.Completion
		}

		summaries = append(summaries, api.ProviderSummary{
			Name:         p.Metadata.Name,
			DisplayName:  p.Metadata.Name,
			Status:       status,
			HasKey:       hasKey,
			AuthType:     authType,
			AuthEnv:      authEnv,
			Account:      account,
			DefaultModel: defaultModel,
			Models:       modelNames,
			Scope:        "system",
		})
	}

	return summaries
}

func (d *Daemon) scanGlobalMCPs() []string {
	var mcps []string
	if userMcpsDir, err := xdg.SubConfigDir("agents/mcps"); err == nil {
		if entries, err := os.ReadDir(userMcpsDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				if strings.HasSuffix(name, ".mcp.yaml") || strings.HasSuffix(name, ".yaml") {
					cleanName := strings.TrimSuffix(strings.TrimSuffix(name, ".mcp.yaml"), ".yaml")
					mcps = append(mcps, cleanName)
				}
			}
		}
	}
	sort.Strings(mcps)
	return mcps
}

func (d *Daemon) checkMissingCredentials(cleanDir string) []string {
	var missing []string
	localEnvPath := filepath.Join(cleanDir, ".env")
	localEnv, _ := env.LoadFile(localEnvPath)

	var userEnv map[string]string
	if cfgDir, err := xdg.SubConfigDir(); err == nil {
		userEnvPath := filepath.Join(cfgDir, ".env")
		userEnv, _ = env.LoadFile(userEnvPath)
	}

	wsFile := filepath.Join(cleanDir, warp.WorkspaceFileName)
	if data, err := os.ReadFile(wsFile); err == nil {
		res, err := warp.Parse(wsFile, string(data))
		if err == nil {
			if wsDef, ok := res.Resource.(*warp.WorkspaceDef); ok && wsDef.Spec.DefaultProvider != "" {
				provName := wsDef.Spec.DefaultProvider
				preset := workspace.Presets()
				for _, p := range preset {
					if p.Metadata.Name == provName {
						if p.Spec.Auth != nil && p.Spec.Auth.Type == "oauth2" {
							if tok, _, err := gateway.LoadProviderToken(provName); err == nil && tok != nil {
								break
							}
						}
						if p.Spec.Auth != nil && p.Spec.Auth.Env != "" {
							envVar := p.Spec.Auth.Env
							val := os.Getenv(envVar)
							if strings.TrimSpace(val) == "" && localEnv != nil {
								val = localEnv[envVar]
							}
							if strings.TrimSpace(val) == "" && userEnv != nil {
								val = userEnv[envVar]
							}
							if strings.TrimSpace(val) == "" {
								missing = append(missing, envVar)
							}
						}
						break
					}
				}
			}
		}
	}

	return missing
}

func (d *Daemon) InitWorkspace(ctx context.Context, req api.InitWorkspaceRequest) error {
	cleanDir, err := fsutils.CleanPath(req.CWD)
	if err != nil {
		return err
	}

	var defaultModels map[warp.ModelRole]string
	if len(req.DefaultModels) > 0 {
		defaultModels = make(map[warp.ModelRole]string)
		for k, v := range req.DefaultModels {
			defaultModels[warp.ModelRole(k)] = v
		}
	}

	opts := workspace.InitOptions{
		Scope:         req.Scope,
		ProjectName:   req.ProjectName,
		Provider:      req.Provider,
		APIKey:        req.APIKey,
		Endpoint:      req.Endpoint,
		AuthType:      req.AuthType,
		AuthEnv:       req.AuthEnv,
		DefaultModels: defaultModels,
		DisabledTools: req.DisabledTools,
		DisabledMCPs:  req.DisabledMCPs,
	}

	if err := workspace.Initialize(ctx, cleanDir, opts); err != nil {
		return err
	}

	if req.DefaultMode != "" || len(req.DisabledTools) > 0 || len(req.DisabledMCPs) > 0 {
		if req.Scope == "user" {
			_ = permissions.UpdateGlobal(req.DefaultMode, req.DisabledTools, req.DisabledMCPs)
		} else {
			if req.DefaultMode != "" || len(req.DisabledMCPs) > 0 {
				_ = permissions.UpdateGlobal(req.DefaultMode, nil, req.DisabledMCPs)
			}
		}
	}

	d.reloadAllWorkspaces(ctx)
	return nil
}

func (d *Daemon) VerifyProvider(ctx context.Context, req api.VerifyProviderRequest) (*api.VerifyProviderResponse, error) {
	provName := strings.TrimSpace(req.Provider)
	if provName == "" {
		return &api.VerifyProviderResponse{
			Valid:        false,
			ErrorMessage: "provider name is required",
		}, nil
	}

	presets := workspace.Presets()
	var foundPreset *warp.ModelProvider
	for _, p := range presets {
		if p.Metadata.Name == provName {
			foundPreset = p
			break
		}
	}

	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" && req.AuthEnv != "" {
		apiKey = strings.TrimSpace(os.Getenv(req.AuthEnv))
	}
	if apiKey == "" && foundPreset != nil && foundPreset.Spec.Auth != nil && foundPreset.Spec.Auth.Env != "" {
		apiKey = strings.TrimSpace(os.Getenv(foundPreset.Spec.Auth.Env))
	}

	if provName == "ollama" {
		endpoint := req.Endpoint
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(endpoint + "/api/tags")
		if err != nil {
			return &api.VerifyProviderResponse{
				Valid:        false,
				ErrorMessage: fmt.Sprintf("failed to reach Ollama at %s: %v", endpoint, err),
			}, nil
		}
		_ = resp.Body.Close()
		return &api.VerifyProviderResponse{
			Valid:       true,
			ModelsCount: 1,
		}, nil
	}

	if foundPreset != nil && foundPreset.Spec.Auth != nil && foundPreset.Spec.Auth.Type == "oauth2" {
		modelsCount := 1
		if len(foundPreset.Spec.Models) > 0 {
			modelsCount = len(foundPreset.Spec.Models)
		}
		if apiKey != "" {
			return &api.VerifyProviderResponse{
				Valid:       true,
				ModelsCount: modelsCount,
			}, nil
		}
		if tok, _, err := gateway.LoadProviderToken(provName); err == nil && tok != nil {
			return &api.VerifyProviderResponse{
				Valid:       true,
				ModelsCount: modelsCount,
			}, nil
		}
		envFallback := ""
		if foundPreset.Spec.Auth.Env != "" {
			envFallback = fmt.Sprintf(" (or %s)", foundPreset.Spec.Auth.Env)
		}
		return &api.VerifyProviderResponse{
			Valid:        false,
			ErrorMessage: fmt.Sprintf("%s requires OAuth authentication%s", provName, envFallback),
		}, nil
	}

	if apiKey == "" && (foundPreset != nil && foundPreset.Spec.Auth != nil && foundPreset.Spec.Auth.Type != "none") {
		return &api.VerifyProviderResponse{
			Valid:        false,
			ErrorMessage: fmt.Sprintf("API key or token is required for %s", provName),
		}, nil
	}

	modelsCount := 1
	if foundPreset != nil && len(foundPreset.Spec.Models) > 0 {
		modelsCount = len(foundPreset.Spec.Models)
	}

	return &api.VerifyProviderResponse{
		Valid:       true,
		ModelsCount: modelsCount,
	}, nil
}

func (d *Daemon) StartProviderAuth(ctx context.Context, provider string) (*api.ProviderAuthResponse, error) {
	prov := strings.TrimSpace(strings.ToLower(provider))
	driver, ok := gateway.GetOAuthDriver(prov)
	if !ok {
		return nil, fmt.Errorf("oauth not supported for provider %q", provider)
	}

	d.providerAuthMu.Lock()
	if existing, ok := d.providerAuths[prov]; ok && existing != nil {
		existing.mu.Lock()
		if existing.cancel != nil {
			existing.cancel()
		}
		existing.mu.Unlock()
	}

	flowCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	flow := &providerAuthFlow{
		provider: prov,
		status:   "authorizing",
		cancel:   cancel,
	}
	d.providerAuths[prov] = flow
	d.providerAuthMu.Unlock()

	oauthCfg := driver.OAuthConfig()
	session, err := auth.BeginAuth(oauthCfg)
	if err != nil {
		flow.mu.Lock()
		flow.status = "error"
		flow.err = err.Error()
		flow.mu.Unlock()
		return nil, fmt.Errorf("begin oauth: %w", err)
	}

	flow.mu.Lock()
	flow.authURL = session.AuthURL
	flow.mu.Unlock()

	go func() {
		defer cancel()
		res, err := auth.ReceiveCallback(flowCtx, oauthCfg.RedirectURL, session.AuthURL, auth.LoopbackOptions{
			Template: oauthCallbackTemplate,
		})
		if err != nil {
			flow.mu.Lock()
			flow.status = "error"
			flow.err = err.Error()
			flow.mu.Unlock()
			return
		}

		tok, err := auth.CompleteAuth(flowCtx, oauthCfg, session, res.Code, res.State)
		if err != nil {
			flow.mu.Lock()
			flow.status = "error"
			flow.err = err.Error()
			flow.mu.Unlock()
			return
		}

		account := driver.ExtractAccount(tok)
		if err := gateway.SaveProviderToken(prov, tok, account); err != nil {
			flow.mu.Lock()
			flow.status = "error"
			flow.err = err.Error()
			flow.mu.Unlock()
			return
		}

		d.reloadAllWorkspaces(flowCtx)

		flow.mu.Lock()
		flow.status = "authenticated"
		flow.account = account
		flow.mu.Unlock()
	}()

	return &api.ProviderAuthResponse{
		Provider: prov,
		AuthURL:  session.AuthURL,
		Status:   "authorizing",
	}, nil
}

func (d *Daemon) GetProviderAuthStatus(ctx context.Context, provider string) (*api.ProviderAuthResponse, error) {
	prov := strings.TrimSpace(strings.ToLower(provider))
	if !gateway.HasOAuthDriver(prov) {
		return nil, fmt.Errorf("oauth not supported for provider %q", provider)
	}

	d.providerAuthMu.Lock()
	flow, ok := d.providerAuths[prov]
	d.providerAuthMu.Unlock()

	if ok && flow != nil {
		flow.mu.Lock()
		status := flow.status
		authURL := flow.authURL
		account := flow.account
		errMsg := flow.err
		flow.mu.Unlock()

		if status == "authorizing" || status == "error" || (status == "authenticated" && errMsg == "") {
			return &api.ProviderAuthResponse{
				Provider: prov,
				AuthURL:  authURL,
				Status:   status,
				Account:  account,
				Error:    errMsg,
			}, nil
		}
	}

	if tok, email, err := gateway.LoadProviderToken(prov); err == nil && tok != nil {
		return &api.ProviderAuthResponse{
			Provider: prov,
			Status:   "authenticated",
			Account:  email,
		}, nil
	}

	return &api.ProviderAuthResponse{
		Provider: prov,
		Status:   "idle",
	}, nil
}

// SessionBackend implementation

func (d *Daemon) CreateSession(ctx context.Context, rawWS string, req api.CreateSessionRequest) (*api.Session, bool, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, false, err
	}

	sess, created, err := rt.CreateSession(ctx, req)
	if err != nil {
		return nil, false, err
	}

	out := session.ToAPISession(sess)
	return &out, created, nil
}

func (d *Daemon) PreviewSession(ctx context.Context, rawWS string, req api.PreviewSessionRequest) (*api.SessionPreview, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	cfg, model, diags, hasModel, err := rt.PreviewSession(ctx, req.Config)
	if err != nil {
		return nil, err
	}

	var resolvedModel *api.Model
	if hasModel {
		converted := model.ToAPI()
		var agentThinking *api.ThinkingSetting
		if ag, ok := rt.SessionRuntime().Workspace().Catalog().GetAgent(cfg.Agent); ok {
			agentThinking = ag.Thinking
		}
		converted.ThinkingDefault = model.EffectiveThinking(cfg.Thinking, agentThinking)
		resolvedModel = &converted
	}

	return &api.SessionPreview{
		Config:      cfg,
		Model:       resolvedModel,
		Diagnostics: diags,
	}, nil
}

func (d *Daemon) ListSessions(ctx context.Context, rawWS string) ([]api.Session, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	list, err := rt.ListSessions(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]api.Session, len(list))
	for i, s := range list {
		out[i] = session.ToAPISession(s)
	}
	return out, nil
}

func (d *Daemon) GetSession(ctx context.Context, rawWS, id string) (*api.Session, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	sess, err := rt.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}

	out := session.ToAPISession(sess)
	return &out, nil
}

func (d *Daemon) UpdateSession(ctx context.Context, rawWS, id string, req api.UpdateSessionRequest) (*api.Session, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	sess, err := rt.UpdateSession(ctx, id, req)
	if err != nil {
		return nil, err
	}

	out := session.ToAPISession(sess)
	return &out, nil
}

func (d *Daemon) DeleteSession(ctx context.Context, rawWS, id string) error {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return err
	}

	return rt.DeleteSession(ctx, id)
}

func (d *Daemon) Timeline(ctx context.Context, rawWS, id string) ([]api.TimelineEntry, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	sess, err := rt.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}

	entries, err := sess.Timeline.List(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]api.TimelineEntry, len(entries))
	for i, e := range entries {
		apiEntry, err := session.ToAPITimelineEntry(e)
		if err != nil {
			return nil, err
		}
		out[i] = apiEntry
	}
	return out, nil
}

func (d *Daemon) Send(ctx context.Context, rawWS, id string, req api.SendRequest) error {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return err
	}

	sess, err := rt.GetSession(ctx, id)
	if err != nil {
		return err
	}

	if err := sess.Send(ctx, req); err != nil {
		if errors.Is(err, session.ErrInvalidConfig) || errors.Is(err, session.ErrNilMessage) || errors.Is(err, session.ErrEmptyMessage) || strings.Contains(err.Error(), "required argument") || strings.Contains(err.Error(), "failed to render") {
			return &api.APIError{
				StatusCode: http.StatusBadRequest,
				Code:       "bad_request",
				Message:    err.Error(),
			}
		}
		return err
	}
	return nil
}

func (d *Daemon) Cancel(ctx context.Context, rawWS, id string) error {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return err
	}

	sess, err := rt.GetSession(ctx, id)
	if err != nil {
		return err
	}

	return sess.Cancel(ctx)
}

func (d *Daemon) Changes(ctx context.Context, rawWS, id string) ([]api.FileChange, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	sess, err := rt.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}

	return sess.Journal.Changes(ctx)
}

func (d *Daemon) Diff(ctx context.Context, rawWS, id, path, entryID string) (*api.DiffResponse, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	path = strings.TrimSpace(path)
	entryID = strings.TrimSpace(entryID)
	if path == "" {
		return nil, &api.APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "bad_request",
			Message:    "path query parameter is required",
		}
	}

	sess, err := rt.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}

	diffText, err := sess.Journal.DiffEntry(ctx, path, entryID)
	if err != nil {
		return nil, err
	}

	return &api.DiffResponse{
		Path:    path,
		EntryID: entryID,
		Diff:    diffText,
	}, nil
}

func (d *Daemon) Revert(ctx context.Context, rawWS, id string, req api.RevertRequest) error {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return err
	}

	sess, err := rt.GetSession(ctx, id)
	if err != nil {
		return err
	}

	path := strings.TrimSpace(req.Path)
	entryID := strings.TrimSpace(req.EntryID)

	if path != "" && entryID != "" {
		return sess.Journal.RecoverAt(ctx, sess.Sandbox.FS(), path, entryID, req.Before)
	}

	if path != "" {
		return sess.Journal.RevertToBaseline(ctx, sess.Sandbox.FS(), path, req.Force)
	}

	return sess.Journal.RevertSession(ctx, sess.Sandbox.FS(), req.Force)
}

func (d *Daemon) Tasks(ctx context.Context, rawWS, id string) ([]api.Task, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	sess, err := rt.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}

	return sess.Tasks.List(), nil
}

func (d *Daemon) KillTask(ctx context.Context, rawWS, id, taskID string) error {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return err
	}

	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return &api.APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "bad_request",
			Message:    "task ID must not be empty",
		}
	}

	sess, err := rt.GetSession(ctx, id)
	if err != nil {
		return err
	}

	t, ok := sess.Tasks.Get(taskID)
	if !ok {
		return &api.APIError{
			StatusCode: http.StatusNotFound,
			Code:       "not_found",
			Message:    fmt.Sprintf("task %q not found", taskID),
		}
	}

	if err := t.Kill(); err != nil {
		if errors.Is(err, tasks.ErrNotRunning) {
			return &api.APIError{
				StatusCode: http.StatusConflict,
				Code:       "conflict",
				Message:    err.Error(),
			}
		}
		return err
	}
	return nil
}

func (d *Daemon) StreamTaskOutput(ctx context.Context, rawWS, id, taskID string) (<-chan string, string, func(), error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, "", nil, err
	}

	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, "", nil, &api.APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "bad_request",
			Message:    "task ID must not be empty",
		}
	}

	sess, err := rt.GetSession(ctx, id)
	if err != nil {
		return nil, "", nil, err
	}

	t, ok := sess.Tasks.Get(taskID)
	if !ok {
		return nil, "", nil, &api.APIError{
			StatusCode: http.StatusNotFound,
			Code:       "not_found",
			Message:    fmt.Sprintf("task %q not found", taskID),
		}
	}

	ch, initial, cancel := t.SubscribeOutput()
	return ch, initial, cancel, nil
}

// FormatEvent converts an internal broker message into a public API event.
// Sessions publish [api.EventPayload] values directly, so this only needs to
// wrap the payload with the message envelope.
func (d *Daemon) FormatEvent(msg broker.Message) (api.Event, error) {
	switch ev := msg.Event.(type) {
	case api.EventPayload:
		return api.Event{
			ID:      msg.ID,
			Type:    api.EventType(ev.EventType()),
			Topic:   msg.Topic,
			Payload: ev,
		}, nil

	default:
		return api.Event{ID: msg.ID}, fmt.Errorf("daemon: unknown broker event type %T", msg.Event)
	}
}
