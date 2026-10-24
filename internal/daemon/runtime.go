package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

type workspaceRuntime struct {
	dir       string
	workspace *workspace.Workspace
	sandbox   sandbox.Sandbox
	gateway   gateway.Gateway
	db        *sql.DB
	manager   *session.Manager
	broker    *broker.Broker
	logger    log.Interface
}

func newWorkspaceRuntime(ctx context.Context, ws *workspace.Workspace, pool gateway.Pool, b *broker.Broker, logger log.Interface) (*workspaceRuntime, error) {
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

	tl, err := timeline.New(ctx, db)
	if err != nil {
		_ = db.Close()
		_ = sb.Close()
		return nil, fmt.Errorf("daemon: initialize timeline for %q: %w", rootDir, err)
	}

	rt := &workspaceRuntime{
		dir:       rootDir,
		workspace: ws,
		sandbox:   sb,
		gateway:   gw,
		db:        db,
		broker:    b,
		logger:    logger,
	}

	mgr, err := session.New(session.Config{
		DB:        db,
		Workspace: ws,
		Gateway:   gw,
		Sandbox:   sb,
		Publisher: b,
		Logger:    logger,
		Timeline:  tl,
	})
	if err != nil {
		_ = db.Close()
		_ = sb.Close()
		return nil, fmt.Errorf("daemon: initialize session manager for %q: %w", rootDir, err)
	}
	rt.manager = mgr

	return rt, nil
}

func (rt *workspaceRuntime) close() error {
	mgrErr := rt.manager.Close()
	sbErr := rt.sandbox.Close()
	dbErr := rt.db.Close()
	return errors.Join(mgrErr, sbErr, dbErr)
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
