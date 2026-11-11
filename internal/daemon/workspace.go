package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/memdb"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/journal"
	"github.com/masterkeysrd/tasksmith/internal/daemon/mcp"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

type catalogSlot struct {
	mu    sync.RWMutex
	value *models.Catalog
}

func (s *catalogSlot) get() *models.Catalog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}

func (s *catalogSlot) set(c *models.Catalog) {
	s.mu.Lock()
	s.value = c
	s.mu.Unlock()
}

type gatewaySlot struct {
	mu    sync.RWMutex
	value gateway.Gateway
}

func (s *gatewaySlot) get() gateway.Gateway {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}

func (s *gatewaySlot) set(gw gateway.Gateway) {
	s.mu.Lock()
	s.value = gw
	s.mu.Unlock()
}

type workspaceRuntime struct {
	dir          string
	ws           *workspace.Workspace
	sessionOwner *workspaceRuntime
	reloadMu     *sync.RWMutex
	gw           *gatewaySlot
	catalog      *catalogSlot
	pool         gateway.Pool
	metrics      metrics.Service
	perms        permissions.Manager
	sessionStore session.Store
	sessions     *memdb.DB[*session.Session]
	bgCtx        context.Context
	bgCancel     context.CancelFunc
	wg           sync.WaitGroup
	createMu     sync.Mutex
	publisher    session.Publisher
	logger       log.Interface
	db           *sql.DB
	// mcpManager owns the workspace's live MCP connections. It is created once per
	// workspace root and shared with sibling worktrees, which resolve MCP servers
	// through the root workspace that the manager is attached to.
	mcpManager *mcp.Manager
}

func (r *workspaceRuntime) Dir() string                       { return r.dir }
func (r *workspaceRuntime) Workspace() *workspace.Workspace   { return r.ws }
func (r *workspaceRuntime) Gateway() gateway.Gateway          { return r.gw.get() }
func (r *workspaceRuntime) Catalog() *models.Catalog          { return r.catalog.get() }
func (r *workspaceRuntime) Permissions() permissions.Manager  { return r.perms }
func (r *workspaceRuntime) SessionRuntime() *workspaceRuntime { return r.sessionOwner }

// MCPRuntime returns the MCP runtime connected to this workspace's root, or nil
// when the root declares no MCP servers. Sibling worktrees share the root's
// manager because agent tools are registered against the root's tool registry.
func (r *workspaceRuntime) MCPRuntime() workspace.MCPService {
	if root := r.RootWorkspace(); root != nil {
		return root.MCP()
	}
	return nil
}

// MCPCaller returns this workspace's MCP manager as the narrow contract a session's
// tool container needs to invoke MCP tools and read resources, or nil when no
// manager is attached. The nil check is explicit because a typed nil *mcp.Manager
// would otherwise satisfy the interface and fail only when something calls it.
func (r *workspaceRuntime) MCPCaller() tools.MCPCaller {
	if r == nil {
		return nil
	}
	owner := r.sessionOwner
	if owner == nil {
		owner = r
	}
	caller := mcp.NewToolRuntime(owner.mcpManager)
	if caller == nil {
		return nil
	}
	return caller
}

// RootWorkspace returns the workspace at the repository root. Requests that
// concern MCP servers resolve definitions and manifest paths there, since that is
// where the MCP manager registers the tools it discovers.
func (r *workspaceRuntime) RootWorkspace() *workspace.Workspace {
	if r == nil {
		return nil
	}
	owner := r.sessionOwner
	if owner == nil {
		owner = r
	}
	return owner.ws
}

func (r *workspaceRuntime) Reload(ctx context.Context) error {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	nextWorkspace, commitWorkspace, err := r.ws.PrepareReload(ctx)
	if err != nil {
		return err
	}
	sessionWorkspace := nextWorkspace
	var commitSession func()
	if r.sessionOwner != r {
		sessionWorkspace, commitSession, err = r.sessionOwner.ws.PrepareReload(ctx)
		if err != nil {
			return err
		}
	}
	next, err := r.pool.Acquire(ctx, buildGatewayConfig(sessionWorkspace, recorderOf(r.metrics)))
	if err != nil {
		return err
	}
	catalog, err := models.Build(nextWorkspace.Catalog(), next)
	if err != nil {
		return fmt.Errorf("daemon: build model catalog: %w", err)
	}
	sessionCatalog := catalog
	if r.sessionOwner != r {
		sessionCatalog, err = models.Build(sessionWorkspace.Catalog(), next)
		if err != nil {
			return fmt.Errorf("daemon: build session model catalog: %w", err)
		}
	}
	commitWorkspace()
	if commitSession != nil {
		commitSession()
	}
	r.gw.set(next)
	if r.sessionOwner != r {
		r.sessionOwner.gw.set(next)
	}
	r.catalog.set(catalog)
	if r.sessionOwner != r {
		r.sessionOwner.catalog.set(sessionCatalog)
	}
	r.reloadMCP(ctx, sessionWorkspace)
	r.RefreshSessions()
	return nil
}

