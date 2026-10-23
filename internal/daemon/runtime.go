package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

type activeRun struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

type workspaceRuntime struct {
	dir       string
	workspace *workspace.Workspace
	sandbox   sandbox.Sandbox
	gateway   gateway.Gateway
	db        *sql.DB
	manager   *session.Manager
	hub       *hub
	logger    log.Interface

	bgCtx    context.Context
	bgCancel context.CancelFunc
	wg       sync.WaitGroup

	mu         sync.Mutex
	activeRuns map[string]*activeRun
}

func newWorkspaceRuntime(ctx context.Context, ws *workspace.Workspace, pool gateway.Pool, h *hub, logger log.Interface) (*workspaceRuntime, error) {
	rootDir := ws.RootDir()

	sb, err := sandbox.New(sandbox.Config{
		Mode:    sandbox.ModeLocal,
		WorkDir: rootDir,
		Env:     ws.Env(),
		Network: sandbox.NetworkPolicy{
			AllowLoopback: true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("daemon: initialize sandbox for %q: %w", rootDir, err)
	}

	gwCfg := buildGatewayConfig(ws)
	gw, err := pool.Acquire(ctx, gwCfg)
	if err != nil {
		_ = sb.Close()
		return nil, fmt.Errorf("daemon: acquire gateway for %q: %w", rootDir, err)
	}

	wsDataDir, err := xdg.WorkspaceDir(rootDir)
	if err != nil {
		_ = sb.Close()
		return nil, fmt.Errorf("daemon: resolve workspace data dir for %q: %w", rootDir, err)
	}

	dbPath := filepath.Join(wsDataDir, "tasksmith.db")
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		_ = sb.Close()
		return nil, fmt.Errorf("daemon: open sqlite db %q: %w", dbPath, err)
	}

	pub := &workspacePublisher{
		workspace: rootDir,
		hub:       h,
	}

	mgr, err := session.New(session.Config{
		DB:        db,
		Workspace: ws,
		Gateway:   gw,
		Sandbox:   sb,
		Publisher: pub,
		Logger:    logger,
	})
	if err != nil {
		_ = db.Close()
		_ = sb.Close()
		return nil, fmt.Errorf("daemon: initialize session manager for %q: %w", rootDir, err)
	}

	bgCtx, bgCancel := context.WithCancel(context.Background())

	return &workspaceRuntime{
		dir:        rootDir,
		workspace:  ws,
		sandbox:    sb,
		gateway:    gw,
		db:         db,
		manager:    mgr,
		hub:        h,
		logger:     logger,
		bgCtx:      bgCtx,
		bgCancel:   bgCancel,
		activeRuns: make(map[string]*activeRun),
	}, nil
}

func (rt *workspaceRuntime) sessionStatus(ctx context.Context, sessionID string) api.SessionStatus {
	rt.mu.Lock()
	_, running := rt.activeRuns[sessionID]
	rt.mu.Unlock()
	if running {
		return api.StatusRunning
	}

	perms, err := rt.manager.PendingPermissions(ctx, sessionID)
	if err == nil && len(perms) > 0 {
		return api.StatusWaitingPermission
	}

	return api.StatusIdle
}

func (rt *workspaceRuntime) startTurn(ctx context.Context, sessionID string, input message.Message, wait bool) error {
	sess, err := rt.manager.Get(ctx, sessionID)
	if err != nil {
		return err
	}

	run, runCtx, err := rt.registerRun(sess.ID)
	if err != nil {
		return err
	}

	go func() {
		var runErr error
		defer func() {
			rt.finishRun(sess.ID, run, runErr)
		}()
		if _, err := rt.manager.Send(runCtx, sess.ID, input); err != nil {
			runErr = err
			rt.publishRunError(sess.ID, err)
		}
	}()

	if !wait {
		return nil
	}
	return waitForRun(ctx, run)
}

func (rt *workspaceRuntime) respondPermission(ctx context.Context, sessionID string, decision tools.PermissionDecision, wait bool) error {
	sess, err := rt.manager.Get(ctx, sessionID)
	if err != nil {
		return err
	}

	rt.mu.Lock()
	_, running := rt.activeRuns[sess.ID]
	rt.mu.Unlock()
	if running {
		return ErrSessionRunning
	}

	ready, err := rt.manager.RecordDecision(ctx, sess.ID, decision)
	if err != nil || !ready {
		return err
	}

	run, runCtx, err := rt.registerRun(sess.ID)
	if err != nil {
		return err
	}

	go func() {
		var runErr error
		defer func() {
			rt.finishRun(sess.ID, run, runErr)
		}()
		if _, err := rt.manager.Resume(runCtx, sess.ID); err != nil {
			runErr = err
			rt.publishRunError(sess.ID, err)
		}
	}()

	if !wait {
		return nil
	}
	return waitForRun(ctx, run)
}

func (rt *workspaceRuntime) cancelTurn(ctx context.Context, sessionID string) error {
	sess, err := rt.manager.Get(ctx, sessionID)
	if err != nil {
		return err
	}

	rt.mu.Lock()
	run := rt.activeRuns[sess.ID]
	rt.mu.Unlock()
	if run == nil {
		return nil
	}

	run.cancel()
	select {
	case <-run.done:
	case <-ctx.Done():
	}
	return nil
}

func (rt *workspaceRuntime) registerRun(sessionID string) (*activeRun, context.Context, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if _, exists := rt.activeRuns[sessionID]; exists {
		return nil, nil, ErrSessionRunning
	}

	runCtx, cancel := context.WithCancel(rt.bgCtx)
	run := &activeRun{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	rt.activeRuns[sessionID] = run
	rt.wg.Add(1)
	return run, runCtx, nil
}

func (rt *workspaceRuntime) finishRun(sessionID string, run *activeRun, err error) {
	run.err = err
	run.cancel()
	rt.mu.Lock()
	if rt.activeRuns[sessionID] == run {
		delete(rt.activeRuns, sessionID)
	}
	rt.mu.Unlock()
	close(run.done)
	rt.wg.Done()
}

func (rt *workspaceRuntime) publishRunError(sessionID string, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	rt.hub.publish(api.Event{
		Type:      api.EventError,
		Workspace: rt.dir,
		SessionID: sessionID,
		Error:     err.Error(),
	})
}

func (rt *workspaceRuntime) close() error {
	rt.bgCancel()
	rt.wg.Wait()
	sbErr := rt.sandbox.Close()
	dbErr := rt.db.Close()
	return errors.Join(sbErr, dbErr)
}

func waitForRun(ctx context.Context, run *activeRun) error {
	select {
	case <-run.done:
		if run.err != nil && !errors.Is(run.err, context.Canceled) {
			return run.err
		}
		return nil
	case <-ctx.Done():
		return nil
	}
}

func buildGatewayConfig(ws *workspace.Workspace) gateway.Config {
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
