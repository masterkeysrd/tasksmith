// Package daemon implements the TaskSmith Brain Daemon server, providing a
// single-instance background runtime over a Unix domain socket with HTTP/JSON
// endpoints and real-time Server-Sent Events (SSE).
//
// # Architecture
//
// The Brain Daemon decouples long-running agent execution, SQLite persistence,
// OS sandboxes, and workspace state from stateless client interfaces (such as
// the TUI or CLI):
//   - Single-Instance Lockfile: [Daemon] acquires a non-blocking exclusive
//     `flock` on [Config.LockPath] before binding [Config.SocketPath], returning
//     [ErrAlreadyRunning] if another daemon instance is active.
//   - Lazy Workspace Runtime Cache: Each workspace directory is opened on
//     demand and cached with its own [workspace.Workspace], [sandbox.Sandbox],
//     pooled [gateway.Gateway], SQLite database (`tasksmith.db`),
//     [timeline.Store], and [session.Manager].
//   - Detached Background Turns: Conversational turns (`Send` and
//     `RespondPermission`) execute in daemon-owned background goroutines bound
//     to the workspace runtime's lifecycle rather than the incoming HTTP
//     request context. If a client disconnects mid-turn, the agent continues
//     executing and persisting messages and events to the timeline in SQLite undisturbed.
//   - Real-Time Topic Broker: Events ([api.EventEntryAdded], [api.EventChunk],
//     [api.EventPermissionRequested], [api.EventCompleted], [api.EventError],
//     and session lifecycle events) are delivered through [broker.Broker] to
//     subscribers matching their exact topics (e.g. `sessions` or `sessions.<id>.stream`).
//   - Modular Server Layer: HTTP routing and request multiplexing are delegated
//     to [server.Server] and specialized modular routers ([server.NewSystemRouter],
//     [server.NewEventRouter], [server.NewWorkspaceRouter], [server.NewSessionRouter]),
//     allowing clean decoupling between HTTP transport and daemon runtime logic.
package daemon
