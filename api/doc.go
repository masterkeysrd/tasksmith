// Package api defines the public, wire-level JSON data transfer objects,
// enumerations, error types, and Server-Sent Event (SSE) envelopes exchanged
// between the TaskSmith Brain Daemon and stateless clients (TUI, CLI, and
// external integrations). It also includes the generated built-in tool input
// and output types (tools.go).
//
// # Real-Time Server-Sent Events (SSE)
//
// The daemon publishes structured events over `/v1/events` encapsulated by [Event].
// Each event carries a typed [EventType], an optional broker topic (see [TopicSessions]
// and [SessionStreamTopic]), and a typed [EventPayload] conforming to the
// daemon-wide event broker:
//   - [ConnectedPayload] ([EventConnected]): echoes initial subscription topics and client subscriber ID.
//   - [SessionCreatedPayload] ([EventSessionCreated]): broadcast when a new session is created.
//   - [SessionUpdatedPayload] ([EventSessionUpdated]): broadcast when session metadata (title, agent, model) changes.
//   - [SessionDeletedPayload] ([EventSessionDeleted]): broadcast when a session is removed.
//   - [EntryAddedPayload] ([EventEntryAdded]): broadcast when a new [TimelineEntry] is recorded in a session.
//   - [ChunkPayload] ([EventChunk]): streaming assistant token or delta blocks.
//   - [PermissionRequestedPayload] ([EventPermissionRequested]): pending tool approvals.
//   - [CompletedPayload] ([EventCompleted]): final assistant reply concluding a turn.
//   - [ErrorPayload] ([EventError]): terminal turn errors.
//
// Dynamic topic subscriptions are managed via [SubscriptionRequest] and [SubscriptionResponse]
// addressing the daemon's internal event broker.
//
// # Chronological Session Timeline
//
// Chronological session event transcripts are structured as [TimelineEntry] items
// classified by [TimelineKind]:
//   - [TimelineKindMessage]: chat turns ([Message]).
//   - [TimelineKindModel]: model transitions ([ModelChange]).
//   - [TimelineKindEffort]: reasoning effort changes ([EffortChange]).
//   - [TimelineKindAgent]: active agent updates ([AgentChange]).
//   - [TimelineKindMode]: permission mode transitions ([ModeChange]).
//   - [TimelineKindPermission]: tool permission resolutions ([PermissionRecord]).
//
// This package is a pure protocol contract and depends exclusively on the Go
// standard library with zero internal or third-party dependencies.
package api
