package tools

// ToolHandlers consolidates all session dependencies and implements the handler methods.
type ToolHandlers struct {
	Storage FileStorage
	CWD     string
}

// NewHandlers creates a new ToolHandlers instance with the given dependencies.
func NewHandlers(storage FileStorage, cwd string) *ToolHandlers {
	return &ToolHandlers{
		Storage: storage,
		CWD:     cwd,
	}
}
