package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/gateway"
	"github.com/masterkeysrd/tasksmith/internal/session"
	"github.com/masterkeysrd/tasksmith/internal/tools"
	"github.com/masterkeysrd/tasksmith/internal/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
)

// Sentinel errors returned by the daemon package.
var (
	ErrInvalidConfig  = errors.New("daemon: invalid configuration")
	ErrAlreadyRunning = errors.New("daemon: server is already running")
	ErrSessionRunning = errors.New("daemon: session already has an active turn")
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

// Config holds the mandatory configuration parameters for starting a [Server].
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

// Server is the single-instance TaskSmith Brain Daemon HTTP+SSE server.
type Server struct {
	cfg        Config
	logger     log.Interface
	startedAt  time.Time
	hub        *hub
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

// New validates cfg and constructs a new [Server].
func New(cfg Config) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cfg.SocketPath = filepath.Clean(strings.TrimSpace(cfg.SocketPath))
	cfg.LockPath = filepath.Clean(strings.TrimSpace(cfg.LockPath))

	s := &Server{
		cfg:        cfg,
		logger:     cfg.Logger.ForComponent("daemon"),
		startedAt:  time.Now().UTC(),
		hub:        newHub(),
		runtimes:   make(map[string]*workspaceRuntime),
		dirToRoot:  make(map[string]string),
		shutdownCh: make(chan struct{}),
		serveErrCh: make(chan error, 1),
	}
	s.httpServer = &http.Server{
		Handler: s.routes(),
	}
	return s, nil
}

// SocketPath returns the Unix domain socket path configured on the server.
func (s *Server) SocketPath() string {
	return s.cfg.SocketPath
}

// Start acquires the non-blocking exclusive flock on [Config.LockPath], binds
// the Unix domain socket at [Config.SocketPath], and begins serving HTTP+SSE
// requests in a background goroutine.
func (s *Server) Start(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.New("daemon: server is closed")
	}
	if s.started {
		return nil
	}

	lockFile, err := acquireLockFile(s.cfg.LockPath)
	if err != nil {
		return err
	}

	ln, err := listenUnixSocket(s.cfg.SocketPath)
	if err != nil {
		_ = releaseLockFile(lockFile)
		return err
	}

	s.lockFile = lockFile
	s.listener = ln
	s.started = true

	go func() {
		err := s.httpServer.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.serveErrCh <- err
			return
		}
		s.serveErrCh <- nil
	}()

	return nil
}

// Serve starts the server (if not already started) and blocks until ctx is
// canceled or a shutdown is requested via `/v1/shutdown` or [Server.Close].
func (s *Server) Serve(ctx context.Context) error {
	if err := s.Start(ctx); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return s.Close()
	case <-s.shutdownCh:
		return s.Close()
	case err := <-s.serveErrCh:
		_ = s.Close()
		return err
	}
}

func (s *Server) signalShutdown() {
	s.shutdownOnce.Do(func() {
		close(s.shutdownCh)
	})
}

// Close gracefully shuts down the HTTP server, waits for any in-flight
// background turns to abort, closes all workspace runtimes and the gateway pool,
// removes the Unix socket, and releases the lockfile. Concurrent calls to Close
// block until teardown completes and return the same result.
func (s *Server) Close() error {
	s.signalShutdown()
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true

		ln := s.listener
		lockFile := s.lockFile
		s.listener = nil
		s.lockFile = nil

		runtimes := make([]*workspaceRuntime, 0, len(s.runtimes))
		for k, rt := range s.runtimes {
			runtimes = append(runtimes, rt)
			delete(s.runtimes, k)
		}
		clear(s.dirToRoot)
		s.mu.Unlock()

		var errs []error
		if ln != nil {
			shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if err := s.httpServer.Shutdown(shutCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errs = append(errs, err)
			}
			cancel()
			_ = os.Remove(s.cfg.SocketPath)
		}

		for _, rt := range runtimes {
			if err := rt.close(); err != nil {
				errs = append(errs, err)
			}
		}

		if err := s.cfg.GatewayPool.Close(); err != nil {
			errs = append(errs, err)
		}

		if lockFile != nil {
			if err := releaseLockFile(lockFile); err != nil {
				errs = append(errs, err)
			}
		}

		s.closeErr = errors.Join(errs...)
	})
	return s.closeErr
}

