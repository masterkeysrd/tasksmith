// Package daemon implements the TaskSmith Brain Daemon server, providing a
// single-instance background runtime over a Unix domain socket with HTTP/JSON
// endpoints and real-time Server-Sent Events (SSE).
//
// # Architecture
//
// The Brain Daemon decouples long-running agent execution, SQLite persistence,
// OS sandboxes, and workspace state from stateless client interfaces (such as
// the TUI or CLI):
//   - Single-Instance Lockfile: [Server] acquires a non-blocking exclusive
//     `flock` on [Config.LockPath] before binding [Config.SocketPath], returning
//     [ErrAlreadyRunning] if another daemon instance is active.
//   - Lazy Workspace Runtime Cache: Each workspace directory is opened on
//     demand and cached with its own [workspace.Workspace], [sandbox.Sandbox],
//     pooled [gateway.Gateway], SQLite database (`tasksmith.db`), and
//     [session.Manager].
//   - Detached Background Turns: Conversational turns (`Send` and
//     `RespondPermission`) execute in daemon-owned background goroutines bound
//     to the workspace runtime's lifecycle rather than the incoming HTTP
//     request context. If a client disconnects mid-turn, the agent continues
//     executing and persisting messages to SQLite undisturbed.
//   - Real-Time SSE Hub: Session events ([api.EventMessageAdded],
//     [api.EventChunk], [api.EventPermissionRequested], [api.EventCompleted],
//     and [api.EventError]) are broadcast to connected `/v1/events` subscribers
//     filtered by workspace and optional session ID.
package daemon