// reloadMCP reconfigures the root workspace's MCP manager with the reloaded
// environment and render context, then reconciles connections with the manifests
// now on disk. Reloading replaces the tool registry, so the reconciliation also
// re-registers the tools of servers that stayed connected.
func (r *workspaceRuntime) reloadMCP(ctx context.Context, rootWorkspace *workspace.Workspace) {
	owner := r.sessionOwner
	if owner == nil {
		owner = r
	}
	if owner.mcpManager == nil {
		return
	}
	owner.mcpManager.SetRuntimeContext(rootWorkspace.Env(), rootWorkspace.RenderOptions())
	owner.mcpManager.Sync(ctx)
}

func (r *workspaceRuntime) Close() error {
	if r.sessionOwner != nil && r.sessionOwner != r {
		return r.sessionOwner.Close()
	}
	if r.bgCancel != nil {
		r.bgCancel()
	}
	if r.sessions != nil {
		for _, sess := range r.sessions.Snapshot().All() {
			_ = sess.Close()
		}
	}
	r.wg.Wait()
	if r.ws != nil {
		r.ws.SetMCPService(nil)
	}
	if r.mcpManager != nil {
		if err := r.mcpManager.Close(); err != nil {
			slog.Warn("daemon: close MCP manager", "workspace", r.dir, "error", err)
		}
	}
	var dbErr error
	if r.db != nil {
		dbErr = r.db.Close()
	}
	return dbErr
}

