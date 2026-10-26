package builtin

import (
	"embed"
	"fmt"
	"io/fs"

	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

//go:generate toolgen -dir . -pkg builtin -out generated.go -types-pkg api -types-out ../../../../api/tools.go

//go:embed *.tool.md
var builtinFS embed.FS

// FS returns a read-only filesystem containing the embedded default *.tool.md
// manifests for all built-in tools.
func FS() fs.FS {
	return builtinFS
}

// ErrInvalidConfig is returned when [New] receives an invalid [Config].
var ErrInvalidConfig = tools.ErrInvalidConfig

// Config holds the required dependencies for constructing the built-in system tools.
type Config struct {
	Std     *std.Std
	Sandbox sandbox.Sandbox
	Guard   *shellguard.Guard
	Tasks   tasks.Manager
}

// Validate ensures that all required dependencies are non-nil. Zero silent
// fallbacks are permitted.
func (c Config) Validate() error {
	if c.Std == nil {
		return fmt.Errorf("%w: std is required", ErrInvalidConfig)
	}
	if c.Sandbox == nil {
		return fmt.Errorf("%w: sandbox is required", ErrInvalidConfig)
	}
	if c.Guard == nil {
		return fmt.Errorf("%w: guard is required", ErrInvalidConfig)
	}
	return nil
}

// New validates cfg and constructs the 11 built-in system tools backed by
// [std.Std], [shellguard.Guard], and [tasks.Manager].
func New(cfg Config) ([]tools.Tool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	tasksMgr := cfg.Tasks
	if tasksMgr == nil {
		tasksMgr = tasks.New(tasks.Config{})
	}

	return []tools.Tool{
		tools.NewHandler(ToolView, &viewTool{std: cfg.Std, sandbox: cfg.Sandbox, guard: cfg.Guard}),
		tools.NewHandler(ToolLS, &lsTool{std: cfg.Std, sandbox: cfg.Sandbox, guard: cfg.Guard}),
		tools.NewHandler(ToolGlob, &globTool{std: cfg.Std, sandbox: cfg.Sandbox, guard: cfg.Guard}),
		tools.NewHandler(ToolGrep, &grepTool{std: cfg.Std, sandbox: cfg.Sandbox, guard: cfg.Guard}),
		tools.NewHandler(ToolWrite, &writeTool{std: cfg.Std, guard: cfg.Guard}),
		tools.NewHandler(ToolEdit, &editTool{std: cfg.Std, guard: cfg.Guard}),
		tools.NewHandler(ToolRemove, &removeTool{std: cfg.Std, guard: cfg.Guard}),
		tools.NewHandler(ToolBash, &bashTool{std: cfg.Std, sandbox: cfg.Sandbox, guard: cfg.Guard, tasks: tasksMgr}),
		tools.NewHandler(ToolWebFetch, &webFetchTool{std: cfg.Std}),
		tools.NewHandler(ToolDownload, &downloadTool{std: cfg.Std, guard: cfg.Guard}),
		tools.NewHandler(ToolTasks, &tasksTool{tasks: tasksMgr}),
	}, nil
}
