package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// Sentinel errors returned by the runtime package.
var (
	ErrInvalidConfig  = errors.New("runtime: invalid configuration")
	ErrEmptyWorkspace = errors.New("runtime: workspace parameter is required")
	ErrClosed         = errors.New("runtime: manager is closed")
)

// Runtime manages resources for a single workspace root.
type Runtime interface {
	Dir() string
	Workspace() workspace.Workspace
	Gateway() gateway.Gateway
	Permissions() permissions.Manager
	Sessions() session.Manager
	Close() error
}

// Manager coordinates multiple workspace runtimes, deduplicating instances by canonical workspace root.
type Manager interface {
	Get(ctx context.Context, dir string) (Runtime, error)
	Workspaces() []string
	Close() error
}

// Config holds runtime dependencies required by Manager.
type Config struct {
	GatewayPool gateway.Pool
	Publisher   session.Publisher
	Logger      log.Interface
}

type workspaceRuntime struct {
	dir      string
	ws       workspace.Workspace
	gw       gateway.Gateway
	perms    permissions.Manager
	sessions session.Manager
	db       *sql.DB
}

var _ Runtime = (*workspaceRuntime)(nil)

func (r *workspaceRuntime) Dir() string                      { return r.dir }
func (r *workspaceRuntime) Workspace() workspace.Workspace   { return r.ws }
func (r *workspaceRuntime) Gateway() gateway.Gateway         { return r.gw }
func (r *workspaceRuntime) Permissions() permissions.Manager { return r.perms }
func (r *workspaceRuntime) Sessions() session.Manager        { return r.sessions }

func (r *workspaceRuntime) Close() error {
	sessErr := r.sessions.Close()
	dbErr := r.db.Close()
	return errors.Join(sessErr, dbErr)
}

type manager struct {
	pool      gateway.Pool
	publisher session.Publisher
	logger    log.Interface

	mu        sync.Mutex
	closed    bool
	runtimes  map[string]*workspaceRuntime // key: canonical rootDir
	dirToRoot map[string]string            // key: cleanDir -> rootDir
}

var _ Manager = (*manager)(nil)

// New constructs a new [Manager].
func New(cfg Config) (Manager, error) {
	if cfg.GatewayPool == nil {
		return nil, fmt.Errorf("%w: gateway pool is required", ErrInvalidConfig)
	}
	if cfg.Publisher == nil {
		return nil, fmt.Errorf("%w: publisher is required", ErrInvalidConfig)
	}
	if cfg.Logger == nil {
		return nil, fmt.Errorf("%w: logger is required", ErrInvalidConfig)
	}
	return &manager{
		pool:      cfg.GatewayPool,
		publisher: cfg.Publisher,
		logger:    cfg.Logger.ForComponent("runtime"),
		runtimes:  make(map[string]*workspaceRuntime),
		dirToRoot: make(map[string]string),
	}, nil
}

// Get opens or retrieves an existing workspace runtime for dir.
func (m *manager) Get(ctx context.Context, dir string) (Runtime, error) {
	cleanDir, err := fsutils.CleanPath(dir)
	if err != nil {
		if errors.Is(err, fsutils.ErrEmptyPath) {
			return nil, ErrEmptyWorkspace
		}
		return nil, fmt.Errorf("runtime: resolve workspace path %q: %w", dir, err)
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrClosed
	}
	if rt, ok := m.runtimes[cleanDir]; ok {
		m.mu.Unlock()
		return rt, nil
	}
	m.mu.Unlock()

	ws, err := workspace.Open(ctx, cleanDir, workspace.WithBuiltinFS(builtin.FS()))
	if err != nil {
		return nil, fmt.Errorf("runtime: open workspace %q: %w", cleanDir, err)
	}
	rootDir := ws.RootDir()

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrClosed
	}
	m.dirToRoot[cleanDir] = rootDir
	if rt, ok := m.runtimes[cleanDir]; ok {
		m.mu.Unlock()
		return rt, nil
	}

	// If a runtime already exists for rootDir, share its db, gateway, and sessions manager
	var db *sql.DB
	var gw gateway.Gateway
	var sessMgr session.Manager
	var permMgr permissions.Manager
	for _, existing := range m.runtimes {
		if existing.ws.RootDir() == rootDir {
			db = existing.db
			gw = existing.gw
			sessMgr = existing.sessions
			permMgr = existing.perms
			break
		}
	}

	if sessMgr == nil {
		gwCfg := buildGatewayConfig(ws)
		var err error
		gw, err = m.pool.Acquire(ctx, gwCfg)
		if err != nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("runtime: acquire gateway for %q: %w", rootDir, err)
		}

		wsDataDir, err := xdg.WorkspaceDir(rootDir)
		if err != nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("runtime: resolve workspace data dir for %q: %w", rootDir, err)
		}

		dbPath := filepath.Join(wsDataDir, "tasksmith.db")
		db, err = sqlite.Open(ctx, dbPath)
		if err != nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("runtime: open sqlite db %q: %w", dbPath, err)
		}

		permMgr, err = permissions.New(permissions.Config{Workspace: ws})
		if err != nil {
			_ = db.Close()
			m.mu.Unlock()
			return nil, fmt.Errorf("runtime: initialize permissions manager for %q: %w", rootDir, err)
		}

		sessMgr, err = session.New(session.Config{
			DB:          db,
			Workspace:   ws,
			Gateway:     gw,
			Publisher:   m.publisher,
			Logger:      m.logger,
			Permissions: permMgr,
		})
		if err != nil {
			_ = db.Close()
			m.mu.Unlock()
			return nil, fmt.Errorf("runtime: initialize session manager for %q: %w", rootDir, err)
		}
	}

	rt := &workspaceRuntime{
		dir:      cleanDir,
		ws:       ws,
		gw:       gw,
		perms:    permMgr,
		sessions: sessMgr,
		db:       db,
	}
	m.runtimes[cleanDir] = rt
	m.mu.Unlock()

	return rt, nil
}

