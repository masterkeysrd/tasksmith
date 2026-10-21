// Package session manages user-facing chat threads, session metadata, persistent
// per-session file change journals, per-session tool containers and permission
// gates, and the permanent UI message history stored in SQLite, resolving
// workspace agents, system prompts, and language models per session and
// executing conversational turns through [github.com/masterkeysrd/tasksmith/internal/harness.Harness].
//
// # Strict Dependency Injection
//
// As an internal application subsystem, [New] requires all six runtime
// dependencies on [Config] to be explicitly provided with zero silent fallbacks:
//   - [Config.DB]: Active SQLite connection used for storing the `sessions` and
//     `messages` tables as well as harness checkpoints.
//   - [Config.Workspace]: Active [github.com/masterkeysrd/tasksmith/internal/workspace.Workspace]
//     used to resolve session agents, default completion models, tool policies,
//     and dynamic system prompts.
//   - [Config.Gateway]: Active [github.com/masterkeysrd/tasksmith/internal/gateway.Gateway]
//     used to resolve concrete [github.com/masterkeysrd/loom/llm.Model] instances.
//   - [Config.Sandbox]: Active [github.com/masterkeysrd/tasksmith/internal/sandbox.Sandbox]
//     used for isolated filesystem, command, and network execution across the
//     session's standard and custom tools.
//   - [Config.Publisher]: Active [Publisher] used to broadcast real-time
//     [EventMessageAdded], [EventChunk], [EventPermissionRequested], and
//     [EventCompleted] notifications.
//   - [Config.Logger]: Structured [github.com/masterkeysrd/tasksmith/internal/core/log.Interface]
//     scoped to the "session" component.
//
// # Separation of UI History and Graph Checkpoints
//
// TaskSmith maintains a strict separation between two persistence layers:
//  1. The `messages` table managed by this package records the complete, ordered
//     UI thread transcript (sorted chronologically by UUIDv7 `id ASC` via
//     [Manager.History]), including user turns, intermediate tool-calling
//     assistant messages, tool execution results, final assistant replies, and
//     chat-only notifications appended internally.
//  2. The `checkpoints` table managed by [github.com/masterkeysrd/tasksmith/internal/harness.Harness]
//     stores the Loom workflow graph execution state sent to the LLM.
//
// Appending a message internally (for example, a UI-only system notice) records
// the message in `messages` and publishes an [EventMessageAdded] event without
// mutating or polluting the underlying harness graph checkpoint for that session.
//
// # All-or-Nothing Permission Resolution
//
// When a turn pauses at `ToolsNode` awaiting user approval for one or more tool
// calls, [Manager.Send] publishes [EventPermissionRequested] and returns
// `(nil, nil)`. Callers inspect unresolved permissions via
// [Manager.PendingPermissions] and submit individual decisions via
// [Manager.RespondPermission]. Partial decisions are buffered in memory per
// session without invoking the workflow graph until every pending permission for
// the turn has been resolved, at which point [Manager.RespondPermission] resumes
// the harness graph with the complete decision set. Calling [Manager.Send] with
// a new user message while permissions are pending discards any buffered partial
// decisions and cancels the pending tool calls in chronological order.
//
// # Persistent Per-Session File Tracker
//
// [Manager.Tracker] returns a persistent [*FileTracker] implementing
// [github.com/masterkeysrd/tasksmith/internal/std.FileTracker] rooted at
// `<xdg.WorkspaceDir>/sessions/<session_id>/changes/`. Each tracked file is
// stored in a compact `<path_hash>.jsonl` file (Line 1: [JournalHeader],
// Lines 2..N: [JournalEntry] without redundant `path` or inline `diff` fields)
// paired with deduplicated content-addressable snapshots in `changes/blobs/<sha256>`.
// This layout enables on-demand unified diff generation ([FileTracker.DiffEntry]),
// point-in-time recovery ([FileTracker.RecoverAt]), and conflict-aware 3-way
// baseline reversion ([FileTracker.RevertToBaseline]).
package session