func acquireLockFile(lockPath string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, fmt.Errorf("daemon: create lock directory: %w", err)
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("daemon: open lock file %q: %w", lockPath, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrAlreadyRunning
		}
		return nil, fmt.Errorf("%w: %v", ErrAlreadyRunning, err)
	}

	return f, nil
}

func releaseLockFile(f *os.File) error {
	if f == nil {
		return nil
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return f.Close()
}

func listenUnixSocket(socketPath string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return nil, fmt.Errorf("daemon: create socket directory: %w", err)
	}
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("daemon: remove stale socket %q: %w", socketPath, err)
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("daemon: listen unix %q: %w", socketPath, err)
	}
	_ = os.Chmod(socketPath, 0o600)
	return ln, nil
}

func cleanWorkspacePath(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", ErrEmptyWorkspace
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("daemon: resolve workspace path %q: %w", dir, err)
	}
	if eval, evalErr := filepath.EvalSymlinks(absDir); evalErr == nil {
		absDir = eval
	}
	return filepath.Clean(absDir), nil
}

func requestWorkspace(r *http.Request) string {
	if cwd := strings.TrimSpace(r.Header.Get(api.HeaderCWD)); cwd != "" {
		return cwd
	}
	return strings.TrimSpace(r.URL.Query().Get("workspace"))
}

func (s *Server) resolveWorkspaceRoot(ctx context.Context, rawWorkspace string) (string, error) {
	cleanDir, err := cleanWorkspacePath(rawWorkspace)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	if rootDir, ok := s.dirToRoot[cleanDir]; ok {
		s.mu.Unlock()
		return rootDir, nil
	}
	s.mu.Unlock()

	ws, err := workspace.Open(ctx, cleanDir, workspace.WithBuiltinFS(builtin.FS()))
	if err != nil {
		return "", fmt.Errorf("daemon: open workspace %q: %w", cleanDir, err)
	}
	rootDir := ws.RootDir()

	s.mu.Lock()
	if !s.closed {
		s.dirToRoot[cleanDir] = rootDir
	}
	s.mu.Unlock()

	return rootDir, nil
}

func (s *Server) getRuntime(ctx context.Context, rawWorkspace string) (*workspaceRuntime, error) {
	cleanDir, err := cleanWorkspacePath(rawWorkspace)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("daemon: server is closed")
	}
	if rt, exists := s.runtimes[cleanDir]; exists {
		s.mu.Unlock()
		return rt, nil
	}
	s.mu.Unlock()

	ws, err := workspace.Open(ctx, cleanDir, workspace.WithBuiltinFS(builtin.FS()))
	if err != nil {
		return nil, fmt.Errorf("daemon: open workspace %q: %w", cleanDir, err)
	}
	rootDir := ws.RootDir()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("daemon: server is closed")
	}
	s.dirToRoot[cleanDir] = rootDir
	if existing, ok := s.runtimes[cleanDir]; ok {
		s.mu.Unlock()
		return existing, nil
	}
	s.mu.Unlock()

	rt, err := newWorkspaceRuntime(ctx, ws, s.cfg.GatewayPool, s.hub, s.logger)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = rt.close()
		return nil, errors.New("daemon: server is closed")
	}
	if existing, ok := s.runtimes[cleanDir]; ok {
		s.mu.Unlock()
		_ = rt.close()
		return existing, nil
	}
	s.runtimes[cleanDir] = rt
	s.mu.Unlock()

	return rt, nil
}

