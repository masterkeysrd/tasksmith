package scope

import "context"

// Scope carries session-, turn-, and tool-scoped metadata across execution.
type Scope struct {
	SessionID  string
	TurnID     string // User message ID that initiated the turn
	MessageID  string // Assistant message ID that emitted the tool call
	ToolCallID string // Active ToolCall.ID
	ToolName   string // Active ToolCall.Name

	WorkspaceRoot   string // Canonical workspace root path
	Agent           string // The session's agent
	Purpose         string // LLM call purpose: chat, compaction or title
	ParentSessionID string // Parent session for subagents; empty otherwise
}

type contextKey struct{}

// With returns a new context with non-empty fields of s merged onto any
// existing [Scope] in ctx.
func With(ctx context.Context, s Scope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	existing, _ := From(ctx)
	if s.SessionID != "" {
		existing.SessionID = s.SessionID
	}
	if s.TurnID != "" {
		existing.TurnID = s.TurnID
	}
	if s.MessageID != "" {
		existing.MessageID = s.MessageID
	}
	if s.ToolCallID != "" {
		existing.ToolCallID = s.ToolCallID
	}
	if s.ToolName != "" {
		existing.ToolName = s.ToolName
	}
	if s.WorkspaceRoot != "" {
		existing.WorkspaceRoot = s.WorkspaceRoot
	}
	if s.Agent != "" {
		existing.Agent = s.Agent
	}
	if s.Purpose != "" {
		existing.Purpose = s.Purpose
	}
	if s.ParentSessionID != "" {
		existing.ParentSessionID = s.ParentSessionID
	}
	return context.WithValue(ctx, contextKey{}, existing)
}

// From extracts the [Scope] from ctx.
func From(ctx context.Context) (Scope, bool) {
	if ctx == nil {
		return Scope{}, false
	}
	v, ok := ctx.Value(contextKey{}).(Scope)
	return v, ok
}