func (d *Daemon) workspace(ctx context.Context, dir string) (*workspaceRuntime, error) {
	cleanDir, err := fsutils.CleanPath(dir)
	if err != nil {
		if errors.Is(err, fsutils.ErrEmptyPath) {
			return nil, ErrEmptyWorkspace
		}
		return nil, fmt.Errorf("daemon: resolve workspace path %q: %w", dir, err)
	}

	d.wsMu.Lock()
	if d.closed {
		d.wsMu.Unlock()
		return nil, errors.New("daemon: server is closed")
	}
	if rt, ok := d.workspaces[cleanDir]; ok {
		d.wsMu.Unlock()
		return rt, nil
	}
	d.wsMu.Unlock()

	trusted, fpChanged, _ := workspace.IsTrusted(cleanDir)
	openOpts := []workspace.Option{workspace.WithBuiltinFS(builtin.FS())}
	if !trusted || fpChanged {
		openOpts = append(openOpts, workspace.WithRestrictedMode(true))
	}

	ws, err := workspace.Open(ctx, cleanDir, openOpts...)
	if err != nil {
		return nil, fmt.Errorf("daemon: open workspace %q: %w", cleanDir, err)
	}
	rootDir := ws.RootDir()

	d.wsMu.Lock()
	if d.closed {
		d.wsMu.Unlock()
		return nil, errors.New("daemon: server is closed")
	}
	d.dirToRoot[cleanDir] = rootDir
	if rt, ok := d.workspaces[cleanDir]; ok {
		d.wsMu.Unlock()
		return rt, nil
	}

	var db *sql.DB
	var permMgr permissions.Manager
	owner := &workspaceRuntime{catalog: &catalogSlot{}, gw: &gatewaySlot{}}
	reloadMu := &sync.RWMutex{}
	for _, existing := range d.workspaces {
		if existing.ws.RootDir() == rootDir {
			db = existing.db
			permMgr = existing.perms
			owner = existing.sessionOwner
			reloadMu = existing.reloadMu
			break
		}
	}

	if owner.sessionStore == nil {
		gwCfg := buildGatewayConfig(ws, recorderOf(d.usage))
		gw, err := d.cfg.GatewayPool.Acquire(ctx, gwCfg)
		if err != nil {
			d.wsMu.Unlock()
			return nil, fmt.Errorf("daemon: acquire gateway for %q: %w", rootDir, err)
		}
		owner.gw.set(gw)

		wsDataDir, err := xdg.WorkspaceDir(rootDir)
		if err != nil {
			d.wsMu.Unlock()
			return nil, fmt.Errorf("daemon: resolve workspace data dir for %q: %w", rootDir, err)
		}

		dbPath := filepath.Join(wsDataDir, "tasksmith.db")
		db, err = sqlite.Open(ctx, dbPath)
		if err != nil {
			d.wsMu.Unlock()
			return nil, fmt.Errorf("daemon: open sqlite db %q: %w", dbPath, err)
		}

		if err := session.Migrate(ctx, db); err != nil {
			_ = db.Close()
			d.wsMu.Unlock()
			return nil, fmt.Errorf("daemon: migrate session schema for %q: %w", rootDir, err)
		}
		if err := timeline.Migrate(ctx, db); err != nil {
			_ = db.Close()
			d.wsMu.Unlock()
			return nil, fmt.Errorf("daemon: migrate timeline schema for %q: %w", rootDir, err)
		}
		if err := journal.Migrate(ctx, db); err != nil {
			_ = db.Close()
			d.wsMu.Unlock()
			return nil, fmt.Errorf("daemon: migrate journal schema for %q: %w", rootDir, err)
		}

		permMgr, err = permissions.New(permissions.Config{Workspace: ws})
		if err != nil {
			_ = db.Close()
			d.wsMu.Unlock()
			return nil, fmt.Errorf("daemon: initialize permissions manager for %q: %w", rootDir, err)
		}

		sessionStore, err := session.NewStore(ctx, db)
		if err != nil {
			_ = db.Close()
			d.wsMu.Unlock()
			return nil, fmt.Errorf("daemon: initialize session store for %q: %w", rootDir, err)
		}

		sessions := memdb.New(func(s *session.Session) string { return s.ID })
		bgCtx, bgCancel := context.WithCancel(context.Background())

		owner.db = db
		owner.sessionStore = sessionStore
		owner.sessions = sessions
		owner.bgCtx = bgCtx
		owner.bgCancel = bgCancel
		owner.perms = permMgr
		owner.reloadMu = reloadMu
		owner.publisher = d.broker
		owner.logger = d.logger
		owner.metrics = d.usage

		// The manager begins connecting in the background so opening a workspace is
		// never blocked by a slow or unreachable MCP server.
		mgr, err := mcp.New(mcp.Options{
			Definer:         ws,
			Syncer:          ws,
			ReloadWorkspace: func(ctx context.Context) error { return owner.Reload(ctx) },
			Env:             ws.Env(),
			RenderOptions:   ws.RenderOptions(),
			AutoConnect:     !d.cfg.DisableMCPAutoConnect,
			// A server finishing its background connect makes new tools callable, while
			// open sessions hold a container built before that happened. Rebuilding them
			// here is what makes "the server is up" and "the agent can call it" the same
			// moment. The write lock mirrors what a workspace reload holds while
			// refreshing sessions, so a refresh never swaps a session's tooling out from
			// under a turn in flight.
			OnDiscover: func() {
				owner.reloadMu.Lock()
				defer owner.reloadMu.Unlock()
				owner.RefreshSessions()
			},
		})
		if err != nil {
			_ = db.Close()
			d.wsMu.Unlock()
			return nil, fmt.Errorf("daemon: initialize MCP manager for %q: %w", rootDir, err)
		}
		owner.mcpManager = mgr
		ws.SetMCPService(mgr)
		mgr.Sync(context.WithoutCancel(ctx))
	}

	catalog, err := models.Build(ws.Catalog(), owner.gw.get())
	if err != nil {
		d.wsMu.Unlock()
		return nil, fmt.Errorf("daemon: build model catalog for %q: %w", rootDir, err)
	}

	rt := owner
	if owner.catalog.get() != nil {
		rt = &workspaceRuntime{
			catalog:      &catalogSlot{},
			dir:          cleanDir,
			ws:           ws,
			sessionOwner: owner,
			reloadMu:     reloadMu,
			gw:           owner.gw,
			pool:         d.cfg.GatewayPool,
			metrics:      d.usage,
			perms:        permMgr,
			publisher:    d.broker,
			logger:       d.logger,
			db:           db,
		}
	} else {
		rt.dir = cleanDir
		rt.ws = ws
		rt.sessionOwner = owner
		rt.reloadMu = reloadMu
		rt.gw = owner.gw
		rt.pool = d.cfg.GatewayPool
		rt.metrics = d.usage
		rt.perms = permMgr
		rt.sessionStore = owner.sessionStore
		rt.sessions = owner.sessions
		rt.bgCtx = owner.bgCtx
		rt.bgCancel = owner.bgCancel
		rt.publisher = d.broker
		rt.logger = d.logger
		rt.db = db
	}
	rt.catalog.set(catalog)
	d.workspaces[cleanDir] = rt
	d.wsMu.Unlock()

	return rt, nil
}

