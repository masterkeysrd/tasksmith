package tools

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/agent/resolver"
	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/filetrack"
	"github.com/masterkeysrd/tasksmith/internal/mcp"
	"github.com/masterkeysrd/tasksmith/internal/metrics"
)

const (
	MaxTotalChars = 32000
	MaxLineChars  = 500
)

// Todo represents a single task in the agent's todo checklist.
type Todo struct {
	Description string `json:"description"`
	Status      string `json:"status"`
	ActiveText  string `json:"active_text,omitempty"`
}

// ToolHandlers consolidates all session dependencies and implements the handler methods.
type ToolHandlers struct {
	Storage           FileStorage
	CWD               string
	TaskManager       *TaskManager
	SessionID         string
	Resolver          *resolver.Resolver
	AgentName         string
	PermissionManager permissions.PermissionManager
	LspManager        *lsp.Manager
	FileTracker       filetrack.FileTracker
	McpManager        *mcp.Manager
	MetricsStore      *metrics.Store
	OnSetActiveAgent  func(ctx context.Context, agentName string) error
	PreWriteHook      func(filePath string, content string) (string, error)
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

// WithResolver configures the Resolver on ToolHandlers.
func (h *ToolHandlers) WithResolver(r *resolver.Resolver) *ToolHandlers {
	h.Resolver = r
	return h
}

// WithAgentName configures the AgentName on ToolHandlers.
func (h *ToolHandlers) WithAgentName(agentName string) *ToolHandlers {
	h.AgentName = agentName
	return h
}

// WithSetActiveAgent configures the OnSetActiveAgent callback on ToolHandlers.
func (h *ToolHandlers) WithSetActiveAgent(fn func(ctx context.Context, agentName string) error) *ToolHandlers {
	h.OnSetActiveAgent = fn
	return h
}

// WithPreWriteHook configures the PreWriteHook callback on ToolHandlers.
func (h *ToolHandlers) WithPreWriteHook(fn func(filePath string, content string) (string, error)) *ToolHandlers {
	h.PreWriteHook = fn
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

// WithMetricsStore configures the MetricsStore on ToolHandlers.
func (h *ToolHandlers) WithMetricsStore(store *metrics.Store) *ToolHandlers {
	h.MetricsStore = store
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
