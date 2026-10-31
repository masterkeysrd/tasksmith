// Package session manages user-facing chat threads, session metadata, persistent
// per-session file change journals, per-session tool containers and permission
// gates, and the chronological session timeline stored in SQLite, resolving
// workspace agents, system prompts, and language models per session and
// executing conversational turns through [github.com/masterkeysrd/tasksmith/internal/daemon/harness.Harness].
//
// # Strict Dependency Injection
//
// As an internal application subsystem, [New] requires all eight runtime
// dependencies on [Config] to be explicitly provided with zero silent fallbacks:
//   - [Config.DB]: Active SQLite connection used for storing the `sessions` table,
//     `timeline` entries, and harness checkpoints.
//   - [Config.Workspace]: Active [github.com/masterkeysrd/tasksmith/internal/daemon/workspace.Workspace]
//     used to resolve session agents, default completion models, tool policies,
//     and dynamic system prompts.
//   - [Config.Gateway]: Active [github.com/masterkeysrd/tasksmith/internal/daemon/gateway.Gateway] getter
//     used to resolve concrete [github.com/masterkeysrd/loom/llm.Model] instances.
//   - [Config.Sandbox]: Active [github.com/masterkeysrd/tasksmith/internal/daemon/sandbox.Sandbox]
//     used for isolated filesystem, command, and network execution across the
//     session's standard and custom tools.
//   - [Config.Publisher]: Active [Publisher] used to broadcast real-time
//     [EventEntryAdded], [EventChunk], [EventPermissionRequested], and
//     [EventCompleted] notifications.
//   - [Config.Logger]: Structured [github.com/masterkeysrd/tasksmith/internal/core/log.Interface]
//     scoped to the "session" component.
//   - [Config.Timeline]: Active [github.com/masterkeysrd/tasksmith/internal/daemon/timeline.Store]
//     used for append-only chronological session event logging and queries.
//   - [Config.Journal]: Active [github.com/masterkeysrd/tasksmith/internal/daemon/journal.Store]
//     used for per-session file change tracking, diffing, and baseline reversion.
//
// # Separation of Session Timeline and Graph Checkpoints
//
// TaskSmith maintains a strict separation between two persistence layers:
//  1. The `timeline` table managed by [github.com/masterkeysrd/tasksmith/internal/daemon/timeline.Store]
//     records the complete, append-only chronological log of session events
//     (sorted chronologically by UUIDv7 `id ASC` via [Manager.Timeline]), including
//     user turns, intermediate tool-calling assistant messages, tool execution
//     results, final assistant replies, model/agent transitions, permission mode
//     updates, and permission decisions. [Manager.History] queries the message
//     subset of the timeline for UI transcript callers.
//  2. The `checkpoints` table managed by [github.com/masterkeysrd/tasksmith/internal/daemon/harness.Harness]
//     stores the Loom workflow graph execution state sent to the LLM.
//
// Appending an entry internally (for example, a UI-only system notice or mode change) records
// the entry in the session timeline and publishes an [EventEntryAdded] event without
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
// # Persistent Per-Session Journal
//
// [Manager.Journal] returns a persistent [github.com/masterkeysrd/tasksmith/internal/daemon/journal.Journal]
// implementing [github.com/masterkeysrd/tasksmith/internal/daemon/std.FileTracker]
// with SQLite metadata paired with content-addressable snapshots in `<sessionDir>/blobs/<sha256>`.
// This enables on-demand unified diff generation ([github.com/masterkeysrd/tasksmith/internal/daemon/journal.Journal.DiffEntry]),
// point-in-time recovery ([github.com/masterkeysrd/tasksmith/internal/daemon/journal.Journal.RecoverAt]), and conflict-aware 3-way
// baseline reversion ([github.com/masterkeysrd/tasksmith/internal/daemon/journal.Journal.RevertToBaseline]).
package session
