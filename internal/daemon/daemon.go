package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/sys"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/runtime"
	"github.com/masterkeysrd/tasksmith/internal/daemon/server"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// Sentinel errors returned by the daemon package.
var (
	ErrInvalidConfig  = errors.New("daemon: invalid configuration")
	ErrAlreadyRunning = errors.New("daemon: server is already running")
	ErrSessionRunning = session.ErrSessionRunning
	ErrEmptyWorkspace = runtime.ErrEmptyWorkspace
	ErrEmptyMessage   = errors.New("daemon: message content must not be empty")
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
	runtimes     runtime.Manager
	shutdownCh   chan struct{}
	shutdownOnce sync.Once
	closeOnce    sync.Once
	closeErr     error
	serveErrCh   chan error
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

	rtMgr, err := runtime.New(runtime.Config{
		GatewayPool: cfg.GatewayPool,
		Publisher:   b,
		Logger:      daemonLogger,
	})
	if err != nil {
		return nil, err
	}

	d := &Daemon{
		cfg:        cfg,
		logger:     daemonLogger,
		startedAt:  time.Now().UTC(),
		broker:     b,
		srv:        server.NewServer(serverLogger),
		runtimes:   rtMgr,
		shutdownCh: make(chan struct{}),
		serveErrCh: make(chan error, 1),
	}

	mux := d.srv.CreateMux(
		context.Background(),
		server.NewSystemRouter(d),
		server.NewEventRouter(d),
		server.NewWorkspaceRouter(d),
		server.NewSessionRouter(d),
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

		if err := d.runtimes.Close(); err != nil {
			errs = append(errs, err)
		}

		if err := d.cfg.GatewayPool.Close(); err != nil {
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
		Workspaces: d.runtimes.Workspaces(),
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
		u, err := uuid.NewV7()
		if err != nil {
			return nil, nil, fmt.Errorf("daemon: generate subscriber id: %w", err)
		}
		subID = u.String()
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
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	var defModel string
	if models := rt.Workspace().ResolveModels(workspace.RoleCompletion); len(models) > 0 {
		defModel = models[0]
	}

	cleanDir, _ := fsutils.CleanPath(rawWS)

	var warnings []string
	if wsWarnings := rt.Workspace().Warnings(); len(wsWarnings) > 0 {
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
		DefaultAgent: rt.Workspace().DefaultAgentName(),
		DefaultModel: defModel,
		Warnings:     warnings,
	}, nil
}

func (d *Daemon) ListAgents(ctx context.Context, rawWS string) ([]api.Agent, error) {
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	agents := rt.Workspace().Agents()
	out := make([]api.Agent, len(agents))
	for i, ag := range agents {
		out[i] = toAPIAgent(ag)
	}
	return out, nil
}

func (d *Daemon) GetAgent(ctx context.Context, rawWS, name string) (*api.Agent, error) {
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	name = strings.TrimSpace(name)
	ag, ok := rt.Workspace().GetAgent(name)
	if !ok {
		return nil, &api.APIError{
			StatusCode: http.StatusNotFound,
			Code:       "not_found",
			Message:    fmt.Sprintf("agent %q not found", name),
		}
	}

	res := toAPIAgent(*ag)
	return &res, nil
}

func (d *Daemon) ListModels(ctx context.Context, rawWS string) ([]api.Model, error) {
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	providers := rt.Workspace().Providers()
	var out []api.Model
	for _, p := range providers {
		for _, m := range p.Models {
			out = append(out, toAPIModel(p.Name, m))
		}
	}
	return out, nil
}

func (d *Daemon) GetModel(ctx context.Context, rawWS, id string) (*api.Model, error) {
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	rawID := strings.TrimSpace(id)
	if rawID == "" {
		return nil, &api.APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "bad_request",
			Message:    "model ID must not be empty",
		}
	}

	m := resolveAPIModel(rt.Workspace(), rawID)
	return &m, nil
}

func (d *Daemon) GetPermissions(ctx context.Context, rawWS string) (*api.PermissionsResponse, error) {
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	proj, err := rt.Permissions().Get(permissions.ScopeProject)
	if err != nil {
		return nil, err
	}
	glob, err := rt.Permissions().Get(permissions.ScopeGlobal)
	if err != nil {
		return nil, err
	}

	return &api.PermissionsResponse{
		Project: toAPIPermissions(proj),
		Global:  toAPIPermissions(glob),
	}, nil
}

func (d *Daemon) AddPermissionRule(ctx context.Context, rawWS string, req api.PermissionRuleRequest) (*api.PermissionsResponse, error) {
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	effect, err := fromAPIPermissionEffect(req.Effect)
	if err != nil {
		return nil, err
	}

	scope := permissions.Scope(strings.TrimSpace(string(req.Scope)))
	if err := rt.Permissions().AddRule(scope, effect, req.Rule); err != nil {
		return nil, err
	}

	return d.GetPermissions(ctx, rawWS)
}

func (d *Daemon) RemovePermissionRule(ctx context.Context, rawWS string, req api.PermissionRuleRequest) (*api.PermissionsResponse, error) {
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	effect, err := fromAPIPermissionEffect(req.Effect)
	if err != nil {
		return nil, err
	}

	scope := permissions.Scope(strings.TrimSpace(string(req.Scope)))
	if err := rt.Permissions().RemoveRule(scope, effect, req.Rule); err != nil {
		return nil, err
	}

	return d.GetPermissions(ctx, rawWS)
}

// SessionBackend implementation

func (d *Daemon) CreateSession(ctx context.Context, rawWS string, req api.CreateSessionRequest) (*api.Session, bool, error) {
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return nil, false, err
	}

	sess, created, err := rt.Sessions().Create(ctx, session.CreateParams{
		ID:    req.ID,
		Title: req.Title,
		Config: session.SessionConfig{
			Agent:  req.Config.Agent,
			Model:  req.Config.Model,
			Effort: req.Config.Effort,
			Mode:   tools.PermissionMode(req.Config.Mode),
		},
	})
	if err != nil {
		return nil, false, err
	}

	out := toAPISession(sess)
	return &out, created, nil
}

