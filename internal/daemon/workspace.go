package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/guard"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/memdb"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	providerauth "github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
	"github.com/masterkeysrd/tasksmith/internal/daemon/journal"
	"github.com/masterkeysrd/tasksmith/internal/daemon/mcp"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandboxcfg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
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
	authObserver gateway.AuthObserver
	metrics      metrics.Service
	perms        permissions.Manager
	sandboxCfg   sandboxcfg.Manager
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
	mcpManager       *mcp.Manager
	siblingsMu       sync.RWMutex
	siblings         []*workspaceRuntime
	resolveWorkspace func(ctx context.Context, dir string) (*workspaceRuntime, error)
	fileIndexMu      sync.Mutex
	fileIndex        *std.FileIndex
	indexSbx         sandbox.Sandbox
}

func (r *workspaceRuntime) Dir() string                       { return r.dir }
func (r *workspaceRuntime) Workspace() *workspace.Workspace   { return r.ws }
func (r *workspaceRuntime) Gateway() gateway.Gateway          { return r.gw.get() }
func (r *workspaceRuntime) Catalog() *models.Catalog          { return r.catalog.get() }
func (r *workspaceRuntime) Permissions() permissions.Manager  { return r.perms }
func (r *workspaceRuntime) SandboxConfig() sandboxcfg.Manager { return r.sandboxCfg }
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

func (r *workspaceRuntime) addSibling(rt *workspaceRuntime) {
	owner := r.sessionOwnerOrSelf()
	owner.siblingsMu.Lock()
	defer owner.siblingsMu.Unlock()
	if !slices.Contains(owner.siblings, rt) {
		owner.siblings = append(owner.siblings, rt)
	}
}

func (r *workspaceRuntime) snapshotSiblings() []*workspaceRuntime {
	owner := r.sessionOwnerOrSelf()
	owner.siblingsMu.RLock()
	defer owner.siblingsMu.RUnlock()
	if len(owner.siblings) == 0 {
		return []*workspaceRuntime{owner}
	}
	return slices.Clone(owner.siblings)
}

type multiWorkspaceSyncer struct {
	owner *workspaceRuntime
}