// Workspaces returns sorted canonical root directories of all open workspace runtimes.
func (d *Daemon) Workspaces() []string {
	d.wsMu.Lock()
	defer d.wsMu.Unlock()

	seen := make(map[string]struct{})
	roots := make([]string, 0, len(d.workspaces))
	for _, rt := range d.workspaces {
		root := rt.ws.RootDir()
		if _, ok := seen[root]; !ok {
			seen[root] = struct{}{}
			roots = append(roots, root)
		}
	}
	sort.Strings(roots)
	return roots
}

func (d *Daemon) reloadAllWorkspaces(ctx context.Context) {
	d.wsMu.Lock()
	seen := make(map[*workspaceRuntime]struct{}, len(d.workspaces))
	runtimes := make([]*workspaceRuntime, 0, len(d.workspaces))
	for _, rt := range d.workspaces {
		if _, ok := seen[rt]; !ok {
			seen[rt] = struct{}{}
			runtimes = append(runtimes, rt)
		}
	}
	d.wsMu.Unlock()

	for _, rt := range runtimes {
		if err := rt.Reload(ctx); err != nil {
			d.logger.Warn("failed to reload workspace after provider update",
				log.String("workspace", rt.dir),
				log.String("error", err.Error()),
			)
		}
	}
}

func (d *Daemon) closeWorkspaces() error {
	d.wsMu.Lock()
	runtimes := make([]*workspaceRuntime, 0, len(d.workspaces))
	for _, rt := range d.workspaces {
		runtimes = append(runtimes, rt)
	}
	clear(d.workspaces)
	clear(d.dirToRoot)
	d.wsMu.Unlock()

	seen := make(map[*workspaceRuntime]struct{})
	var errs []error
	for _, rt := range runtimes {
		owner := rt.SessionRuntime()
		if owner == nil {
			owner = rt
		}
		if _, ok := seen[owner]; !ok {
			seen[owner] = struct{}{}
			if err := owner.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func buildGatewayConfig(ws *workspace.Workspace, rec metrics.Recorder) gateway.Config {
	wsEnv := ws.Env()
	wsProviders := ws.Catalog().Providers()
	providers := make([]gateway.ProviderConfig, 0, len(wsProviders))
	for _, p := range wsProviders {
		providers = append(providers, toGatewayProvider(p))
	}

	return gateway.Config{
		Providers: providers,
		Env:       wsEnv,
		Metrics:   rec,
	}
}

func toGatewayProvider(p *warp.ModelProvider) gateway.ProviderConfig {
	var auth *gateway.AuthConfig
	if p.Spec.Auth != nil {
		auth = &gateway.AuthConfig{
			Type:   gateway.AuthType(p.Spec.Auth.Type),
			Header: p.Spec.Auth.Header,
			Env:    p.Spec.Auth.Env,
			File:   resolveCredentialFilePath(p.Spec.Auth.File, p.GetDir()),
		}
	}

	return gateway.ProviderConfig{
		Name:     p.Metadata.Name,
		Type:     p.Spec.Type,
		Endpoint: p.Spec.Endpoint,
		Auth:     auth,
		Headers:  p.Spec.Headers,
		Config:   p.Spec.Config,
	}
}

// resolveCredentialFilePath turns a manifest auth.file value into an absolute
// path: "~" expands to the home directory, and a relative path is resolved next
// to the directory the provider manifest lives in, so "file: ./token" reads the
// token shipped alongside that manifest.
func resolveCredentialFilePath(file, providerDir string) string {
	if strings.TrimSpace(file) == "" {
		return ""
	}
	return fsutils.ResolveConfigPath(file, providerDir)
}

func recorderOf(s metrics.Service) metrics.Recorder {
	if s == nil {
		return nil
	}
	return s
}
