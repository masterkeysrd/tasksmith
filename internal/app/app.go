// Package app provides the main application structure and lifecycle management for TaskSmith.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"syscall"

	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/app/flags"
	coredb "github.com/masterkeysrd/tasksmith/internal/core/db"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutil"
	"github.com/masterkeysrd/tasksmith/internal/core/fuzzy"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/filetrack"
	"github.com/masterkeysrd/tasksmith/internal/metrics"
	"github.com/masterkeysrd/tasksmith/internal/session"
	"github.com/masterkeysrd/tasksmith/internal/tui"
	"github.com/masterkeysrd/tasksmith/internal/tui/plugin/autocomplete"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
)

type closerEntry struct {
	name string
	fn   func(ctx context.Context) error
}

type Application struct {
	opts       *flags.Flags
	ws         *workspace.Workspace
	api        *api.Service
	lspManager *lsp.Manager
	closers    []closerEntry
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

	if app.opts.Debug {
		addr := "localhost:6060"
		if envAddr := os.Getenv("TASKSMITH_PPROF_ADDR"); envAddr != "" {
			addr = envAddr
		}
		srv := &http.Server{
			Addr: addr,
		}
		go func() {
			log.Info("Starting pprof server on http://" + addr + "/debug/pprof/")
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("pprof server error", log.Err(err))
			}
		}()
		app.AddCloser("pprof server", func(ctx context.Context) error {
			log.Info("Stopping pprof server")
			return srv.Shutdown(ctx)
		})
	}

	// Initialize sqlite connections and session manager
	sqliteConn, err := coredb.Open(app.opts.CWD, "tasksmith.db")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	app.AddCloser("sqliteConn", func(ctx context.Context) error {
		return sqliteConn.Close()
	})

	checkpointsConn, err := coredb.Open(app.opts.CWD, "checkpoints.db")
	if err != nil {
		return fmt.Errorf("failed to open checkpoints database: %w", err)
	}
	app.AddCloser("checkpointsConn", func(ctx context.Context) error {
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
	app.AddCloser("metricsDB", func(ctx context.Context) error {
		return metricsDB.Close()
	})

	app.lspManager = lsp.NewManager()
	app.AddCloser("lspManager", func(ctx context.Context) error {
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
		Context:      ctx,
	})
	app.AddCloser("sessionMgr & mcpManager", func(ctx context.Context) error {
		_ = sessionMgr.Close()
		if sessionMgr.McpManager() != nil {
			return sessionMgr.McpManager().Close()
		}
		return nil
	})
	// Initialize and start the global workspace tracker
	wsTracker := filetrack.NewWorkspaceTracker(app.opts.CWD)
	if err := wsTracker.Start(ctx); err != nil {
		return fmt.Errorf("failed to start workspace tracker: %w", err)
	}
	app.AddCloser("workspaceTracker", func(ctx context.Context) error {
		return wsTracker.Stop()
	})

	// Initialize the API service
	app.api = api.NewService(app.ws, sessionMgr, metricsStore, app.lspManager, wsTracker)

	// Setup autocomplete plugin with file, symbol, and command sources
	acPlugin := autocomplete.NewPlugin(autocomplete.Deps{
		Sources: []autocomplete.Source{
			autocomplete.NewFileSource(func(query string) []autocomplete.FileSearchResult {
				results := wsTracker.Search(query)
				var searchResults []autocomplete.FileSearchResult
				for _, r := range results {
					searchResults = append(searchResults, autocomplete.FileSearchResult{
						Path:      r.Path,
						ShortPath: r.ShortPath,
						IsDir:     r.IsDir,
					})
				}
				return searchResults
			}),
			autocomplete.NewSymbolSource(func(query string) []autocomplete.SymbolSearchResult {
				if query == "" {
					query = "a" // Fallback to "a" for empty queries to show initial list
				}
				results, err := app.api.LspSymbols(context.Background(), api.LspSymbolsRequest{Query: query})
				if err != nil {
					return nil
				}
				var searchResults []autocomplete.SymbolSearchResult
				for _, r := range results.Results {
					searchResults = append(searchResults, autocomplete.SymbolSearchResult{
						Name:          r.Name,
						Kind:          r.Kind,
						Path:          r.Path,
						StartLine:     r.Line,
						StartChar:     r.Char,
						ContainerName: r.ContainerName,
					})
					if len(searchResults) >= 50 { // Limit to top 50 symbols for performance
						break
					}
				}
				return searchResults
			}),
			autocomplete.NewSkillSource(func(ctx context.Context, sessionID, query string) ([]autocomplete.SkillSearchResult, error) {
				res, err := app.api.ListSkills(ctx, api.ListSkillsRequest{SessionID: sessionID})
				if err != nil {
					return nil, err
				}
				var searchResults []autocomplete.SkillSearchResult
				for _, skill := range res.Skills {
					if query == "" {
						searchResults = append(searchResults, autocomplete.SkillSearchResult{
							Name:        skill.Name,
							Description: skill.Description,
						})
					} else if matched, _ := fuzzy.Match(skill.Name, query); matched {
						searchResults = append(searchResults, autocomplete.SkillSearchResult{
							Name:        skill.Name,
							Description: skill.Description,
						})
					}
				}
				return searchResults, nil
			}),
			autocomplete.NewCommandSource(),
		},
	})
	autocomplete.SetPlugin(acPlugin)

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
	app.AddCloser("prevLogger", func(ctx context.Context) error {
		log.SetDefault(prevLogger)
		return nil
	})

	app.AddCloser("log file", func(ctx context.Context) error {
		return file.Close()
	})

	log.SetDefault(log.New(file, app.opts.LogLevel))

	// Redirect fd 2 (stderr) to the log file so that Go runtime panic stack traces
	// from all goroutines are captured in the log file.
	if err := syscall.Dup2(int(file.Fd()), 2); err != nil {
		log.Warn("Failed to redirect stderr to log file", log.Err(err))
	}

	return nil
}

func (app *Application) AddCloser(name string, closer func(ctx context.Context) error) {
	app.closers = append(app.closers, closerEntry{name: name, fn: closer})
}

func (app *Application) Close(ctx context.Context) error {
	var errs []error
	for i := len(app.closers) - 1; i >= 0; i-- {
		entry := app.closers[i]
		fmt.Fprintf(os.Stderr, "[Closer Trace] Running closer: %s\n", entry.name)
		if err := entry.fn(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "[Closer Trace] Closer %s failed: %v\n", entry.name, err)
			errs = append(errs, err)
		} else {
			fmt.Fprintf(os.Stderr, "[Closer Trace] Closer %s succeeded\n", entry.name)
		}
	}
	return errors.Join(errs...)
}

func (app *Application) Quit() {
	if app.cancel != nil {
		app.cancel()
	}
}
