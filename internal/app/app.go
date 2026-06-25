// Package app provides the main application structure and lifecycle management for TaskSmith.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/app/flags"
	coredb "github.com/masterkeysrd/tasksmith/internal/core/db"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutil"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/metrics"
	"github.com/masterkeysrd/tasksmith/internal/session"
	"github.com/masterkeysrd/tasksmith/internal/tui"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
)

type Application struct {
	opts       *flags.Flags
	ws         *workspace.Workspace
	api        *api.Service
	lspManager *lsp.Manager
	closers    []func(ctx context.Context) error
	cancel     context.CancelFunc
}

func New(opts *flags.Flags, cancel context.CancelFunc) *Application {
	return &Application{
		opts:   opts,
		cancel: cancel,
	}
}

func (app *Application) Run(ctx context.Context) error {
	if err := app.InitializeLogs(); err != nil {
		return fmt.Errorf("failed to initialize logs: %w", err)
	}

	// Initialize sqlite connections and session manager
	sqliteConn, err := coredb.Open(app.opts.CWD, "tasksmith.db")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	app.AddCloser(func(ctx context.Context) error {
		return sqliteConn.Close()
	})

	checkpointsConn, err := coredb.Open(app.opts.CWD, "checkpoints.db")
	if err != nil {
		return fmt.Errorf("failed to open checkpoints database: %w", err)
	}
	app.AddCloser(func(ctx context.Context) error {
		return checkpointsConn.Close()
	})

	store, err := session.NewSQLiteStore(sqliteConn, checkpointsConn)
	if err != nil {
		return fmt.Errorf("failed to initialize session store: %w", err)
	}

	metricsDB, err := coredb.InitMetricsDB()
	if err != nil {
		return fmt.Errorf("failed to initialize metrics database: %w", err)
	}
	app.AddCloser(func(ctx context.Context) error {
		return metricsDB.Close()
	})

	app.lspManager = lsp.NewManager()
	app.AddCloser(func(ctx context.Context) error {
		app.lspManager.CloseAll()
		return nil
	})

	app.ws = workspace.New(app.opts.CWD)
	if err := app.ws.Load(ctx); err != nil {
		return fmt.Errorf("failed to load workspace: %w", err)
	}

	metricsStore := metrics.NewStore(metricsDB)
	sessionMgr := session.NewManager(session.ManagerConfig{
		Store:        store,
		Workspace:    app.ws,
		MetricsStore: metricsStore,
		LspManager:   app.lspManager,
	})
	app.AddCloser(func(ctx context.Context) error {
		if sessionMgr.McpManager() != nil {
			return sessionMgr.McpManager().Close()
		}
		return nil
	})
	app.api = api.NewService(app.ws, sessionMgr, metricsStore, app.lspManager)

	log.Info("Starting TaskSmith application",
		log.String("cwd", app.opts.CWD),
		log.Any("log_level", app.opts.LogLevel))

	go func() {
		if err := app.lspManager.RestartClient(context.Background(), app.opts.CWD); err != nil {
			log.Error("Failed to start LSP client on startup", log.Err(err))
		}
	}()

	app.InitializeCommands()
	app.InitializeKeymap()

	return tui.Run(ctx, tui.RunOptions{
		Client:       app.api,
		DevToolsAddr: app.opts.DevToolsAddr,
	})
}

func (app *Application) Workspace() *workspace.Workspace {
	return app.ws
}

func (app *Application) API() *api.Service {
	return app.api
}

func (app *Application) InitializeLogs() error {
	path, err := xdg.LogsDir()
	if err != nil {
		return fmt.Errorf("failed to determine logs directory: %w", err)
	}

	// Ensure the logs directory exists
	if err := fsutil.EnsureDir(path); err != nil {
		return fmt.Errorf("failed to ensure logs directory exists: %w", err)
	}

	fname := filepath.Join(path, log.DefaultLogFilename())
	file, err := os.OpenFile(fname, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	prevLogger := log.Default()
	app.AddCloser(func(ctx context.Context) error {
		log.SetDefault(prevLogger)
		return nil
	})

	app.AddCloser(func(ctx context.Context) error {
		return file.Close()
	})

	log.SetDefault(log.New(file, app.opts.LogLevel))

	return nil
}

func (app *Application) AddCloser(closer func(ctx context.Context) error) {
	app.closers = append(app.closers, closer)
}

func (app *Application) Close(ctx context.Context) error {
	var errs []error
	for i := len(app.closers) - 1; i >= 0; i-- {
		closer := app.closers[i]
		if err := closer(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (app *Application) Quit() {
	if app.cancel != nil {
		app.cancel()
	}
}
