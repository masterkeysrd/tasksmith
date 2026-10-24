package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
	"github.com/masterkeysrd/tasksmith/internal/daemon/server"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// Sentinel errors returned by the daemon package.
var (
	ErrInvalidConfig  = errors.New("daemon: invalid configuration")
	ErrAlreadyRunning = errors.New("daemon: server is already running")
	ErrSessionRunning = session.ErrSessionRunning
	ErrEmptyWorkspace = errors.New("daemon: workspace parameter is required")
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
	runtimes     map[string]*workspaceRuntime
	dirToRoot    map[string]string
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

	d := &Daemon{
		cfg:       cfg,
		logger:    daemonLogger,
		startedAt: time.Now().UTC(),
		broker: broker.New(broker.WithTopicBufferSize(func(topic string) int {
			if topic == api.TopicSessions {
				return 128
			}
			if strings.HasSuffix(topic, ".stream") {
				return 1024
			}
			return 128
		})),
		srv:        server.NewServer(serverLogger),
		runtimes:   make(map[string]*workspaceRuntime),
		dirToRoot:  make(map[string]string),
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

		runtimes := make([]*workspaceRuntime, 0, len(d.runtimes))
		for k, rt := range d.runtimes {
			runtimes = append(runtimes, rt)
			delete(d.runtimes, k)
		}
		clear(d.dirToRoot)
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

		for _, rt := range runtimes {
			if err := rt.close(); err != nil {
				errs = append(errs, err)
			}
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
		Workspaces: d.openWorkspaces(),
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
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	var defModel string
	if models := rt.workspace.ResolveModels(workspace.RoleCompletion); len(models) > 0 {
		defModel = models[0]
	}

	cleanDir, _ := fsutils.CleanPath(rawWS)

	return &api.Workspace{
		Name:         rt.workspace.Name(),
		RootDir:      rt.workspace.RootDir(),
		CWD:          cleanDir,
		DefaultAgent: rt.workspace.DefaultAgentName(),
		DefaultModel: defModel,
	}, nil
}

func (d *Daemon) ListAgents(ctx context.Context, rawWS string) ([]api.Agent, error) {
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	agents := rt.workspace.Agents()
	out := make([]api.Agent, len(agents))
	for i, ag := range agents {
		out[i] = toAPIAgent(ag)
	}
	return out, nil
}

func (d *Daemon) GetAgent(ctx context.Context, rawWS, name string) (*api.Agent, error) {
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	name = strings.TrimSpace(name)
	ag, ok := rt.workspace.GetAgent(name)
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
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	providers := rt.workspace.Providers()
	var out []api.Model
	for _, p := range providers {
		for _, m := range p.Models {
			out = append(out, toAPIModel(p.Name, m))
		}
	}
	return out, nil
}

func (d *Daemon) GetModel(ctx context.Context, rawWS, id string) (*api.Model, error) {
	rt, err := d.getRuntime(ctx, rawWS)
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

	m := resolveAPIModel(rt.workspace, rawID)
	return &m, nil
}

// SessionBackend implementation

func (d *Daemon) CreateSession(ctx context.Context, rawWS string, req api.CreateSessionRequest) (*api.Session, bool, error) {
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return nil, false, err
	}

	sess, created, err := rt.manager.Ensure(ctx, session.CreateParams{
		ID:    req.ID,
		Title: req.Title,
		Agent: req.Agent,
		Model: req.Model,
	})
	if err != nil {
		return nil, false, err
	}

	out := toAPISession(sess)
	if created {
		d.broker.Publish(api.TopicSessions, api.SessionCreatedEvent{
			Workspace: rt.dir,
			Session:   out,
		})
	}
	return &out, created, nil
}

func (d *Daemon) ListSessions(ctx context.Context, rawWS string) ([]api.Session, error) {
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	list, err := rt.manager.List(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]api.Session, len(list))
	for i := range list {
		out[i] = toAPISession(&list[i])
	}
	return out, nil
}

func (d *Daemon) GetSession(ctx context.Context, rawWS, id string) (*api.Session, error) {
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	sess, err := rt.manager.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	out := toAPISession(sess)
	return &out, nil
}

func (d *Daemon) UpdateSession(ctx context.Context, rawWS, id string, req api.UpdateSessionRequest) (*api.Session, error) {
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	sess, err := rt.manager.Update(ctx, id, session.UpdateParams{
		Title: req.Title,
		Agent: req.Agent,
		Model: req.Model,
	})
	if err != nil {
		return nil, err
	}

	out := toAPISession(sess)
	d.broker.Publish(api.TopicSessions, api.SessionUpdatedEvent{
		Workspace: rt.dir,
		Session:   out,
	})
	return &out, nil
}

func (d *Daemon) DeleteSession(ctx context.Context, rawWS, id string) error {
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return err
	}

	if err := rt.manager.Delete(ctx, id); err != nil {
		return err
	}
	d.broker.DeleteTopic(api.SessionStreamTopic(id))

	d.broker.Publish(api.TopicSessions, api.SessionDeletedEvent{
		Workspace: rt.dir,
		SessionID: id,
	})
	return nil
}

