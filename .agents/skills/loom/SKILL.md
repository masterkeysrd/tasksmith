---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
    name: loom
    description: "Instructions and architectural patterns for building LangGraph-style AI agent workflows using the Loom framework."
spec:
    useWhen: "designing agent workflows or state machines, implementing graph nodes and routing logic, managing agent conversation memory, configuring LLM streaming or persistence, integrating external tools or MCP servers, or setting up human-in-the-loop checkpoints"
    keywords: [loom, workflows, langgraph, state-machines, agent-graph]
---

# Loom Agent Workflows

TaskSmith relies on **Loom** (`github.com/masterkeysrd/loom`), a high-performance, graph-based AI workflow engine for Go. 

## Key Loom Features
- **State-First Architecture**: Graphs are defined as state machines with nodes and conditional edges, enabling loops and resumes.
- **Advanced Memory**: Includes token-limit summarization and precise sliding-window trimming.
- **Provider Agnostic**: Seamlessly binds to OpenAI, Anthropic, Gemini, Ollama, and MCP servers.
- **Real-time Streaming**: First-class support for streaming tool execution and token generation.

## 1. TaskSmith Implementation (`internal/agent/graph`)

The core agent graph is implemented in `internal/agent/graph/graph.go`. It orchestrates the lifecycle through primary nodes:

### `check_inbox` Node
- Drains new messages from the `InboxProvider`.
- **System Prompt Injection**: Calls `InjectReminders()` to dynamically append invisible system constraints (e.g., warning if Todos are empty).
- Returns a `graph.Update` to append messages to the state.

### `think` Node
- Takes `AgentState.Messages` and calls `tools.RehydrateMessagesForLLM()` to reload binary data (like images) from disk.
- Prepends the `systemPrompt`.
- Calls `a.model.Invoke()`.
- **Streaming**: Fires a `stream.Event{Name: "agent_message"}` so the UI starts rendering immediately.

### `execute_tools` Node
- Scans the last assistant message for tool calls.
- **Permission Interception**: Evaluates tools via `permissions.EvaluateToolCall()`. Returns `interruptUpdate` if user authorization is required.
- **Execution**: Calls the tool via `container.Call()`.
- **Hooks**: Fires `ToolStateHook` callbacks (defined in `hooks.go`) so tools can mutate `AgentState` directly.

## 2. Graph Orchestration (`graph` package)
Loom agents are directed graphs where shared state is transformed by nodes.
- **Routing Edges**: Use `AddRouteEdge` for dynamic, multi-destination branching based on state.
- **Commands**: Nodes return `graph.Command[State]`. 
  - `graph.Update(func(s State) State)` mutates state.
  - `graph.Interrupt()` pauses execution (saves state to checkpointer).

## 3. LLM Interaction & Registry (`llm` package)
- Use `model.Invoke` for blocking or `model.Stream` for real-time.
- `message.TokenMetrics` attached to `Assistant` messages tracks tokens, cost, and latency.

## 4. Tool Development (`tool` package)
- Use `tool.NewContainer()` to group tools.
- Bind to the model with `model = model.BindToolDefs(container.Definitions()...)`.

## 5. Streaming & Persistence
- `g.Stream` returns an iterator of `StreamEvent` for real-time chunks.
- Checkpoints (e.g., `sqlite.NewCheckpointer(db)`) enable resuming interrupted workflows.

## Specialized Resources and Guides

Detailed guides on Loom are available in the local directory:
- [State Management](./resources/state_management.md): Essential details on deep-copy requirements for `AgentState`.
- [Loom Developer Guide](./references/loom_developer_guide.md): Expert guidance for extending Loom.
- [Conversations Guide](./references/conversations.md)
- [LLM Package Guide](./references/llm-package.md)
- [Tools Integration](./references/tools.md)
- [Memory Management](./references/memory.md)
- [Human-In-The-Loop](./references/hitl.md)
- [Persistence & State](./references/persistence.md)
- [MCP Integration](./references/mcp.md)

Whenever you are working with Loom, read these references as needed.
