package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

type catalogSlot struct {
	mu    sync.RWMutex
	value models.Catalog
}

func (s *catalogSlot) get() models.Catalog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}

func (s *catalogSlot) set(c models.Catalog) {
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
	sessions     session.Manager
	db           *sql.DB
}

func (r *workspaceRuntime) Dir() string                       { return r.dir }
func (r *workspaceRuntime) Workspace() *workspace.Workspace   { return r.ws }
func (r *workspaceRuntime) Gateway() gateway.Gateway          { return r.gw.get() }
func (r *workspaceRuntime) Catalog() models.Catalog           { return r.catalog.get() }
func (r *workspaceRuntime) Permissions() permissions.Manager  { return r.perms }
func (r *workspaceRuntime) Sessions() session.Manager         { return r.sessions }
func (r *workspaceRuntime) SessionRuntime() *workspaceRuntime { return r.sessionOwner }

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
	r.sessions.Refresh()
	return nil
}

func (r *workspaceRuntime) Close() error {
	sessErr := r.sessions.Close()
	dbErr := r.db.Close()
	return errors.Join(sessErr, dbErr)
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

	ws, err := workspace.Open(ctx, cleanDir, workspace.WithBuiltinFS(builtin.FS()))
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
	var sessMgr session.Manager
	var permMgr permissions.Manager
	owner := &workspaceRuntime{catalog: &catalogSlot{}, gw: &gatewaySlot{}}
	reloadMu := &sync.RWMutex{}
	for _, existing := range d.workspaces {
		if existing.ws.RootDir() == rootDir {
			db = existing.db
			sessMgr = existing.sessions
			permMgr = existing.perms
			owner = existing.sessionOwner
			reloadMu = existing.reloadMu
			break
		}
	}

	if sessMgr == nil {
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

		permMgr, err = permissions.New(permissions.Config{Workspace: ws})
		if err != nil {
			_ = db.Close()
			d.wsMu.Unlock()
			return nil, fmt.Errorf("daemon: initialize permissions manager for %q: %w", rootDir, err)
		}

		sessMgr, err = session.New(session.Config{
			DB:          db,
			Workspace:   ws,
			Gateway:     owner.gw.get,
			Publisher:   d.broker,
			Logger:      d.logger,
			Permissions: permMgr,
			ReloadLock:  reloadMu,
			Catalog:     owner.catalog.get,
			Metrics:     d.usage,
		})
		if err != nil {
			_ = db.Close()
			d.wsMu.Unlock()
			return nil, fmt.Errorf("daemon: initialize session manager for %q: %w", rootDir, err)
		}
	}

	catalog, err := models.Build(ws.Catalog(), owner.gw.get())
	if err != nil {
		d.wsMu.Unlock()
		return nil, fmt.Errorf("daemon: build model catalog for %q: %w", rootDir, err)
	}

	rt := owner
	if owner.catalog.get() != nil {
		rt = &workspaceRuntime{catalog: &catalogSlot{}, gw: owner.gw}
	}
	rt.catalog.set(catalog)
	rt.dir = cleanDir
	rt.ws = ws
	rt.sessionOwner = owner
	rt.reloadMu = reloadMu
	rt.gw = owner.gw
	rt.pool = d.cfg.GatewayPool
	rt.metrics = d.usage
	rt.perms = permMgr
	rt.sessions = sessMgr
	rt.db = db
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

func (d *Daemon) closeWorkspaces() error {
	d.wsMu.Lock()
	runtimes := make([]*workspaceRuntime, 0, len(d.workspaces))
	for _, rt := range d.workspaces {
		runtimes = append(runtimes, rt)
	}
	clear(d.workspaces)
	clear(d.dirToRoot)
	d.wsMu.Unlock()

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
			File:   p.Spec.Auth.File,
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

func recorderOf(s metrics.Service) metrics.Recorder {
	if s == nil {
		return nil
	}
	return s
}