func (s *Server) openWorkspaces() []string {
	s.mu.Lock()
	seen := make(map[string]struct{})
	dirs := make([]string, 0, len(s.runtimes))
	for _, rt := range s.runtimes {
		root := rt.workspace.RootDir()
		if _, ok := seen[root]; !ok {
			seen[root] = struct{}{}
			dirs = append(dirs, root)
		}
	}
	s.mu.Unlock()
	sort.Strings(dirs)
	return dirs
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("POST /v1/shutdown", s.handleShutdown)
	mux.HandleFunc("GET /v1/events", s.handleEvents)

	mux.HandleFunc("GET /v1/workspace", s.handleGetWorkspace)
	mux.HandleFunc("GET /v1/agents", s.handleListAgents)
	mux.HandleFunc("GET /v1/agents/{name}", s.handleGetAgent)
	mux.HandleFunc("GET /v1/models", s.handleListModels)
	mux.HandleFunc("GET /v1/models/{id...}", s.handleGetModel)

	mux.HandleFunc("POST /v1/sessions", s.handleCreateSession)
	mux.HandleFunc("GET /v1/sessions", s.handleListSessions)
	mux.HandleFunc("GET /v1/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("PATCH /v1/sessions/{id}", s.handleUpdateSession)
	mux.HandleFunc("DELETE /v1/sessions/{id}", s.handleDeleteSession)

	mux.HandleFunc("GET /v1/sessions/{id}/messages", s.handleHistory)
	mux.HandleFunc("POST /v1/sessions/{id}/messages", s.handleSend)
	mux.HandleFunc("POST /v1/sessions/{id}/cancel", s.handleCancel)

	mux.HandleFunc("GET /v1/sessions/{id}/permissions", s.handlePendingPermissions)
	mux.HandleFunc("POST /v1/sessions/{id}/permissions", s.handleRespondPermission)
	mux.HandleFunc("GET /v1/sessions/{id}/mode", s.handleGetMode)
	mux.HandleFunc("PUT /v1/sessions/{id}/mode", s.handleSetMode)

	mux.HandleFunc("GET /v1/sessions/{id}/changes", s.handleChanges)
	mux.HandleFunc("GET /v1/sessions/{id}/diff", s.handleDiff)
	mux.HandleFunc("POST /v1/sessions/{id}/revert", s.handleRevert)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.HealthResponse{
		PID:        os.Getpid(),
		StartedAt:  s.startedAt,
		Workspaces: s.openWorkspaces(),
	})
}

func (s *Server) handleShutdown(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "shutting_down"})
	s.signalShutdown()
	go func() {
		_ = s.Close()
	}()
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, errors.New("streaming unsupported"))
		return
	}

	var wsFilter string
	if rawWS := requestWorkspace(r); rawWS != "" {
		rootDir, err := s.resolveWorkspaceRoot(r.Context(), rawWS)
		if err != nil {
			writeError(w, err)
			return
		}
		wsFilter = rootDir
	}
	sessionFilter := strings.TrimSpace(r.URL.Query().Get("session_id"))

	sub, unsubscribe := s.hub.subscribe(wsFilter, sessionFilter)
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, ": connected\n\n")
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.shutdownCh:
			return
		case <-ticker.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			flusher.Flush()
		case ev, open := <-sub.ch:
			if !open {
				return
			}
			raw, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, raw)
			flusher.Flush()
		}
	}
}

