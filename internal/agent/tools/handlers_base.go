package tools

// ToolHandlers consolidates all session dependencies and implements the handler methods.
type ToolHandlers struct {
	Storage FileStorage
}

// NewHandlers creates a new ToolHandlers instance with the given dependencies.
func NewHandlers(storage FileStorage) *ToolHandlers {
	return &ToolHandlers{
		Storage: storage,
	}
}
