// Package server implements the HTTP server layer for the TaskSmith Brain
// Daemon, providing a pluggable router, Server-Sent Events (SSE) streaming
// endpoint, and REST routes that serve the daemon to its clients.
//
// # Architecture
//
// The server package is the network-facing boundary of the daemon. It decouples
// HTTP transport and routing from the daemon's internal runtime (workspace
// management, session lifecycle, and gateway orchestration) via the [Backend]
// interface:
//
//   - Backend Abstraction: All business-logic access goes through the
//     [Backend] interface (currently exposing [Backend.ListSessions]),
//     allowing the server to be tested with fakes and evolved independently of
//     the daemon internals.
//
//   - Router and Route Registration: [Router] is a minimal path-and-method
//     mux built on [http.ServeMux]. Routes are declared with helpers
//     ([NewRoute], [GetRoute], [PostRoute]) and registered via
//     [Router.buildRoutes]; the resulting [http.Handler] is produced by
//     [Router.handler], which enforces HTTP method matching and delegates to
//     the registered [HandlerFunc].
//
//   - SSE Event Stream: The `GET /v1/events` endpoint (registered as a route
//     in the daemon's `routes()` builder) maintains a long-lived
//     `text/event-stream` connection. Subscribers filter by workspace and
//     optional session ID; server-sent events carry
//     [github.com/masterkeysrd/tasksmith/api.Event] payloads
//     ([api.EventMessageAdded], [api.EventChunk],
//     [api.EventPermissionRequested], [api.EventCompleted],
//     [api.EventError]). Periodic pings keep intermediaries from stalling the
//     stream.
//
//   - Server Lifecycle: [Server] wraps a [Router] and owns its [http.Server].
//     [NewServer] constructs a server bound to a [Backend]; [Server.Start]
//     launches the HTTP listener. Graceful shutdown is coordinated by the
//     parent daemon via its `Close` path.
//
// # REST Routes
//
// The daemon's full route table (defined in the parent package) includes:
//
//   - `GET  /v1/health`                  – Liveness and open-workspace summary.
//   - `POST /v1/shutdown`                – Graceful shutdown trigger.
//   - `GET  /v1/events`                  – SSE stream of session events.
//   - `GET  /v1/workspace`               – Workspace metadata.
//   - `GET  /v1/agents`                  – List configured agents.
//   - `GET  /v1/agents/{name}`           – Fetch a single agent.
//   - `GET  /v1/models`                  – List available models.
//   - `GET  /v1/models/{id}`             – Fetch a single model.
//   - `POST /v1/sessions`                – Create or retrieve a session.
//   - `GET  /v1/sessions`                – List sessions.
//   - `GET  /v1/sessions/{id}`           – Fetch a session.
//   - `PATCH /v1/sessions/{id}`          – Update a session.
//   - `DELETE /v1/sessions/{id}`         – Delete a session.
//   - `GET  /v1/sessions/{id}/messages`  – Message history.
//   - `POST /v1/sessions/{id}/messages`  – Send a user message / start turn.
//   - `POST /v1/sessions/{id}/cancel`    – Cancel an active turn.
//   - `GET  /v1/sessions/{id}/permissions`     – Pending permission requests.
//   - `POST /v1/sessions/{id}/permissions`     – Respond to a permission request.
//   - `GET  /v1/sessions/{id}/mode`           – Current permission mode.
//   - `PUT  /v1/sessions/{id}/mode`           – Set permission mode.
//   - `GET  /v1/sessions/{id}/changes`        – File-change tracker state.
//   - `GET  /v1/sessions/{id}/diff`          – Unified diff for a change entry.
//   - `POST /v1/sessions/{id}/revert`        – Revert file changes.
//
// # Workspace Scoping
//
// Requests identify their target workspace either via the `X-CWD` header
// ([api.HeaderCWD]) or the `workspace` query parameter. The server resolves
// the raw path to a canonical root directory and caches the runtime per
// workspace.
//
// # Error Handling
//
// HTTP handlers return structured [github.com/masterkeysrd/tasksmith/api.ErrorResponse]
// JSON bodies with a machine-readable `code` field
// (`bad_request`, `not_found`, `conflict`, `internal_error`)
// and a human-readable `message`.
package server
