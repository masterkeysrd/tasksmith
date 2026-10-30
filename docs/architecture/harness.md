# Agent Execution Harness

The **Agent Execution Harness** (`internal/daemon/harness`) is the core execution engine that drives multi-turn agent reasoning, manages autonomous tool dispatch loops, coordinates interactive user approvals, and maintains context window health.

---

## 1. Architectural Overview

```mermaid
flowchart TD
    subgraph InputLayer ["Turn Ingestion"]
        UserTurn["User Prompt / Slash Command"]
        ContextBuild["Context Assembly & State Evaluation"]
    end

    subgraph ExecutionGraph ["Stateful Execution Graph (Loom)"]
        EntryRouter{"Should Compact?"}
        CompactNode["Compaction Node\n(In-Chat Distillation & State Replacement)"]
        InboxNode["Inbox / Queue Node\n(Command & Message Dequeuing)"]
        AgentNode["Agent Node\n(Streaming LLM Inference & Reasoning)"]
        ToolEval{"Requires Tool Calls?"}
        ToolsNode["Tools Node\n(Container Execution & Output Capture)"]
        PostToolRouter{"Context Exceeds Trigger?"}
    end

    subgraph Suspension ["Interactive Suspension Boundary"]
        PermGate{"Permission or Question Required?"}
        SuspendState["Harness Suspended\n(State Checkpointed to Journal)"]
        UserResolve["User Grants / Answers via CLI"]
    end

    UserTurn --> ContextBuild --> EntryRouter
    EntryRouter -->|Manual / Over Budget| CompactNode
    EntryRouter -->|Normal Flow| InboxNode
    CompactNode -->|Auto-Compacted| AgentNode
    CompactNode -->|Manual Complete| Finish["Emit Final Turn Response"]
    InboxNode --> AgentNode
    AgentNode --> ToolEval
    ToolEval -->|No (Final Response)| Finish
    ToolEval -->|Yes| PermGate

    PermGate -->|Approval Needed| SuspendState
    SuspendState --> UserResolve
    UserResolve -->|Resume Execution| ToolsNode

    PermGate -->|Approved / Allowed| ToolsNode
    ToolsNode --> PostToolRouter
    PostToolRouter -->|Exceeds Trigger| CompactNode
    PostToolRouter -->|Within Budget| InboxNode
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
- **Zero-Block Suspension**: Calls that require authorization or trigger an interactive prompt (`ask_question`) remain pending, while other allowed calls in the same batch execute before the harness suspends.
- **Durable Checkpointing**: The graph state, unfulfilled tool calls, and pending questions are serialized to the session journal, placing the session in `waiting_permission` or `waiting_question` state.
- **Seamless Resumption**: When the user approves, denies, or answers via the CLI, the daemon feeds the decisions into the suspended graph and resumes execution without re-running earlier steps.
- **Protected Suspension**: Manual compaction is rejected while an interaction is pending; queued commands and background task notifications wait for resolution. A new user message can instead cancel the pending tool calls.

### 3. Context Compaction & Summarization

In long-running, multi-turn software engineering workflows, the conversation history rapidly accumulates user turns, file contents, code diffs, command outputs, and intermediate reasoning steps. Without intervention, this accumulation degrades model reasoning performance ("lost in the middle"), inflates per-turn inference latency and cost, and eventually causes hard token-limit failures from model providers.

Context Compaction is the harness subsystem responsible for preventing token exhaustion by distilling verbose conversation history into an authoritative, structured context summary while preserving task continuity, user objectives, and prompt cache warmth.

#### 1. In-Chat Compaction Philosophy
Traditional multi-agent frameworks often delegate summarization to secondary "summarizer" models or separate subagent contexts with lower temperature settings. TaskSmith intentionally adopts an **in-chat, same-model compaction strategy**:
- **Prompt Cache Warmth**: Using the active session model with identical system prompts and tool definitions guarantees that the long static prefix remains cached in upstream provider engines (such as Anthropic prompt caching or OpenAI prefix caching), preventing expensive cache invalidations.
- **Cognitive Consistency**: The model performing the work summarizes its own context, ensuring that nuanced architectural decisions, subtleties in file edits, and active constraints are interpreted with the same reasoning fidelity.
- **Zero Configuration Drift**: The compaction step inherits the session's active reasoning effort and model settings without requiring auxiliary configuration or secondary provider credentials.

#### 2. Dynamic Safety Reserve & Trigger Formula
To ensure turns complete reliably without encountering provider context limits during response generation, the harness calculates dynamic operating boundaries based on the active model's profile limits:

$$\text{ReserveTokens} = \max(\lfloor\text{ContextLimit} \times 0.20\rfloor, \text{OutputLimit} + 4096)$$
$$\text{TriggerTokens} = \text{ContextLimit} - \text{ReserveTokens}$$

| Metric | Source | Purpose |
| :--- | :--- | :--- |
| **`ContextLimit`** | Model Provider / Catalog | Total token capacity supported by the active model (e.g. 128,000 or 200,000). |
| **`OutputLimit`** | Model Provider / Catalog | Maximum tokens the model can produce in a single completion turn (e.g. 8,192 or 16,384). |
| **`ReserveTokens`** | Harness Calculation | Guaranteed safety buffer reserved for model generation and upcoming user prompts. |
| **`TriggerTokens`** | Harness Calculation | Utilization threshold that triggers autonomous compaction. |
| **`UsedTokens`** | Recomputed State | Cumulative token count of static prefix + conversation messages, calibrated against provider usage metrics. |

#### 3. Token Accounting & Calibration Strategy
Context utilization is measured through a **hybrid ground-truth + delta accounting** model that trades a full re-tokenization on every state update for a provider-anchored total plus a fast heuristic for the uncounted tail:

- **Provider Anchoring**: After each completion turn, `UsedTokens` is anchored to the upstream provider's exact tokenizer count for the most recent assistant message (its prompt and output tokens), keeping the running total calibrated against the model's own tokenizer rather than a heuristic.
- **Incremental Trailing Delta**: Messages appended *between* assistant turns—tool executions, structured outputs, and new user prompts—have not yet been counted by the provider, so the harness adds a fast, incremental heuristic estimate for only that trailing delta. Before the first completion (when no provider metrics exist yet), the entire conversation is estimated heuristically.
- **Code-Calibrated Density (3.0 chars/token)**: The heuristic estimates tokens as ⌈bytes ÷ 3.0⌉. Standard English prose averages ~4.0 characters per token, but software-engineering transcripts are denser—indentation, code symbols, diff syntax (`+`/`-`/`@@`), and tool-call JSON schemas pack fewer characters per token. Calibrating to 3.0 yields more conservative estimates than a prose-tuned 4.0 density, preventing the systematic *undercounting* that would otherwise let utilization silently cross `TriggerTokens` and risk a hard provider context-limit failure.

**Defense-in-Depth Safety Invariants.** Two invariants guarantee the measured usage can never understate true consumption:
- **Cache-Bounded Floor**: `UsedTokens` is bounded below by `CachedTokens`, since the prompt cache is always a subset of a turn's input tokens.
- **Monotonic Non-Decrease**: Absent compaction, a recomputed conversation total is never allowed to fall below the previously reported `UsedTokens`, so a transient estimation dip can never mask real context growth and delay compaction.

#### 4. Execution Lifecycle & Trigger Paths
The harness supports two primary compaction entry points:

| Trigger Mode | Activation Source | Graph Routing & Behavior |
| :--- | :--- | :--- |
| **Autonomous Compaction** | Context utilization reaches or exceeds `TriggerTokens` post-tool or on turn entry. | The harness autonomously routes through the `compact` node before continuing execution. Following summarization, the harness routes directly to `agent`, allowing the agent to continue solving the active objective uninterrupted. |
| **Manual Compaction** | Explicit compaction request received via daemon API. | The harness executes the `compact` node with optional user-supplied focus guidelines. Following summarization, the session transitions cleanly to `idle`. |

If prompt rendering or model invocation fails, or the model returns an empty summary, the turn fails without replacing the conversation. The checkpoint remains at `compact`, so graph resumption retries compaction before continuing.

#### 5. Structured Summary Anatomy
When compaction executes, the harness distills history into a standardized context summary message:

```markdown
<context_summary>
# User Requests (Chronological)
1. Initialize repository structure and setup service endpoints.
2. Implement SQLite persistent store with transaction rollback tests.
3. Configure authentication middleware and token validation.

