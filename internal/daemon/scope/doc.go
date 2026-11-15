// Package scope carries session-, turn-, and tool-scoped metadata through
// [context.Context] so that subsystems (journal, metrics, sandbox, and others)
// can attribute their work without explicit parameter passing.
//
// # Scope
//
// A [Scope] identifies the active execution context:
//   - [Scope.SessionID] / [Scope.ParentSessionID]: the session and its parent
//     (for subagents).
//   - [Scope.TurnID]: the user message ID that initiated the current turn.
//   - [Scope.MessageID]: the assistant message ID that emitted the active tool
//     call.
//   - [Scope.ToolCallID] / [Scope.ToolName]: the active tool call's identity.
//   - [Scope.WorkspaceRoot]: the canonical workspace root path.
//   - [Scope.Agent]: the session's agent name.
//   - [Scope.Purpose]: the LLM call purpose (chat, compaction, or title).
//
// # Context Propagation
//
// [With] returns a new context with the non-empty fields of the given [Scope]
// merged onto any existing scope in the context. [From] extracts the current
// scope. Callers nest scopes by calling [With] at each level (session, turn,
// tool call), and inner scopes inherit outer fields automatically.
package scope