func (d *Daemon) Timeline(ctx context.Context, rawWS, id string) ([]api.TimelineEntry, error) {
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	entries, err := rt.manager.Timeline(ctx, id)
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
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return err
	}

	userMsg, err := fromSendRequest(req)
	if err != nil {
		return err
	}

	return rt.manager.StartTurn(ctx, id, userMsg, req.Wait)
}

func (d *Daemon) Cancel(ctx context.Context, rawWS, id string) error {
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return err
	}

	return rt.manager.Cancel(ctx, id)
}

func (d *Daemon) PendingPermissions(ctx context.Context, rawWS, id string) ([]api.PermissionRequest, error) {
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	perms, err := rt.manager.PendingPermissions(ctx, id)
	if err != nil {
		return nil, err
	}

	return toAPIPermissionRequests(perms), nil
}

func (d *Daemon) RespondPermission(ctx context.Context, rawWS, id string, dec api.PermissionDecision) error {
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return err
	}

	return rt.manager.RespondPermissionAsync(ctx, id, fromAPIPermissionDecision(dec), dec.Wait)
}

func (d *Daemon) GetMode(ctx context.Context, rawWS, id string) (api.PermissionMode, error) {
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return "", err
	}

	mode, err := rt.manager.PermissionMode(ctx, id)
	if err != nil {
		return "", err
	}

	return api.PermissionMode(mode), nil
}

func (d *Daemon) SetMode(ctx context.Context, rawWS, id string, mode api.PermissionMode) (api.PermissionMode, error) {
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return "", err
	}

	if err := rt.manager.SetPermissionMode(ctx, id, tools.PermissionMode(mode)); err != nil {
		return "", err
	}

	return mode, nil
}

func (d *Daemon) Changes(ctx context.Context, rawWS, id string) ([]api.FileChange, error) {
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	jr, err := rt.manager.Journal(ctx, id)
	if err != nil {
		return nil, err
	}

	changes, err := jr.Changes(ctx)
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
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return nil, err
	}

	path = strings.TrimSpace(path)
	entryID = strings.TrimSpace(entryID)
	if path == "" || entryID == "" {
		return nil, &api.APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "bad_request",
			Message:    "path and entry_id query parameters are required",
		}
	}

	jr, err := rt.manager.Journal(ctx, id)
	if err != nil {
		return nil, err
	}

	diffText, err := jr.DiffEntry(ctx, path, entryID)
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
	rt, err := d.getRuntime(ctx, rawWS)
	if err != nil {
		return err
	}

	jr, err := rt.manager.Journal(ctx, id)
	if err != nil {
		return err
	}

	path := strings.TrimSpace(req.Path)
	entryID := strings.TrimSpace(req.EntryID)

	if path != "" && entryID != "" {
		return jr.RecoverAt(ctx, rt.sandbox.FS(), path, entryID, req.Before)
	}

	if path != "" {
		return jr.RevertToBaseline(ctx, rt.sandbox.FS(), path, req.Force)
	}

	return jr.RevertSession(ctx, rt.sandbox.FS(), req.Force)
}

func (d *Daemon) getRuntime(ctx context.Context, rawWorkspace string) (*workspaceRuntime, error) {
	cleanDir, err := fsutils.CleanPath(rawWorkspace)
	if err != nil {
		if errors.Is(err, fsutils.ErrEmptyPath) {
			return nil, ErrEmptyWorkspace
		}
		return nil, fmt.Errorf("daemon: resolve workspace path %q: %w", rawWorkspace, err)
	}

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, errors.New("daemon: server is closed")
	}
	if rt, exists := d.runtimes[cleanDir]; exists {
		d.mu.Unlock()
		return rt, nil
	}
	d.mu.Unlock()

	ws, err := workspace.Open(ctx, cleanDir, workspace.WithBuiltinFS(builtin.FS()))
	if err != nil {
		return nil, fmt.Errorf("daemon: open workspace %q: %w", cleanDir, err)
	}
	rootDir := ws.RootDir()

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, errors.New("daemon: server is closed")
	}
	d.dirToRoot[cleanDir] = rootDir
	if existing, ok := d.runtimes[cleanDir]; ok {
		d.mu.Unlock()
		return existing, nil
	}
	d.mu.Unlock()

	rt, err := newWorkspaceRuntime(ctx, ws, d.cfg.GatewayPool, d.broker, d.logger)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		_ = rt.close()
		return nil, errors.New("daemon: server is closed")
	}
	if existing, ok := d.runtimes[cleanDir]; ok {
		d.mu.Unlock()
		_ = rt.close()
		return existing, nil
	}
	d.runtimes[cleanDir] = rt
	d.mu.Unlock()

	return rt, nil
}

func (d *Daemon) openWorkspaces() []string {
	d.mu.Lock()
	seen := make(map[string]struct{})
	dirs := make([]string, 0, len(d.runtimes))
	for _, rt := range d.runtimes {
		root := rt.workspace.RootDir()
		if _, ok := seen[root]; !ok {
			seen[root] = struct{}{}
			dirs = append(dirs, root)
		}
	}
	d.mu.Unlock()
	sort.Strings(dirs)
	return dirs
}
