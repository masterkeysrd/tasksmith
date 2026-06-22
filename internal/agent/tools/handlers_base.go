package tools

// ToolHandlers consolidates all session dependencies and implements the handler methods.
type ToolHandlers struct {
	Storage     FileStorage
	CWD         string
	TaskManager *TaskManager
	SessionID   string
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
