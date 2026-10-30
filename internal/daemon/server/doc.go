// Package server implements the HTTP server layer for the TaskSmith Brain
// Daemon, providing pluggable modular routers, Server-Sent Events (SSE) streaming,
// and REST routes that serve the daemon to its clients.
//
// # Architecture
//
// The server package is the network-facing boundary of the daemon. It decouples
// HTTP transport and routing from the daemon's internal runtime (workspace
// management, session lifecycle, and gateway orchestration) via composed
// backend interfaces:
//
//   - Backend Abstraction: Business logic access is factored into four cohesive
//     interfaces ([SystemBackend], [EventBackend], [WorkspaceBackend], and
//     [SessionBackend]), composed into [Backend]. This allows routers to be
//     tested independently with mock implementations.
//
//   - Modular Routers: Route declarations are grouped logically into four router
//     constructors ([NewSystemRouter], [NewEventRouter], [NewWorkspaceRouter],
//     and [NewSessionRouter]). Each produces a [Router] containing declared
//     [Route] definitions.
//
//   - Go 1.22+ Multiplexing: [Server.CreateMux] combines routers onto an
//     [http.ServeMux] using Go 1.22+ method-and-path patterns (e.g.
//     `"GET /v1/sessions/{id}"`, `"GET /v1/models/{id...}"`), supporting multiple
//     HTTP methods on shared path patterns natively, and attaching a fallback
//     handler returning [api.ErrorResponse] with code `"route_not_found"` and
//     HTTP 404 StatusNotFound.
//
//   - SSE Event Stream: The `GET /v1/events` endpoint connects subscribers to
//     the daemon's topic-based broker. Connections emit an initial `connected`
//     frame carrying the subscriber ID and subscribed topics, stream JSON events
//     ([api.EventEntryAdded], [api.EventChunk], [api.EventPermissionRequested],
//     [api.EventCompleted], [api.EventError]), and emit periodic `: ping\n\n`
//     keep-alives.
//
// # REST Routes
//
// The daemon route table includes:
//
//   - `GET  /v1/health`                      – Liveness and initialized workspace summary.
//   - `POST /v1/shutdown`                    – Graceful shutdown trigger.
//   - `GET  /v1/events`                      – SSE stream of broker events.
//   - `POST /v1/events/subscribe`            – Add topics to an active SSE stream.
//   - `POST /v1/events/unsubscribe`          – Remove topics from an active SSE stream.
//   - `GET  /v1/workspace`                   – Workspace metadata.
//   - `GET  /v1/agents`                      – List configured agents.
//   - `GET  /v1/agents/{name}`               – Fetch a single agent.
//   - `GET  /v1/models`                      – List available models.
//   - `GET  /v1/models/{id...}`              – Fetch a single model.
//   - `POST /v1/sessions`                    – Create or idempotently ensure a session.
//   - `POST /v1/sessions/preview`            – Preview draft session model resolution and diagnostics.
//   - `GET  /v1/sessions`                    – List sessions.
//   - `GET  /v1/sessions/{id}`               – Fetch a session.
//   - `PATCH /v1/sessions/{id}`              – Update session metadata.
//   - `DELETE /v1/sessions/{id}`             – Delete a session.
//   - `GET  /v1/sessions/{id}/timeline`      – Chronological session timeline events.
//   - `POST /v1/sessions/{id}/send`          – Send a user message / start turn.
//   - `POST /v1/sessions/{id}/cancel`        – Cancel an active turn.
//   - `GET  /v1/sessions/{id}/permissions`   – Pending permission requests.
//   - `POST /v1/sessions/{id}/permissions`   – Respond to a permission request.
//   - `GET  /v1/sessions/{id}/mode`          – Current permission mode.
//   - `PUT  /v1/sessions/{id}/mode`          – Set permission mode.
//   - `GET  /v1/sessions/{id}/changes`       – File-change tracker state.
//   - `GET  /v1/sessions/{id}/diff`          – Unified diff for a change entry.
//   - `POST /v1/sessions/{id}/revert`        – Revert file changes.
//
// # Workspace Scoping
//
// Requests identify their target workspace either via the `X-TaskSmith-CWD` header
// ([api.HeaderCWD]) or the `workspace` query parameter.
//
// # Error Handling
//
// Handlers return structured [github.com/masterkeysrd/tasksmith/api.ErrorResponse]
// JSON bodies with standardized machine-readable codes (`bad_request`, `not_found`,
// `conflict`, `internal_error`) and human-readable messages.
package server
