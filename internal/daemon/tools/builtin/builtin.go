package builtin

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
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
	Std          *std.Std
	Sandbox      sandbox.Sandbox
	Guard        *shellguard.Guard
	Tasks        tasks.Manager
	Workspace    workspace.Workspace
	AllowedTools []string
	Skills       []warp.Skill
	RenderSkill  SkillRenderer
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

// New validates cfg and constructs the built-in system tools backed by
// [std.Std], [shellguard.Guard], and [tasks.Manager]. If [Config.AllowedTools]
// is specified, only tools in that list are constructed.
func New(cfg Config) ([]tools.Tool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	tasksMgr := cfg.Tasks
	if tasksMgr == nil {
		tasksMgr = tasks.New(tasks.Config{})
	}

	isAllowed := func(name string) bool {
		if cfg.AllowedTools == nil {
			return true
		}
		for _, a := range cfg.AllowedTools {
			if strings.EqualFold(a, name) {
				return true
			}
		}
		return false
	}

	toolEntries := []struct {
		name    string
		factory func(meta tools.Metadata) tools.Tool
	}{
		{ToolView, func(m tools.Metadata) tools.Tool {
			return tools.NewHandler(ToolView, m, &viewTool{std: cfg.Std, sandbox: cfg.Sandbox, guard: cfg.Guard})
		}},
		{ToolLS, func(m tools.Metadata) tools.Tool {
			return tools.NewHandler(ToolLS, m, &lsTool{std: cfg.Std, sandbox: cfg.Sandbox, guard: cfg.Guard})
		}},
		{ToolGlob, func(m tools.Metadata) tools.Tool {
			return tools.NewHandler(ToolGlob, m, &globTool{std: cfg.Std, sandbox: cfg.Sandbox, guard: cfg.Guard})
		}},
		{ToolGrep, func(m tools.Metadata) tools.Tool {
			return tools.NewHandler(ToolGrep, m, &grepTool{std: cfg.Std, sandbox: cfg.Sandbox, guard: cfg.Guard})
		}},
		{ToolWrite, func(m tools.Metadata) tools.Tool {
			return tools.NewHandler(ToolWrite, m, &writeTool{std: cfg.Std, guard: cfg.Guard})
		}},
		{ToolEdit, func(m tools.Metadata) tools.Tool {
			return tools.NewHandler(ToolEdit, m, &editTool{std: cfg.Std, guard: cfg.Guard})
		}},
		{ToolRemove, func(m tools.Metadata) tools.Tool {
			return tools.NewHandler(ToolRemove, m, &removeTool{std: cfg.Std, guard: cfg.Guard})
		}},
		{ToolBash, func(m tools.Metadata) tools.Tool {
			return tools.NewHandler(ToolBash, m, &bashTool{std: cfg.Std, sandbox: cfg.Sandbox, guard: cfg.Guard, tasks: tasksMgr})
		}},
		{ToolWebFetch, func(m tools.Metadata) tools.Tool {
			return tools.NewHandler(ToolWebFetch, m, &webFetchTool{std: cfg.Std})
		}},
		{ToolDownload, func(m tools.Metadata) tools.Tool {
			return tools.NewHandler(ToolDownload, m, &downloadTool{std: cfg.Std, guard: cfg.Guard})
		}},
		{ToolTasks, func(m tools.Metadata) tools.Tool { return tools.NewHandler(ToolTasks, m, &tasksTool{tasks: tasksMgr}) }},
		{ToolTodos, func(m tools.Metadata) tools.Tool { return tools.NewHandler(ToolTodos, m, &todosTool{}) }},
		{ToolAskQuestion, func(m tools.Metadata) tools.Tool { return tools.NewHandler(ToolAskQuestion, m, &askQuestionTool{}) }},
		{ToolLoadSkill, func(m tools.Metadata) tools.Tool {
			return tools.NewHandler(ToolLoadSkill, m, newLoadSkillTool(cfg.Skills, cfg.RenderSkill, cfg.Workspace))
		}},
	}

	var out []tools.Tool
	for _, entry := range toolEntries {
		if isAllowed(entry.name) {
			var meta tools.Metadata
			if cfg.Workspace != nil {
				wsTool, ok := cfg.Workspace.GetTool(entry.name)
				if !ok {
					return nil, fmt.Errorf("%w: tool %q not found in workspace", tools.ErrToolNotFound, entry.name)
				}
				if wsTool.DisplayName != "" {
					meta.Title = wsTool.DisplayName
				} else {
					meta.Title = wsTool.Name
				}
				if wsTool.Instructions != "" {
					meta.Description = wsTool.Instructions
				} else {
					meta.Description = wsTool.Description
				}
				if wsTool.Annotations != nil {
					meta.Annotation = tool.Annotation{
						IsOpenWorld:  wsTool.Annotations.IsOpenWorld,
						IsDangerous:  wsTool.Annotations.IsDangerous,
						IsReadOnly:   wsTool.Annotations.IsReadOnly,
						IsIdempotent: wsTool.Annotations.IsIdempotent,
						UserHint:     wsTool.Annotations.UserHint,
					}
				}
			}
			out = append(out, entry.factory(meta))
		}
	}
	return out, nil
}
