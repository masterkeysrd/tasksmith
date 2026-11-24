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

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
	"github.com/masterkeysrd/tasksmith/internal/core/sys"
	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/server"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/tasksmith/internal/version"
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
	return client.DefaultSocketPath()
}

// DefaultLockPath returns the default XDG runtime lockfile path ("brain.lock").
func DefaultLockPath() (string, error) {
	return client.DefaultLockPath()
}

// LockPathForSocket returns the lockfile path paired with a socket path: the
// same directory and base name with a ".lock" extension. A socket that does not
// end in ".sock" gets ".lock" appended.
func LockPathForSocket(socketPath string) string {
	return client.LockPathForSocket(socketPath)
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

	providerInstallMu  sync.Mutex
	providerAuthMu     sync.Mutex
	providerAuths      map[string]*providerAuthFlow
	browserAuthWriteMu map[string]*sync.Mutex
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
		db:                 db,
		metricsStore:       store,
		usage:              usage,
		cfg:                cfg,
		logger:             daemonLogger,
		startedAt:          time.Now().UTC(),
		broker:             b,
		srv:                server.NewServer(serverLogger),
		workspaces:         make(map[string]*workspaceRuntime),
		dirToRoot:          make(map[string]string),
		providerAuths:      make(map[string]*providerAuthFlow),
		browserAuthWriteMu: make(map[string]*sync.Mutex),
		shutdownCh:         make(chan struct{}),
		serveErrCh:         make(chan error, 1),
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

// lockProbeTimeout bounds the liveness probe against an existing socket when
// the daemon lock is already held.
const lockProbeTimeout = 300 * time.Millisecond

// lockRetryTimeout and lockRetryInterval bound the graceful lock retry performed
// when a previous daemon is still tearing down (socket closed, lock not yet released).
const (
	lockRetryTimeout  = 3 * time.Second
	lockRetryInterval = 100 * time.Millisecond
)

// socketAlive reports whether a daemon is still accepting connections on the
// configured socket path.
func (d *Daemon) socketAlive() bool {
	conn, err := net.DialTimeout("unix", d.cfg.SocketPath, lockProbeTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// acquireLock acquires the daemon lock with a graceful handoff window. When the
// lock is held but no daemon answers on the socket, the previous instance is most
// likely still finishing teardown (which releases the lock last), so acquisition
// is retried briefly before failing with ErrAlreadyRunning including the holder pid.
func (d *Daemon) acquireLock(ctx context.Context) (*os.File, error) {
	lockFile, err := sys.AcquireLock(d.cfg.LockPath)
	if err == nil {
		return lockFile, nil
	}
	if !errors.Is(err, sys.ErrLocked) {
		return nil, fmt.Errorf("%w: %v", ErrAlreadyRunning, err)
	}
	if d.socketAlive() {
		return nil, alreadyRunningErr(d.cfg.LockPath)
	}

	d.logger.Warn("daemon lock held by a process that is not serving; retrying briefly",
		log.String("lock", d.cfg.LockPath),
		log.String("socket", d.cfg.SocketPath))

	lockFile, err = sys.AcquireLockWithRetry(ctx, d.cfg.LockPath, lockRetryTimeout, lockRetryInterval)
	if err != nil {
		if errors.Is(err, sys.ErrLocked) {
			return nil, alreadyRunningErr(d.cfg.LockPath)
		}
		return nil, err
	}
	return lockFile, nil
}

// alreadyRunningErr builds ErrAlreadyRunning annotated with the recorded holder pid.
func alreadyRunningErr(lockPath string) error {
	if pid, ok := sys.LockHolderPID(lockPath); ok {
		return fmt.Errorf("%w (held by pid=%d)", ErrAlreadyRunning, pid)
	}
	return ErrAlreadyRunning
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

	lockFile, err := d.acquireLock(ctx)
	if err != nil {
		return err
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
// background turns to abort, closes all workspace runtimes, the gateway pool,
// and browser OAuth drivers, removes the Unix socket, and releases the lockfile.
// Concurrent calls to Close block until teardown completes and return the same result.
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
		if err := auth.CloseBrowserOAuthDrivers(); err != nil {
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
		Version:    version.Get(),
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
	if model, ok := owner.Catalog().ResolveContext(ctx, models.RoleCompletion, candidates...); ok {
		converted := model.ToAPI()
		var agentThinking *api.ThinkingSetting
		if ag, ok := cat.GetAgent(defaultAgent); ok {
			agentThinking = ag.Thinking
		}
		converted.ThinkingDefault = model.EffectiveThinking(nil, agentThinking)
		resolvedDefault = &converted
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cleanDir, _ := fsutils.CleanPath(rawWS)

	// Workspace warnings are durable startup diagnostics. MCP server health changes
	// during the session, so the CLI reports it in /warnings from the MCP listing
	// instead of this response.
	var sandboxWarnings []string

	// The effective sandbox posture comes from sandbox.json (global + project
	// scopes); a manager failure degrades to the platform default rather than
	// failing the whole workspace read, but the read failure itself is surfaced
	// as a warning so a malformed file is never silent.
	sandboxMode := sandbox.DefaultMode()
	if cfgMgr := rt.SandboxConfig(); cfgMgr != nil {
		if eff, err := cfgMgr.Effective(); err == nil {
			if resolved, err := eff.ResolveMode(""); err == nil {
				sandboxMode = resolved
			}
			if availErr := sandbox.NativeAvailable(); availErr != nil {
				// Without an explicit mode the user has not opted into local
				// isolation, so tell them kernel sandboxing is off by default
				// here; an explicit native request on such a host always
				// deserves the warning too.
				if eff.Mode == "" || eff.Mode == api.SandboxNative {
					sandboxWarnings = append(sandboxWarnings, "Kernel sandboxing is unavailable on this host ("+availErr.Error()+"); sessions fall back to in-process (local) isolation, or fail when failIfUnavailable is enabled.")
				}
			}
		} else {
			sandboxWarnings = append(sandboxWarnings, "sandbox.json could not be read ("+err.Error()+"); falling back to the default sandbox posture — check the global and project sandbox.json files for syntax errors.")
		}
	}
	warnings := append([]string(nil), sandboxWarnings...)
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
		Name:            rt.Workspace().Name(),
		RootDir:         rt.Workspace().RootDir(),
		CWD:             cleanDir,
		DefaultAgent:    owner.Workspace().Catalog().DefaultAgentName(),
		DefaultModel:    resolvedDefault,
		Warnings:        warnings,
		SandboxWarnings: sandboxWarnings,
		IsRestricted:    rt.Workspace().IsRestricted(),
		SandboxMode:     sandboxMode,
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
		idx, err = rt.getFileIndex()
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
		model, ok = rt.Catalog().GetContext(ctx, models.ID(id))
	} else {
		model, ok = rt.Catalog().ResolveContext(ctx, models.RoleCompletion, id)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !ok {
		return &api.Model{ID: id, Name: id, Label: id}, nil
	}
	m := model.ToAPI()
	return &m, nil
}

// GetModelQuota reports the remaining quota of a quota-billed model. It
// returns a nil payload (HTTP 204 semantics handled by the router) when the
// model is unknown, has no provider gateway, or its provider does not report
// quotas.
func (d *Daemon) GetModelQuota(ctx context.Context, rawWS, id string) (*api.ModelQuota, error) {
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
		model, ok = rt.Catalog().GetContext(ctx, models.ID(id))
	} else {
		model, ok = rt.Catalog().ResolveContext(ctx, models.RoleCompletion, id)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	gw := rt.Gateway()
	if gw == nil {
		return nil, nil
	}
	return gw.Quota(ctx, model)
}

// GetQuotaSnapshots returns the live multi-bucket quota snapshots across all
// quota-capable providers in the workspace.
func (d *Daemon) GetQuotaSnapshots(ctx context.Context, rawWS string) (*api.QuotaSnapshotResponse, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}
	gw := rt.Gateway()
	if gw == nil {
		return &api.QuotaSnapshotResponse{}, nil
	}
	return gw.QuotaSnapshots(ctx)
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

// GetSandbox returns the workspace's sandbox posture: the stored global and
// project sandbox.json scopes, their effective merge, host kernel backend
// diagnostics, and the canonical resolved path rules for transparent
// inspection in clients.
func (d *Daemon) GetSandbox(ctx context.Context, rawWS string) (*api.SandboxResponse, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	cfgMgr := rt.SandboxConfig()
	if cfgMgr == nil {
		return nil, &api.APIError{
			StatusCode: http.StatusInternalServerError,
			Code:       "internal_error",
			Message:    "daemon: sandbox settings manager is unavailable for this workspace",
		}
	}

	project, err := cfgMgr.Get(api.ScopeProject)
	if err != nil {
		return nil, err
	}
	global, err := cfgMgr.Get(api.ScopeGlobal)
	if err != nil {
		return nil, err
	}
	eff, err := cfgMgr.Effective()
	if err != nil {
		return nil, err
	}

	return &api.SandboxResponse{
		Global:        global,
		Project:       project,
		Effective:     eff.Settings,
		Availability:  sandbox.Availability(),
		ResolvedPaths: eff.ResolvedPaths(),
		Restricted:    cfgMgr.Restricted(),
	}, nil
}

// UpdateSandbox validates and atomically overwrites one scope's sandbox.json
// settings, then refreshes the kernel path policy of every open session in the
// workspace so edits take effect on the next command without restarting them.
// It returns the updated [api.SandboxResponse].
func (d *Daemon) UpdateSandbox(ctx context.Context, rawWS string, req api.UpdateSandboxRequest) (*api.SandboxResponse, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	cfgMgr := rt.SandboxConfig()
	if cfgMgr == nil {
		return nil, &api.APIError{
			StatusCode: http.StatusInternalServerError,
			Code:       "internal_error",
			Message:    "daemon: sandbox settings manager is unavailable for this workspace",
		}
	}

	if err := cfgMgr.Set(req.Scope, req.Settings); err != nil {
		return nil, err
	}

	// A global-scope edit changes ~/.config/tasksmith/sandbox.json, which is
	// the base posture of every workspace the daemon hosts, so every open
	// runtime must re-resolve its live sessions' policies — not just the
	// caller's. Runtimes are deduplicated because several workspace keys can
	// map to the same runtime.
	if req.Scope == api.ScopeGlobal {
		d.wsMu.Lock()
		seen := make(map[*workspaceRuntime]struct{}, len(d.workspaces))
		runtimes := make([]*workspaceRuntime, 0, len(d.workspaces))
		for _, wsRT := range d.workspaces {
			if _, ok := seen[wsRT]; !ok {
				seen[wsRT] = struct{}{}
				runtimes = append(runtimes, wsRT)
			}
		}
		d.wsMu.Unlock()
		for _, wsRT := range runtimes {
			wsRT.RefreshSessions()
		}
	} else {
		rt.RefreshSessions()
	}

	return d.GetSandbox(ctx, rawWS)
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
			Decisions:   summary.Decisions,
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
			Decisions:   summary.Decisions,
			HasEnv:      summary.HasEnv,
		},
		GlobalProviders:    globalProviders,
		GlobalMCPs:         globalMCPs,
		MissingCredentials: missingCredentials,
		NeedsOnboarding:    needsOnboarding,
	}, nil
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

func (d *Daemon) InitWorkspace(ctx context.Context, req api.InitWorkspaceRequest) (*api.InitWorkspaceResponse, error) {
	d.providerInstallMu.Lock()
	defer d.providerInstallMu.Unlock()

	cleanDir, err := fsutils.CleanPath(req.CWD)
	if err != nil {
		return nil, err
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

	skipped, err := workspace.Initialize(ctx, cleanDir, opts)
	if err != nil {
		return nil, err
	}

	if req.DefaultMode != "" {
		if err := permissions.UpdateGlobal(req.DefaultMode); err != nil {
			return nil, fmt.Errorf("daemon: update default permission mode: %w", err)
		}
	}

	d.reloadAllWorkspaces(ctx)
	return &api.InitWorkspaceResponse{Status: "ok", SkippedPresetAliases: skipped}, nil
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

// LatestSession returns metadata for the workspace's most recently updated
// top-level session without hydrating session runtimes. It returns a nil
// session when the workspace has no sessions.
func (d *Daemon) LatestSession(ctx context.Context, rawWS string) (*api.Session, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	meta, err := rt.LatestSessionMeta(ctx)
	if errors.Is(err, session.ErrSessionNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &meta, nil
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

// ForkSession branches a session into a new top-level session seeded with the
// source's conversation.
func (d *Daemon) ForkSession(ctx context.Context, rawWS, id string, req api.ForkSessionRequest) (*api.Session, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	sess, err := rt.ForkSession(ctx, id, req)
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

func (d *Daemon) UploadMedia(ctx context.Context, rawWS, id, mimeType, filename string, data []byte) (*api.Media, error) {
	rt, err := d.workspace(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	sess, err := rt.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}

	return sess.UploadMedia(ctx, mimeType, filename, data)
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