func (s *multiWorkspaceSyncer) SyncMCPTools(def *warp.MCP, rawTools []*warp.Tool) error {
	var errs []error
	for _, sib := range s.owner.snapshotSiblings() {
		if sib.ws == nil {
			continue
		}
		if err := sib.ws.SyncMCPTools(def, rawTools); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (r *workspaceRuntime) Reload(ctx context.Context) error {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	return r.reloadLocked(ctx)
}

func (r *workspaceRuntime) reloadLocked(ctx context.Context) error {
	owner := r.sessionOwnerOrSelf()
	siblings := owner.snapshotSiblings()

	type preparedReload struct {
		rt      *workspaceRuntime
		ws      *workspace.Workspace
		commit  func()
		gw      gateway.Gateway
		catalog *models.Catalog
	}
	prepared := make([]preparedReload, 0, len(siblings))
	for _, sib := range siblings {
		nextWS, commitWS, err := sib.ws.PrepareReload(ctx)
		if err != nil {
			return err
		}
		nextGW, err := sib.pool.Acquire(ctx, buildGatewayConfig(nextWS, recorderOf(sib.metrics), sib.authObserver))
		if err != nil {
			return err
		}
		nextCat, err := models.BuildContext(ctx, nextWS.Catalog(), nextGW)
		if err != nil {
			return fmt.Errorf("daemon: build model catalog: %w", err)
		}
		prepared = append(prepared, preparedReload{
			rt:      sib,
			ws:      nextWS,
			commit:  commitWS,
			gw:      nextGW,
			catalog: nextCat,
		})
	}

	for _, p := range prepared {
		p.commit()
		p.rt.gw.set(p.gw)
		p.rt.catalog.set(p.catalog)
		if p.rt.sandboxCfg != nil {
			p.rt.sandboxCfg.SetRestricted(p.ws.IsRestricted())
		}
		p.rt.fileIndexMu.Lock()
		if p.rt.fileIndex != nil {
			p.rt.fileIndex.SetAllowExec(p.rt.ws != nil && !p.rt.ws.IsRestricted())
			p.rt.fileIndex.Invalidate()
		}
		p.rt.fileIndexMu.Unlock()
	}

	owner.reloadMCP(ctx, owner.ws)
	owner.RefreshSessions()
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

func (r *workspaceRuntime) closeFileIndex() {
	r.fileIndexMu.Lock()
	if r.fileIndex != nil {
		_ = r.fileIndex.Close()
		r.fileIndex = nil
	}
	if r.indexSbx != nil {
		_ = r.indexSbx.Close()
		r.indexSbx = nil
	}
	r.fileIndexMu.Unlock()
}

func (r *workspaceRuntime) Close() error {
	if r.sessionOwner != nil && r.sessionOwner != r {
		r.closeFileIndex()
		return r.sessionOwner.Close()
	}
	if r.bgCancel != nil {
		r.bgCancel()
	}
	if r.mcpManager != nil {
		if err := r.mcpManager.Close(); err != nil {
			slog.Warn("daemon: close MCP manager", "workspace", r.dir, "error", err)
		}
	}
	if r.sessions != nil {
		r.reloadMu.Lock()
		r.createMu.Lock()
		sessions := r.sessions.Snapshot().All()
		r.sessions.Replace()
		r.createMu.Unlock()
		r.reloadMu.Unlock()
		for _, sess := range sessions {
			_ = sess.Close()
		}
	}
	r.wg.Wait()
	for _, sib := range r.snapshotSiblings() {
		if sib.ws != nil {
			sib.ws.SetMCPService(nil)
		}
	}
	r.closeFileIndex()
	var dbErr error
	if r.db != nil {
		dbErr = r.db.Close()
	}
	return dbErr
}

func (r *workspaceRuntime) getFileIndex() (*std.FileIndex, error) {
	r.fileIndexMu.Lock()
	defer r.fileIndexMu.Unlock()
	if r.fileIndex != nil {
		r.fileIndex.SetAllowExec(r.ws != nil && !r.ws.IsRestricted())
		return r.fileIndex, nil
	}

	// The indexer executes git/rg inside the repository, so it gets the same
	// kernel backend, credential masking, and protected-path rules as sessions,
	// plus the user's configured read posture so rg/git indexing never reads
	// what a session is forbidden to read. Write lists are skipped: the indexer
	// runs read-only.
	fsPolicy := sandbox.FilesystemPolicy{
		DeniedReadPaths:  guard.HostCredentialPaths(r.dir),
		DeniedWritePaths: guard.ProtectedWorkspaceWritePaths(r.dir),
	}
	if cfgMgr := r.sandboxCfg; cfgMgr != nil {
		if eff, err := cfgMgr.EffectiveFor(r.dir); err == nil {
			fsPolicy.DeniedReadPaths = append(fsPolicy.DeniedReadPaths, eff.DeniedReadPaths...)
			fsPolicy.AllowReadPaths = eff.AllowReadPaths
		} else {
			r.logger.Warn("workspace: effective sandbox settings for indexing failed",
				log.String("work_dir", r.dir),
				log.String("error", err.Error()),
			)
		}
	}
	sbx, err := sandbox.New(sandbox.Config{
		WorkDir:    r.dir,
		Mode:       sandbox.DefaultMode(),
		ReadOnly:   true,
		Filesystem: fsPolicy,
		Network:    sandbox.NetworkPolicy{Disabled: true},
	})
	if err != nil {
		return nil, fmt.Errorf("daemon: create workspace sandbox for indexing: %w", err)
	}

	stdInst, err := std.New(std.Config{
		Sandbox: sbx,
		Tracker: std.NewMemoryTracker(),
		Logger:  r.logger,
	})
	if err != nil {
		_ = sbx.Close()
		return nil, fmt.Errorf("daemon: create workspace std for indexing: %w", err)
	}

	idx, err := std.NewFileIndex(std.FileIndexOptions{
		Std:       stdInst,
		AllowExec: r.ws != nil && !r.ws.IsRestricted(),
		Lifecycle: r.sessionOwnerOrSelf().bgCtx,
	})
	if err != nil {
		_ = sbx.Close()
		return nil, fmt.Errorf("daemon: create workspace file index: %w", err)
	}

	r.indexSbx = sbx
	r.fileIndex = idx
	return idx, nil
}

type workspaceOpen struct {
	done chan struct{}
	rt   *workspaceRuntime
	err  error
}

type workspaceRoot struct {
	mu    sync.Mutex
	owner *workspaceRuntime
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
	if d.closed.Load() {
		d.wsMu.Unlock()
		return nil, errors.New("daemon: server is closed")
	}
	if rt, ok := d.workspaces[cleanDir]; ok {
		d.wsMu.Unlock()
		return rt, nil
	}

	op := d.opening[cleanDir]
	if op == nil {
		op = &workspaceOpen{done: make(chan struct{})}
		d.opening[cleanDir] = op
		go func() {
			rt, openErr := d.openWorkspace(d.openCtx, cleanDir)
			d.wsMu.Lock()
			closed := d.closed.Load()
			if openErr == nil {
				d.workspaces[cleanDir] = rt
				if !closed {
					d.publishWorkspacesLocked()
				}
			}
			if closed {
				rt = nil
				openErr = errors.New("daemon: server is closed")
			}
			op.rt, op.err = rt, openErr
			delete(d.opening, cleanDir)
			close(op.done)
			d.wsMu.Unlock()
		}()
	}
	d.wsMu.Unlock()

	select {
	case <-op.done:
		return op.rt, op.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (d *Daemon) openWorkspace(ctx context.Context, cleanDir string) (*workspaceRuntime, error) {
	ws, err := workspace.Open(ctx, cleanDir, workspace.WithBuiltinFS(builtin.FS()), workspace.WithWorkspaceTrust())
	if err != nil {
		return nil, fmt.Errorf("daemon: open workspace %q: %w", cleanDir, err)
	}
	rootDir := ws.RootDir()
	d.wsMu.Lock()
	root := d.roots[rootDir]
	if root == nil {
		root = &workspaceRoot{}
		d.roots[rootDir] = root
	}
	d.wsMu.Unlock()
	root.mu.Lock()
	defer root.mu.Unlock()

	var db *sql.DB
	var permMgr permissions.Manager
	var sandboxMgr sandboxcfg.Manager
	owner := &workspaceRuntime{catalog: &catalogSlot{}, gw: &gatewaySlot{}}
	reloadMu := &sync.RWMutex{}
	if existing := root.owner; existing != nil {
		db = existing.db
		permMgr = existing.perms
		owner = existing
		reloadMu = existing.reloadMu
	}

	gwCfg := buildGatewayConfig(ws, recorderOf(d.usage), d.auth)
	gw, err := d.cfg.GatewayPool.Acquire(ctx, gwCfg)
	if err != nil {
		return nil, fmt.Errorf("daemon: acquire gateway for %q: %w", cleanDir, err)
	}
	gwSlot := owner.gw
	if owner.sessionStore != nil {
		gwSlot = &gatewaySlot{}
	}
	gwSlot.set(gw)

	catalog, err := models.BuildContext(ctx, ws.Catalog(), gwSlot.get())
	if err != nil {
		return nil, fmt.Errorf("daemon: build model catalog for %q: %w", cleanDir, err)
	}

	justStartedMCP := false
	if owner.sessionStore == nil {
		wsDataDir, err := xdg.WorkspaceDir(rootDir)
		if err != nil {
			return nil, fmt.Errorf("daemon: resolve workspace data dir for %q: %w", rootDir, err)
		}

		dbPath := filepath.Join(wsDataDir, "tasksmith.db")
		db, err = sqlite.Open(ctx, dbPath)
		if err != nil {
			return nil, fmt.Errorf("daemon: open sqlite db %q: %w", dbPath, err)
		}

		if err := session.Migrate(ctx, db); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("daemon: migrate session schema for %q: %w", rootDir, err)
		}
		if err := timeline.Migrate(ctx, db); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("daemon: migrate timeline schema for %q: %w", rootDir, err)
		}
		if err := journal.Migrate(ctx, db); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("daemon: migrate journal schema for %q: %w", rootDir, err)
		}

		permMgr, err = permissions.New(permissions.Config{Workspace: ws})
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("daemon: initialize permissions manager for %q: %w", rootDir, err)
		}

		sandboxMgr, err = sandboxcfg.New(sandboxcfg.Config{
			WorkDir:    rootDir,
			Restricted: ws.IsRestricted(),
		})
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("daemon: initialize sandbox settings manager for %q: %w", rootDir, err)
		}

		sessionStore, err := session.NewStore(ctx, db)
		if err != nil {
			_ = db.Close()
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
		owner.sandboxCfg = sandboxMgr
		owner.reloadMu = reloadMu
		owner.publisher = d.broker
		owner.logger = d.logger
		owner.metrics = d.usage
		owner.authObserver = d.auth

		// The manager begins connecting in the background so opening a workspace is
		// never blocked by a slow or unreachable MCP server.
		mgr, err := mcp.New(mcp.Options{
			Definer:         ws,
			Syncer:          &multiWorkspaceSyncer{owner: owner},
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
			bgCancel()
			_ = db.Close()
			return nil, fmt.Errorf("daemon: initialize MCP manager for %q: %w", rootDir, err)
		}
		owner.mcpManager = mgr
		ws.SetMCPService(mgr)
		justStartedMCP = true
	} else {
		sandboxMgr = owner.sandboxCfg.WithRestricted(ws.IsRestricted())
		if owner.mcpManager != nil {
			ws.SetMCPService(owner.mcpManager)
		}
	}

	rt := owner
	if owner.catalog.get() != nil {
		rt = &workspaceRuntime{
			catalog:          &catalogSlot{},
			dir:              cleanDir,
			ws:               ws,
			sessionOwner:     owner,
			reloadMu:         reloadMu,
			gw:               gwSlot,
			pool:             d.cfg.GatewayPool,
			authObserver:     d.auth,
			metrics:          d.usage,
			perms:            permMgr,
			sandboxCfg:       sandboxMgr,
			publisher:        d.broker,
			logger:           d.logger,
			db:               db,
			resolveWorkspace: d.workspace,
		}
	} else {
		rt.dir = cleanDir
		rt.ws = ws
		rt.sessionOwner = owner
		rt.reloadMu = reloadMu
		rt.gw = gwSlot
		rt.pool = d.cfg.GatewayPool
		rt.authObserver = d.auth
		rt.metrics = d.usage
		rt.perms = permMgr
		rt.sandboxCfg = sandboxMgr
		rt.sessionStore = owner.sessionStore
		rt.sessions = owner.sessions
		rt.bgCtx = owner.bgCtx
		rt.bgCancel = owner.bgCancel
		rt.publisher = d.broker
		rt.logger = d.logger
		rt.db = db
		rt.resolveWorkspace = d.workspace
	}
	rt.catalog.set(catalog)
	owner.addSibling(rt)
	if !justStartedMCP && owner.mcpManager != nil {
		if err := owner.mcpManager.SyncTo(ws); err != nil {
			d.logger.Warn("daemon: sync discovered MCP tools to sibling workspace",
				log.String("workspace", cleanDir),
				log.String("error", err.Error()),
			)
		}
	}
	root.owner = owner

	if justStartedMCP && owner.mcpManager != nil {
		owner.mcpManager.Sync(context.WithoutCancel(ctx))
	}

	return rt, nil
}

// Workspaces returns sorted canonical root directories of all open workspace runtimes.
func (d *Daemon) Workspaces() []string {
	return slices.Clone(*d.workspaceNames.Load())
}

func (d *Daemon) publishWorkspacesLocked() {
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
	d.workspaceNames.Store(&roots)
}

func (d *Daemon) reloadAllWorkspaces(ctx context.Context) {
	if err := d.reloadWorkspacesUnder(ctx, ""); err != nil {
		d.logger.Warn("failed to reload workspaces", log.String("error", err.Error()))
	}
}

func (d *Daemon) reloadWorkspacesUnder(ctx context.Context, dir string) error {
	d.wsMu.Lock()
	seen := make(map[*workspaceRuntime]struct{}, len(d.workspaces))
	owners := make([]*workspaceRuntime, 0, len(d.workspaces))
	for _, rt := range d.workspaces {
		if dir != "" && !fsutils.IsInsideDir(rt.dir, dir) && !fsutils.IsInsideDir(rt.ws.RootDir(), dir) {
			continue
		}
		owner := rt.sessionOwnerOrSelf()
		if _, ok := seen[owner]; !ok {
			seen[owner] = struct{}{}
			owners = append(owners, owner)
		}
	}
	d.wsMu.Unlock()

	var errs []error
	for _, owner := range owners {
		if err := owner.Reload(ctx); err != nil {
			errs = append(errs, fmt.Errorf("reload %q: %w", owner.dir, err))
		}
	}
	return errors.Join(errs...)
}

func (d *Daemon) closeWorkspaces() error {
	d.wsMu.Lock()
	for len(d.opening) > 0 {
		ops := slices.Collect(maps.Values(d.opening))
		d.wsMu.Unlock()
		for _, op := range ops {
			<-op.done
		}
		d.wsMu.Lock()
	}
	runtimes := make([]*workspaceRuntime, 0, len(d.workspaces))
	for _, rt := range d.workspaces {
		runtimes = append(runtimes, rt)
	}
	clear(d.workspaces)
	clear(d.roots)
	d.publishWorkspacesLocked()
	d.wsMu.Unlock()

	seen := make(map[*workspaceRuntime]struct{})
	var errs []error
	for _, rt := range runtimes {
		rt.closeFileIndex()
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

func buildGatewayConfig(ws *workspace.Workspace, rec metrics.Recorder, authObserver gateway.AuthObserver) gateway.Config {
	wsEnv := ws.Env()
	wsProviders := ws.Catalog().Providers()
	providers := make([]gateway.ProviderConfig, 0, len(wsProviders))
	seen := make(map[string]struct{}, len(wsProviders))
	for _, p := range wsProviders {
		if p == nil {
			continue
		}
		name := strings.TrimSpace(p.Metadata.Name)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		providers = append(providers, toGatewayProvider(p))
	}

	return gateway.Config{
		Providers:    providers,
		Env:          wsEnv,
		Metrics:      rec,
		AuthObserver: authObserver,
	}
}

func toGatewayProvider(p *warp.ModelProvider) gateway.ProviderConfig {
	var auth *providerauth.Config
	if p.Spec.Auth != nil {
		auth = toGatewayAuth(p.Spec.Auth, p.GetDir())
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

func toGatewayAuth(pa *warp.ProviderAuth, providerDir string) *providerauth.Config {
	auth := &providerauth.Config{
		Type:   providerauth.Type(pa.Type),
		Header: pa.Header,
		Token:  parseCredentialSource(pa.Token, providerDir),
	}
	if strings.EqualFold(strings.TrimSpace(pa.Type), warp.AuthTypeBasic) && pa.Token == "" {
		auth.Username = strings.TrimSpace(pa.Username)
		auth.Password = parseCredentialSource(pa.Password, providerDir)
	}
	return auth
}

func parseCredentialSource(rawExpr, providerDir string) *providerauth.CredentialSource {
	expr, ok := warp.ParseExpression(strings.TrimSpace(rawExpr))
	if !ok {
		return nil
	}
	switch expr.Scheme {
	case "env":
		if envName := strings.TrimSpace(expr.Target); envName != "" {
			return &providerauth.CredentialSource{Env: envName}
		}
	case "file":
		if filePath := resolveCredentialFilePath(expr.Target, providerDir); filePath != "" {
			return &providerauth.CredentialSource{File: filePath}
		}
	}
	return nil
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
