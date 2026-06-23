package tools

import "context"

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
	Storage       FileStorage
	CWD           string
	TaskManager   *TaskManager
	SessionID     string
	SkillResolver SkillResolver
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
