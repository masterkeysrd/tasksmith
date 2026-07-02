package tools

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/mcp"
	"github.com/masterkeysrd/tasksmith/internal/session/filetrack"
)

const (
	MaxTotalChars = 16000
	MaxLineChars  = 500
)

// Todo represents a single task in the agent's todo checklist.
type Todo struct {
	Description string `json:"description"`
	Status      string `json:"status"`
	ActiveText  string `json:"active_text,omitempty"`
}

// SkillResolver defines the interface to find and render skill instructions.
type SkillResolver interface {
	ResolveSkill(ctx context.Context, name string) (instructions string, path string, err error)
}

// ToolHandlers consolidates all session dependencies and implements the handler methods.
type ToolHandlers struct {
	Storage           FileStorage
	CWD               string
	TaskManager       *TaskManager
	SessionID         string
	SkillResolver     SkillResolver
	PermissionManager permissions.PermissionManager
	LspManager        *lsp.Manager
	FileTracker       filetrack.FileTracker
	McpManager        *mcp.Manager
}

// NewHandlers creates a new ToolHandlers instance with the given dependencies.
func NewHandlers(storage FileStorage, cwd string) *ToolHandlers {
	return &ToolHandlers{
		Storage: storage,
		CWD:     cwd,
	}
}

// WithTaskManager configures the TaskManager and SessionID on ToolHandlers.
func (h *ToolHandlers) WithTaskManager(taskMgr *TaskManager, sessionID string) *ToolHandlers {
	h.TaskManager = taskMgr
	h.SessionID = sessionID
	return h
}

// WithSkillResolver configures the SkillResolver on ToolHandlers.
func (h *ToolHandlers) WithSkillResolver(resolver SkillResolver) *ToolHandlers {
	h.SkillResolver = resolver
	return h
}

// WithPermissionManager configures the PermissionManager on ToolHandlers.
func (h *ToolHandlers) WithPermissionManager(pm permissions.PermissionManager) *ToolHandlers {
	h.PermissionManager = pm
	return h
}

// WithLspManager configures the LspManager on ToolHandlers.
func (h *ToolHandlers) WithLspManager(mgr *lsp.Manager) *ToolHandlers {
	h.LspManager = mgr
	return h
}

// WithFileTracker configures the FileTracker on ToolHandlers.
func (h *ToolHandlers) WithFileTracker(ft filetrack.FileTracker) *ToolHandlers {
	h.FileTracker = ft
	return h
}

// WithMcpManager configures the McpManager on ToolHandlers.
func (h *ToolHandlers) WithMcpManager(mgr *mcp.Manager) *ToolHandlers {
	h.McpManager = mgr
	return h
}

// isProtectedPath returns true if the path points to or is inside TaskSmith internal directories.
func (h *ToolHandlers) isProtectedPath(absPath string) bool {
	dataDir, _ := xdg.Home(xdg.VarTypeData)
	logsDir, _ := xdg.LogsDir()
	configDir, _ := xdg.Home(xdg.VarTypeConfig)

	for _, protected := range []string{dataDir, logsDir, configDir} {
		if protected != "" {
			rel, err := filepath.Rel(protected, absPath)
			if err == nil && !strings.HasPrefix(rel, "..") {
				return true
			}
		}
	}
	return false
}
