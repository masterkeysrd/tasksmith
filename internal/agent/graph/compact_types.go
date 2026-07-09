package graph

// CompactedData represents the compressed representation of a tool execution.
type CompactedData struct {
	// Summary is a human-readable, token-efficient description of what the tool accomplished.
	Summary string
	// CompactedArgs is a map of arguments to replace the original ToolCall arguments.
	// If non-nil, the heavy payload arguments are stripped or replaced with lightweight equivalents.
	CompactedArgs map[string]any
}

// CompactContentProvider is implemented by tools that want to provide custom compaction behavior
// during Phase 1 (Observation Masking).
type CompactContentProvider interface {
	CompactContent(args map[string]any) CompactedData
}

// TimelineData represents the ultra-dense representation of a tool execution for timeline generation.
type TimelineData struct {
	// Summary is a concise summary of the action/result, suitable for a chronological log.
	Summary string
}

// TimelineContentProvider is implemented by tools that want to provide a specific, ultra-lean
// representation of their execution during Phase 2 timeline generation.
type TimelineContentProvider interface {
	TimelineContent(args map[string]any) TimelineData
}

// Global registries mapping tool names to their compaction content providers.
var CompactProviders = make(map[string]CompactContentProvider)
var TimelineProviders = make(map[string]TimelineContentProvider)