// Workspaces returns sorted canonical root directories of all open runtimes.
func (m *manager) Workspaces() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	seen := make(map[string]struct{})
	roots := make([]string, 0, len(m.runtimes))
	for _, rt := range m.runtimes {
		root := rt.ws.RootDir()
		if _, ok := seen[root]; !ok {
			seen[root] = struct{}{}
			roots = append(roots, root)
		}
	}
	sort.Strings(roots)
	return roots
}

// Close marks manager closed and closes all open runtimes.
func (m *manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	runtimes := make([]*workspaceRuntime, 0, len(m.runtimes))
	for _, rt := range m.runtimes {
		runtimes = append(runtimes, rt)
	}
	clear(m.runtimes)
	clear(m.dirToRoot)
	m.mu.Unlock()

	seenDB := make(map[*sql.DB]struct{})
	var errs []error
	for _, rt := range runtimes {
		if _, ok := seenDB[rt.db]; !ok && rt.db != nil {
			seenDB[rt.db] = struct{}{}
			if err := rt.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func buildGatewayConfig(ws workspace.Workspace) gateway.Config {
	wsEnv := ws.Env()
	wsProviders := ws.Providers()
	providers := make([]gateway.ProviderConfig, 0, len(wsProviders))
	for _, p := range wsProviders {
		providers = append(providers, toGatewayProvider(p))
	}

	defaultModels := make(map[gateway.ModelRole]string)
	if models := ws.ResolveModels(workspace.RoleCompletion); len(models) > 0 {
		defaultModels[gateway.RoleCompletion] = models[0]
	}
	if models := ws.ResolveModels(workspace.RoleDecision); len(models) > 0 {
		defaultModels[gateway.RoleDecision] = models[0]
	}
	if models := ws.ResolveModels(workspace.RoleEmbedding); len(models) > 0 {
		defaultModels[gateway.RoleEmbedding] = models[0]
	}

	return gateway.Config{
		Providers:     providers,
		DefaultModels: defaultModels,
		Env:           wsEnv,
	}
}

func toGatewayProvider(p workspace.Provider) gateway.ProviderConfig {
	var auth *gateway.AuthConfig
	if p.Auth != nil {
		auth = &gateway.AuthConfig{
			Type:   gateway.AuthType(p.Auth.Type),
			Header: p.Auth.Header,
			Env:    p.Auth.Env,
			File:   p.Auth.File,
		}
	}

	var defaultModels map[gateway.ModelRole]string
	if len(p.DefaultModels) > 0 {
		defaultModels = make(map[gateway.ModelRole]string, len(p.DefaultModels))
		for r, m := range p.DefaultModels {
			defaultModels[gateway.ModelRole(r)] = m
		}
	}

	var models []gateway.ModelSpec
	if len(p.Models) > 0 {
		models = make([]gateway.ModelSpec, len(p.Models))
		for i, m := range p.Models {
			var caps *gateway.ModelCapabilities
			if m.Capabilities != nil {
				caps = &gateway.ModelCapabilities{
					Completion: m.Capabilities.Completion,
					Decision:   m.Capabilities.Decision,
					Embedding:  m.Capabilities.Embedding,
					Attachment: m.Capabilities.Attachment,
					Tools:      m.Capabilities.Tools,
					Dimensions: m.Capabilities.Dimensions,
				}
			}
			models[i] = gateway.ModelSpec{
				ID:    m.ID,
				Name:  m.Name,
				Label: m.Label,
				Limits: gateway.ModelLimits{
					Context:    m.Limits.Context,
					Output:     m.Limits.Output,
					Dimensions: m.Limits.Dimensions,
				},
				Capabilities: caps,
			}
		}
	}

	return gateway.ProviderConfig{
		Name:          p.Name,
		Type:          p.Type,
		Endpoint:      p.Endpoint,
		DefaultModel:  p.DefaultModel,
		DefaultModels: defaultModels,
		Auth:          auth,
		Headers:       p.Headers,
		Config:        p.Config,
		Models:        models,
	}
}
