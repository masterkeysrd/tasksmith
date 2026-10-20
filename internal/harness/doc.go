// Package harness orchestrates conversational agent execution over a compiled
// [github.com/masterkeysrd/loom/graph.Graph] and durable SQLite-backed
// conversation state checkpointing.
//
// # Strict Dependency Injection and Decoupling
//
// The harness is a pure workflow execution engine with zero coupling to
// workspace configuration or model provider routing. Higher-level orchestration
// layers (such as internal/session) resolve the active [*github.com/masterkeysrd/loom/llm.Model]
// and rendered system prompt, injecting them directly into [New] via [Config]:
//   - [Config.DB]: Active SQLite connection used to initialize [github.com/masterkeysrd/loom/checkpoint/sqlite.Checkpointer].
//   - [Config.Model]: Resolved [*github.com/masterkeysrd/loom/llm.Model] invoked by [AgentNode].
//   - [Config.SystemPrompt]: Optional rendered system instructions prepended transiently to each LLM call.
//   - [Config.Logger]: Structured [github.com/masterkeysrd/tasksmith/internal/core/log.Interface] scoped to the "harness" component.
//
// # Graph Architecture and Native Loom Messages
//
// In Iteration 1, the harness compiles a linear workflow graph:
//
//	START -> agent -> END
//
// Conversation history is stored directly as a [github.com/masterkeysrd/loom/message.MessageList]
// on [State.Messages], eliminating intermediate domain translation and enabling
// polymorphic JSON serialization across user, assistant, system, and tool turns.
//
// Each invocation of [Harness.Stream] appends a cloned user [github.com/masterkeysrd/loom/message.Message]
// to the thread's [State], executes [AgentNode], yields incremental
// [EventChunk] fragments ([github.com/masterkeysrd/loom/message.AssistantChunk])
// over a standard [iter.Seq2] iterator, persists the updated checkpoint in SQLite,
// and yields a terminal [EventCompleted] event carrying the assembled
// [*github.com/masterkeysrd/loom/message.Assistant] reply.
//
// # System Prompt Isolation Invariant
//
// When [Config.SystemPrompt] is non-empty, [AgentNode] prepends it only to the
// transient message slice sent to [github.com/masterkeysrd/loom/llm.Model.Invoke]
// and never stores it inside checkpointed [State.Messages]. This guarantees that
// changes to system instructions take effect immediately on subsequent runs of
// existing threads without polluting persisted conversation history.
package harness
