// Package mcp owns the live lifecycle of the MCP servers a workspace declares.
//
// # Architecture
//
// A [Manager] is created per open workspace and keeps one connection per
// effective warp.MCP resource. It is the runtime behind the workspace's MCP
// endpoints and the source of everything the /mcp inspector shows.
//
//   - Non-blocking workspace open: Servers connect and discover in the
//     background, so opening or reloading a workspace is never delayed by a slow
//     or unreachable server. A server whose connection fails reports status error
//     with the failure reason instead of failing the workspace.
//
//   - Catalog synchronization: Discovered tools are converted into raw warp.Tool
//     definitions and registered with the workspace registry through
//     SetMCPTools, which applies spec.policies filtering, names tools
//     "<server>__<tool>", and resolves effective annotations. After discovery the
//     workspace catalog is refreshed so tool lists and token counts reflect what
//     the server actually offers.
//
//   - Prohibited items stay visible: Tools, prompts, and resources blocked by
//     spec.policies are still reported with Allowed false, so callers can show
//     everything a server offers and why an item is restricted. Blocking itself is
//     enforced by the daemon against the manifest definition, which keeps the
//     guarantee independent of this package.
//
//   - Authentication: Static bearer, api-key, and basic credentials resolve from
//     spec.auth env or file entries and are sent as headers. OAuth flows are
//     delegated to the MCP SDK handlers, with acquired tokens and dynamically
//     registered clients persisted per server so daemon restarts do not force a
//     browser re-login. Interactive authorization never blocks a connection
//     attempt: the server moves to status auth_required and exposes the URL the
//     user should open.
//
//   - Environment isolation: spec.env and spec.headers values expand ${VAR}
//     placeholders from the workspace's isolated .env map, consulting the host
//     environment only for names the manifest explicitly references, so stdio
//     subprocesses never inherit unreferenced host variables.
//
// # Relationship to the workspace
//
// [Manager] implements the workspace MCPService contract. The daemon attaches it
// to a workspace runtime once manifests are loaded, calls [Manager.Sync] after
// every reload to reconcile servers with the current declarations, and
// [Manager.Close] when the runtime shuts down.
package mcp
