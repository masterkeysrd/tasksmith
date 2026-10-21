// Package session manages user-facing chat threads, session metadata, and the
// permanent UI message history stored in SQLite, resolving workspace agents,
// system prompts, and language models per session and executing conversational
// turns through [github.com/masterkeysrd/tasksmith/internal/harness.Harness].
//
// # Strict Dependency Injection
//
// As an internal application subsystem, [New] requires all five runtime
// dependencies on [Config] to be explicitly provided with zero silent fallbacks:
//   - [Config.DB]: Active SQLite connection used for storing the `sessions` and
//     `messages` tables as well as harness checkpoints.
//   - [Config.Workspace]: Active [github.com/masterkeysrd/tasksmith/internal/workspace.Workspace]
//     used to resolve session agents, default completion models, and dynamic
//     system prompts.
//   - [Config.Gateway]: Active [github.com/masterkeysrd/tasksmith/internal/gateway.Gateway]
//     used to resolve concrete [github.com/masterkeysrd/loom/llm.Model] instances.
//   - [Config.Publisher]: Active [Publisher] used to broadcast real-time
//     [EventMessageAdded], [EventChunk], and [EventCompleted] notifications.
//   - [Config.Logger]: Structured [github.com/masterkeysrd/tasksmith/internal/core/log.Interface]
//     scoped to the "session" component.
//
// # Separation of UI History and Graph Checkpoints
//
// TaskSmith maintains a strict separation between two persistence layers:
//  1. The `messages` table managed by this package records the complete, ordered
//     UI thread transcript (sorted chronologically by UUIDv7 `id ASC` via
//     [Manager.History]), including user turns, assistant replies, and chat-only
//     notifications appended via [Manager.AppendMessage].
//  2. The `checkpoints` table managed by [github.com/masterkeysrd/tasksmith/internal/harness.Harness]
//     stores the Loom workflow graph execution state sent to the LLM.
//
// Appending a message via [Manager.AppendMessage] (for example, a UI-only system
// notice) records the message in `messages` and publishes an [EventMessageAdded]
// event without mutating or polluting the underlying harness graph checkpoint
// for that session.
package session
