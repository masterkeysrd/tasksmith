// Package harness orchestrates conversational and tool-augmented agent execution
// over a compiled [github.com/masterkeysrd/loom/graph.Graph] with durable
// SQLite-backed conversation state and permission checkpointing.
//
// # Strict Dependency Injection and Decoupling
//
// The harness is a pure workflow execution engine with zero coupling to
// workspace configuration or model provider routing. Higher-level orchestration
// layers (such as internal/daemon/session) resolve the active [*github.com/masterkeysrd/loom/llm.Model],
// [*github.com/masterkeysrd/tasksmith/internal/daemon/tools.Container], and rendered
// system prompt, injecting them directly into [New] via [Config]:
//   - [Config.DB]: Active SQLite connection used to initialize [github.com/masterkeysrd/loom/checkpoint/sqlite.Checkpointer].
//   - [Config.Model]: Resolved [*github.com/masterkeysrd/loom/llm.Model] invoked by [AgentNode].
//   - [Config.Tools]: Initialized [*github.com/masterkeysrd/tasksmith/internal/daemon/tools.Container] providing tool definitions, two-phase Prepare/Execute, and the permission [github.com/masterkeysrd/tasksmith/internal/daemon/tools.Gate].
//   - [Config.SystemPrompt]: Optional rendered system instructions prepended transiently to each LLM call.
//   - [Config.Logger]: Structured [github.com/masterkeysrd/tasksmith/internal/core/log.Interface] scoped to the "harness" component.
//
// # Cyclic Agent-Tools Workflow Graph
//
// The harness compiles a cyclic workflow graph:
//
//	START -> agent -> (conditional: has tool calls?) -> tools -> agent
//	               -> (otherwise)                    -> END
//
// Conversation history is stored directly as a [github.com/masterkeysrd/loom/message.MessageList]
// on [State.Messages], alongside any active [State.PendingPermissions] and
// [State.PermissionDecisions].
//
// # Two-Phase Tool Execution and Permission Interrupts
//
// When [AgentNode] produces an [*github.com/masterkeysrd/loom/message.Assistant]
// message containing tool calls, the graph transitions to [ToolsNode]:
//   - Pre-flight Prepare & Evaluate: Every tool call is validated via
//     [github.com/masterkeysrd/tasksmith/internal/daemon/tools.Container.Prepare] and
//     evaluated against the permission gate via [github.com/masterkeysrd/tasksmith/internal/daemon/permissions.SessionManager.Evaluate].
//   - Interrupt on AccessAsk: If one or more tool calls require user approval
//     ([github.com/masterkeysrd/tasksmith/internal/daemon/tools.AccessAsk]), [ToolsNode]
//     executes allowed calls and records denied calls before interrupting. It persists
//     the pending [github.com/masterkeysrd/tasksmith/internal/daemon/tools.PermissionRequest] slice
//     in [State.PendingPermissions] and returns a [github.com/masterkeysrd/loom/graph.InterruptUpdate]
//     command. [Harness.Stream] yields an [EventInterrupted] notification carrying
//     the pending permission requests.
//   - Strict All-or-Nothing Resume: [Harness.Stream] validates that every request
//     in [State.PendingPermissions] has a corresponding decision before invoking
//     the graph. If any pending request is missing a decision, [Harness.Stream]
//     yields [ErrPendingPermissionsUnresolved] immediately without calling the graph.
//   - Canceled by New User Message: If the caller sends a new user message via
//     [Harness.Stream] while a thread is paused at [ToolsNode], [ToolsNode]
//     automatically synthesizes canceled [*github.com/masterkeysrd/loom/message.Tool]
//     error responses for all unfulfilled tool calls, inserts them immediately
//     before the new user message to preserve strict LLM message ordering, clears
//     [State.PendingPermissions], and transitions directly to [AgentNode].
//
// # System Prompt Isolation Invariant
//
// When [Config.SystemPrompt] is non-empty, [AgentNode] prepends it only to the
// transient message slice sent to [github.com/masterkeysrd/loom/llm.Model.Invoke]
// and never stores it inside checkpointed [State.Messages].
package harness
