// Package client provides a stateless HTTP and Server-Sent Events (SSE) client
// for communicating with the TaskSmith Brain Daemon over a Unix domain socket.
//
// # Session Operations
//
// Sessions support conversation turns and timeline inspection:
//   - [Client.Send]: submits user messages to `/v1/sessions/{id}/send`, triggering conversational turns in the background runtime.
//   - [Client.Timeline]: retrieves chronological session timeline entries ([api.TimelineEntry]) via `GET /v1/sessions/{id}/timeline`.
//   - [Client.Cancel],
//     and file mutation journal methods ([Client.Changes], [Client.DiffChange], [Client.Revert]).
//
// # MCP Server Operations
//
// Configured MCP servers are inspected and driven through the workspace-scoped
// `/v1/workspace/mcps` routes:
//   - [Client.ListMCPs]: lists each server's resolved transport, live status, and
//     discovered tools, prompts, and resources ([api.MCPServer]).
//   - [Client.RestartMCP] and [Client.AuthenticateMCP]: reconnect a server or start
//     its interactive authorization flow.
//   - [Client.GetMCPPrompt] and [Client.ReadMCPResource]: render a prompt or read a
//     resource; items blocked by a server manifest's policies are rejected unless
//     the request opts into a single user-authorized use.
//   - [Client.AllowMCPPolicy]: permit a blocked item by updating the manifest.
//
// # Real-Time Server-Sent Events (SSE)
//
// Clients stream typed real-time notifications ([api.Event]) from the daemon's internal event broker:
//   - [Client.Events] and [Client.EventsWithReady]: open a real-time SSE stream for arbitrary broker topics,
//     supporting optional subscriber identity ([api.HeaderSubscriberID]) and receiving the assigned subscriber ID.
//   - [Client.Subscribe] and [Client.Unsubscribe]: dynamically adjust active subscriptions for a
//     running SSE connection via `/v1/events/subscribe` and `/v1/events/unsubscribe`.
//
// All SSE streams yield structured [api.Event] values whose typed payloads satisfy [api.EventPayload].
//
// This package depends exclusively on [github.com/masterkeysrd/tasksmith/api]
// and the Go standard library, maintaining strict separation from internal
// daemon and engine packages.
package client