func (d *Daemon) ListSessions(ctx context.Context, rawWS string) ([]api.Session, error) {
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	list, err := rt.Sessions().List(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]api.Session, len(list))
	for i, s := range list {
		out[i] = toAPISession(s)
	}
	return out, nil
}

func (d *Daemon) GetSession(ctx context.Context, rawWS, id string) (*api.Session, error) {
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	sess, err := rt.Sessions().Get(ctx, id)
	if err != nil {
		return nil, err
	}

	out := toAPISession(sess)
	return &out, nil
}

func (d *Daemon) UpdateSession(ctx context.Context, rawWS, id string, req api.UpdateSessionRequest) (*api.Session, error) {
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	sess, err := rt.Sessions().Update(ctx, id, session.UpdateParams{
		Title: req.Title,
		Config: session.SessionConfig{
			Agent:  req.Config.Agent,
			Model:  req.Config.Model,
			Effort: req.Config.Effort,
			Mode:   tools.PermissionMode(req.Config.Mode),
		},
	})
	if err != nil {
		return nil, err
	}

	out := toAPISession(sess)
	return &out, nil
}

func (d *Daemon) DeleteSession(ctx context.Context, rawWS, id string) error {
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return err
	}

	return rt.Sessions().Delete(ctx, id)
}

func (d *Daemon) Timeline(ctx context.Context, rawWS, id string) ([]api.TimelineEntry, error) {
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	sess, err := rt.Sessions().Get(ctx, id)
	if err != nil {
		return nil, err
	}

	entries, err := sess.Timeline.List(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]api.TimelineEntry, len(entries))
	for i, e := range entries {
		apiEntry, err := toAPITimelineEntry(e)
		if err != nil {
			return nil, err
		}
		out[i] = apiEntry
	}
	return out, nil
}

func (d *Daemon) Send(ctx context.Context, rawWS, id string, req api.SendRequest) error {
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return err
	}

	sess, err := rt.Sessions().Get(ctx, id)
	if err != nil {
		return err
	}

	in, err := fromSendRequest(req)
	if err != nil {
		return err
	}

	return sess.Send(ctx, in)
}

func (d *Daemon) Cancel(ctx context.Context, rawWS, id string) error {
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return err
	}

	sess, err := rt.Sessions().Get(ctx, id)
	if err != nil {
		return err
	}

	return sess.Cancel(ctx)
}

func (d *Daemon) Changes(ctx context.Context, rawWS, id string) ([]api.FileChange, error) {
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	sess, err := rt.Sessions().Get(ctx, id)
	if err != nil {
		return nil, err
	}

	changes, err := sess.Journal.Changes(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]api.FileChange, len(changes))
	for i, fc := range changes {
		out[i] = toAPIFileChange(fc)
	}
	return out, nil
}

func (d *Daemon) Diff(ctx context.Context, rawWS, id, path, entryID string) (*api.DiffResponse, error) {
	rt, err := d.runtimes.Get(ctx, rawWS)
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

	sess, err := rt.Sessions().Get(ctx, id)
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
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return err
	}

	sess, err := rt.Sessions().Get(ctx, id)
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
	rt, err := d.runtimes.Get(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	sess, err := rt.Sessions().Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if sess.Tasks == nil {
		return []api.Task{}, nil
	}

	return toAPITasks(sess.Tasks.List()), nil
}

func (d *Daemon) KillTask(ctx context.Context, rawWS, id, taskID string) error {
	rt, err := d.runtimes.Get(ctx, rawWS)
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

	sess, err := rt.Sessions().Get(ctx, id)
	if err != nil {
		return err
	}

	if sess.Tasks == nil {
		return &api.APIError{
			StatusCode: http.StatusNotFound,
			Code:       "not_found",
			Message:    "task not found",
		}
	}

	if err := sess.Tasks.Kill(taskID); err != nil {
		if errors.Is(err, tasks.ErrTaskNotFound) {
			return &api.APIError{
				StatusCode: http.StatusNotFound,
				Code:       "not_found",
				Message:    fmt.Sprintf("task %q not found", taskID),
			}
		}
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
	rt, err := d.runtimes.Get(ctx, rawWS)
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

	sess, err := rt.Sessions().Get(ctx, id)
	if err != nil {
		return nil, "", nil, err
	}

	if sess.Tasks == nil {
		return nil, "", nil, &api.APIError{
			StatusCode: http.StatusNotFound,
			Code:       "not_found",
			Message:    "task not found",
		}
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