# Active Todos
- [x] Configure SQLite migration runner
- [>] Implement bearer token validation (in progress)
- [ ] Add endpoint rate limiting

# Conversation Summary
## Current State
Working on authentication middleware integration and request validation.

## Files & Changes
- `pkg/auth/middleware.go`: Added bearer token parser and claims validator.
- `pkg/auth/token.go`: Defined TokenClaims and TokenStore interfaces.

## Technical Context
Enforcing strict error propagation and structured logging across middleware chains.

## Strategy & Exact Next Steps
1. Complete claims validation in `pkg/auth/middleware.go`.
2. Run service test suite to verify end-to-end authentication flow.
</context_summary>

Continue working on the latest user request above from the current state without asking for confirmation or repeating completed steps.
```

- **Chronological User Requests**: Preserves the complete sequence of user objectives across previous compactions and subsequent turns, preventing drift from the overarching goal.
- **Active Todos**: Retains active task items and completion states directly from session state.
- **Technical Progress & File Locations**: Captures modified files, relevant symbols, architecture decisions, and remaining steps.
- **Continuity Directive**: Appends an explicit continuation instruction for autonomous compactions so the model immediately resumes execution without awaiting external confirmation.

#### 6. Atomic State Replacement & Persistence
1. **Message History Replacement**: The prior conversation message list is atomically replaced with the single `<context_summary>` message tagged with system metadata (`tasksmith.system.kind = compaction`).
2. **Context Recalibration**: Session context metrics are recomputed (`UsedTokens` drops substantially, `Compactions` counter increments, and `LastCompacted` timestamp updates).
3. **Durable Timeline Journaling**: A `TimelineKindCompacted` record containing `BeforeTokens`, `AfterTokens`, `Trigger`, `Focus`, and `DurationMs` is appended to the SQLite timeline.
4. **Event Emission**: The harness emits `compaction_started` and `context_updated` stream events, allowing upstream consumers (daemon broker, SSE clients) to observe context transitions in real time.

### 4. Subagents & Asynchronous Tasks
Complex workflows frequently require delegation and parallel operations:
- **Isolated Subagent Sessions**: Agents can invoke subagents for targeted investigations or self-contained refactoring passes. Subagents receive isolated workspaces or shared worktrees, preventing context clutter in the primary session.
- **Asynchronous Task Runner**: Long-running background processes (such as compilers, test suites, or dev servers) run asynchronously under the session's task manager, streaming updates via SSE without blocking the agent's conversational loop.
