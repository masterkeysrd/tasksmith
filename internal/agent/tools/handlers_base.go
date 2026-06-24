package tools

import (
	"context"

	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
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
