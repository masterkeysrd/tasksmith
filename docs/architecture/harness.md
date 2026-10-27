# Agent Execution Harness

The **Agent Execution Harness** (`internal/daemon/harness`) is the core execution engine that drives multi-turn agent reasoning, manages autonomous tool dispatch loops, coordinates interactive user approvals, and maintains context window health.

---

## 1. Architectural Overview

```mermaid
flowchart TD
    subgraph InputLayer ["Turn Ingestion"]
        UserTurn["User Prompt / Slash Command"]
        ContextBuild["Context Assembly & Compaction Gate"]
    end

    subgraph ExecutionGraph ["Stateful Execution Graph (Loom)"]
        InboxNode["Inbox / Queue Node\n(Command & Message Dequeuing)"]
        AgentNode["Agent Node\n(Streaming LLM Inference & Reasoning)"]
        ToolEval{"Requires Tool Calls?"}
        ToolsNode["Tools Node\n(Container Execution & Output Capture)"]
    end

    subgraph Suspension ["Interactive Suspension Boundary"]
        PermGate{"Permission or Question Required?"}
        SuspendState["Harness Suspended\n(State Checkpointed to Journal)"]
        UserResolve["User Grants / Answers via CLI"]
    end

    UserTurn --> ContextBuild --> InboxNode
    InboxNode --> AgentNode
    AgentNode --> ToolEval
    ToolEval -->|No (Final Response)| Finish["Emit Final Turn Response"]
    ToolEval -->|Yes| PermGate

    PermGate -->|Approval Needed| SuspendState
    SuspendState --> UserResolve
    UserResolve -->|Resume Execution| ToolsNode

    PermGate -->|Approved / Allowed| ToolsNode
    ToolsNode -->|Tool Results Feedback| AgentNode
```

---

## 2. Core Architectural Design

### 1. Graph-Driven Turn Loop
The harness structures agent turns as a directed execution graph built on Loom's graph runtime:
- **Node Pipeline**: Inbound turns transition through dedicated graph nodes—`command` (slash command expansion), `inbox` (queued message assembly), `agent` (LLM invocation), and `tools` (tool execution).
- **Autonomous Feedback Loop**: When the model emits tool calls, the `tools` node executes them inside the session's sandbox container, formats stdout/stderr and structured content, and feeds the outputs directly back into the `agent` node for subsequent reasoning steps. The loop continues autonomously until the model satisfies the objective or halts.

### 2. Interactive Suspension & Resumption
AI agents often encounter sensitive actions or require user clarification mid-turn:
- **Zero-Block Suspension**: When a tool requires explicit user authorization (e.g. executing an unapproved shell command) or triggers an interactive prompt (`ask_question`), the harness suspends execution immediately.
- **Durable Checkpointing**: The graph state, unfulfilled tool calls, and pending questions are serialized to the session journal, placing the session in `waiting_permission` or `waiting_question` state.
- **Seamless Resumption**: When the user approves, denies, or answers via the CLI, the daemon feeds the decisions into the suspended graph and resumes execution without re-running earlier steps.

### 3. Context Compaction & Summarization
To support long-horizon software engineering sessions without exceeding provider token limits:
- **Context Monitoring**: The harness tracks cumulative token counts across all turns against the active model's context ceiling.
- **Autonomous Compaction**: When context utilization exceeds safe operating thresholds, the harness invokes the internal `compaction-summarizer` agent. Older conversation turns are distilled into structured summary blocks while recent turns, tool outputs, and encrypted reasoning blobs (`openai.encrypted_content`) are preserved intact.

### 4. Subagents & Asynchronous Tasks
Complex workflows frequently require delegation and parallel operations:
- **Isolated Subagent Sessions**: Agents can invoke subagents for targeted investigations or self-contained refactoring passes. Subagents receive isolated workspaces or shared worktrees, preventing context clutter in the primary session.
- **Asynchronous Task Runner**: Long-running background processes (such as compilers, test suites, or dev servers) run asynchronously under the session's task manager, streaming updates via SSE without blocking the agent's conversational loop.