func (s *Server) handleGetWorkspace(w http.ResponseWriter, r *http.Request) {
	wsPath := requestWorkspace(r)
	rt, err := s.getRuntime(r.Context(), wsPath)
	if err != nil {
		writeError(w, err)
		return
	}

	var defModel string
	if models := rt.workspace.ResolveModels(workspace.RoleCompletion); len(models) > 0 {
		defModel = models[0]
	}

	cleanDir, _ := cleanWorkspacePath(wsPath)

	out := api.Workspace{
		Name:         rt.workspace.Name(),
		RootDir:      rt.workspace.RootDir(),
		CWD:          cleanDir,
		DefaultAgent: rt.workspace.DefaultAgentName(),
		DefaultModel: defModel,
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	agents := rt.workspace.Agents()
	out := make([]api.Agent, len(agents))
	for i, ag := range agents {
		out[i] = toAPIAgent(ag)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	name := strings.TrimSpace(r.PathValue("name"))
	ag, ok := rt.workspace.GetAgent(name)
	if !ok {
		writeError(w, &api.APIError{
			StatusCode: http.StatusNotFound,
			Code:       "not_found",
			Message:    fmt.Sprintf("agent %q not found", name),
		})
		return
	}

	writeJSON(w, http.StatusOK, toAPIAgent(*ag))
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	providers := rt.workspace.Providers()
	var out []api.Model
	for _, p := range providers {
		for _, m := range p.Models {
			out = append(out, toAPIModel(p.Name, m))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetModel(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	rawID := strings.TrimSpace(r.PathValue("id"))
	if rawID == "" {
		writeBadRequest(w, errors.New("model ID must not be empty"))
		return
	}

	writeJSON(w, http.StatusOK, resolveAPIModel(rt.workspace, rawID))
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	var req api.CreateSessionRequest
	if err := decodeOptionalJSON(r.Body, &req); err != nil {
		writeBadRequest(w, err)
		return
	}

	sess, created, err := rt.manager.Ensure(r.Context(), session.CreateParams{
		ID:    req.ID,
		Title: req.Title,
		Agent: req.Agent,
		Model: req.Model,
	})
	if err != nil {
		writeError(w, err)
		return
	}

	// An idempotent create of an existing session returns it with 200.
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	writeJSON(w, status, toAPISession(sess, rt.sessionStatus(r.Context(), sess.ID)))
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	list, err := rt.manager.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	out := make([]api.Session, len(list))
	for i := range list {
		out[i] = toAPISession(&list[i], rt.sessionStatus(r.Context(), list[i].ID))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	id := r.PathValue("id")
	sess, err := rt.manager.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, toAPISession(sess, rt.sessionStatus(r.Context(), sess.ID)))
}

func (s *Server) handleUpdateSession(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	id := r.PathValue("id")
	var req api.UpdateSessionRequest
	if err := decodeOptionalJSON(r.Body, &req); err != nil {
		writeBadRequest(w, err)
		return
	}

	sess, err := rt.manager.Update(r.Context(), id, session.UpdateParams{
		Title: req.Title,
		Agent: req.Agent,
		Model: req.Model,
	})
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, toAPISession(sess, rt.sessionStatus(r.Context(), sess.ID)))
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	id := r.PathValue("id")
	_ = rt.cancelTurn(r.Context(), id)
	if err := rt.manager.Delete(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	id := r.PathValue("id")
	history, err := rt.manager.History(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	out := make([]api.Message, 0, len(history))
	for _, msg := range history {
		converted, err := toAPIMessage(msg)
		if err != nil {
			writeError(w, err)
			return
		}
		if converted != nil {
			out = append(out, *converted)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	var req api.SendRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeBadRequest(w, err)
		return
	}

	userMsg, err := fromSendRequest(req)
	if err != nil {
		writeError(w, err)
		return
	}

	id := r.PathValue("id")
	if err := rt.startTurn(r.Context(), id, userMsg, req.Wait); err != nil {
		writeError(w, err)
		return
	}

	status := http.StatusAccepted
	if req.Wait {
		status = http.StatusOK
	}
	w.WriteHeader(status)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	id := r.PathValue("id")
	if err := rt.cancelTurn(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handlePendingPermissions(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	id := r.PathValue("id")
	perms, err := rt.manager.PendingPermissions(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	out := make([]api.PermissionRequest, len(perms))
	for i, p := range perms {
		out[i] = toAPIPermissionRequest(p)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRespondPermission(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	var req api.PermissionDecision
	if err := decodeJSON(r.Body, &req); err != nil {
		writeBadRequest(w, err)
		return
	}

	id := r.PathValue("id")
	if err := rt.respondPermission(r.Context(), id, fromAPIPermissionDecision(req), req.Wait); err != nil {
		writeError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetMode(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	id := r.PathValue("id")
	mode, err := rt.manager.PermissionMode(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, api.ModeResponse{Mode: api.PermissionMode(mode)})
}

func (s *Server) handleSetMode(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	var req api.SetModeRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeBadRequest(w, err)
		return
	}

	id := r.PathValue("id")
	if err := rt.manager.SetPermissionMode(r.Context(), id, tools.PermissionMode(req.Mode)); err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, api.ModeResponse{Mode: req.Mode})
}

func (s *Server) handleChanges(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	id := r.PathValue("id")
	tr, err := rt.manager.Tracker(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	changes, err := tr.Changes()
	if err != nil {
		writeError(w, err)
		return
	}

	out := make([]api.FileChange, len(changes))
	for i, fc := range changes {
		out[i] = toAPIFileChange(fc)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	id := r.PathValue("id")
	tr, err := rt.manager.Tracker(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	path := strings.TrimSpace(r.URL.Query().Get("path"))
	entryID := strings.TrimSpace(r.URL.Query().Get("entry_id"))
	if path == "" || entryID == "" {
		writeBadRequest(w, errors.New("path and entry_id query parameters are required"))
		return
	}

	diffText, err := tr.DiffEntry(path, entryID)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, api.DiffResponse{
		Path:    path,
		EntryID: entryID,
		Diff:    diffText,
	})
}

func (s *Server) handleRevert(w http.ResponseWriter, r *http.Request) {
	rt, err := s.getRuntime(r.Context(), requestWorkspace(r))
	if err != nil {
		writeError(w, err)
		return
	}

	var req api.RevertRequest
	if err := decodeOptionalJSON(r.Body, &req); err != nil {
		writeBadRequest(w, err)
		return
	}

	id := r.PathValue("id")
	tr, err := rt.manager.Tracker(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	path := strings.TrimSpace(req.Path)
	entryID := strings.TrimSpace(req.EntryID)

	if path != "" && entryID != "" {
		if err := tr.RecoverAt(r.Context(), rt.sandbox.FS(), path, entryID, req.Before); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	if path != "" {
		if err := tr.RevertToBaseline(r.Context(), rt.sandbox.FS(), path, req.Force); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := tr.RevertSession(r.Context(), rt.sandbox.FS(), req.Force); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func decodeJSON(r io.Reader, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r, 4<<20))
	return dec.Decode(dst)
}

func decodeOptionalJSON(r io.Reader, dst any) error {
	if r == nil {
		return nil
	}
	dec := json.NewDecoder(io.LimitReader(r, 4<<20))
	if err := dec.Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeBadRequest(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusBadRequest, api.ErrorResponse{
		Code:    "bad_request",
		Message: err.Error(),
	})
}

func writeError(w http.ResponseWriter, err error) {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.Code
		if code == "" {
			code = "error"
		}
		writeJSON(w, apiErr.StatusCode, api.ErrorResponse{
			Code:    code,
			Message: apiErr.Message,
		})
		return
	}
	status, code := classifyError(err)
	writeJSON(w, status, api.ErrorResponse{
		Code:    code,
		Message: err.Error(),
	})
}

func classifyError(err error) (int, string) {
	switch {
	case errors.Is(err, ErrEmptyWorkspace),
		errors.Is(err, ErrEmptyMessage),
		errors.Is(err, session.ErrEmptySessionID),
		errors.Is(err, session.ErrInvalidDecision),
		errors.Is(err, session.ErrInvalidConfig),
		errors.Is(err, session.ErrInvalidSessionID),
		errors.Is(err, session.ErrNoPendingPermissions):
		return http.StatusBadRequest, "bad_request"

	case errors.Is(err, session.ErrSessionNotFound),
		errors.Is(err, session.ErrPermissionNotFound),
		errors.Is(err, session.ErrJournalNotFound),
		errors.Is(err, session.ErrEntryNotFound),
		errors.Is(err, session.ErrBlobNotFound):
		return http.StatusNotFound, "not_found"

	case errors.Is(err, ErrSessionRunning),
		errors.Is(err, session.ErrSessionConflict),
		errors.Is(err, session.ErrRevertConflict):
		return http.StatusConflict, "conflict"

	default:
		return http.StatusInternalServerError, "internal_error"
	}
}
