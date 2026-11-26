# Built-in Tools Reference

The catalog covers all 22 built-in tools. Select a tool for its inputs, outputs, safety annotations and examples. Command-backed custom tools and tools discovered from MCP servers use the same registry and permission pipeline.

---

## 1. Core Tools Catalog Summary

| Tool Name | Capability | Default Permission Behavior | Description |
| :--- | :--- | :--- | :--- |
| <span id="view"></span>[`view`](tools/filesystem.md#view) | `FS` | Safe read; allowed. | Reads whole files or specific line ranges with line numbering and SHA-256 digests. |
| <span id="ls"></span>[`ls`](tools/filesystem.md#ls) | `FS` | Safe read; allowed. | Lists directory entries with file sizes, directories, and optional recursion. |
| <span id="glob"></span>[`glob`](tools/filesystem.md#glob) | `FS` | Safe read; allowed. | Matches workspace paths against wildcard glob patterns. |
| <span id="grep"></span>[`grep`](tools/filesystem.md#grep) | `FS` | Safe read; allowed. | Searches file contents using regular expressions with match line numbers and context. |
| <span id="write"></span>[`write`](tools/filesystem.md#write) | `FS` | Workspace write; prompts. | Creates or overwrites files with atomic comparison and diff tracking. |
| <span id="edit"></span>[`edit`](tools/filesystem.md#edit) | `FS` | Workspace write; prompts. | Replaces text using exact matches first, with conservative whole-line fallbacks and a unified diff. |
| <span id="remove"></span>[`remove`](tools/filesystem.md#remove) | `FS` | Delete; prompts. | Deletes a file within the workspace with safety checks. |
| <span id="bash"></span>[`bash`](tools/execution-and-tasks.md#bash) | `Proc` | Depends on command classification. | Executes commands inside the sandbox with synchronous timeout or background task delegation. |
| <span id="web_fetch"></span>[`web_fetch`](tools/web.md#web_fetch) | `Net` | Network read; prompts. | Fetches web documents via HTTP/HTTPS with body extraction. |
| <span id="web_search"></span>[`web_search`](tools/web.md#web_search) | `Net` | Network read; prompts. | Runs a web search from the daemon host and returns ranked titles, URLs, and snippets with optional domain filters. |
| <span id="download"></span>[`download`](tools/web.md#download) | `Net`, `FS` | Network-backed write; prompts. | Downloads a remote HTTP/HTTPS resource directly into a workspace destination. |
| <span id="tasks"></span>[`tasks`](tools/execution-and-tasks.md#tasks) | `Proc`, `Session` | List/status allowed; kill/send_input prompt. | Lists, inspects, feeds input to, or terminates running background tasks. |
| <span id="todos"></span>[`todos`](tools/agents-and-planning.md#todos) | `Session` | Safe read; allowed. | Manages the session progress checklist, tracking pending and completed items. |
| <span id="ask_question"></span>[`ask_question`](tools/agents-and-planning.md#ask_question) | `Session` | Safe read; allowed. | Prompts the user with structured interactive questions (`single`, `multi`, `text`, `confirm`). |
| <span id="enter_plan_mode"></span>[`enter_plan_mode`](tools/agents-and-planning.md#enter_plan_mode) | `Session` | Safe read; allowed. | Transitions a top-level session into `plan` mode and returns the session's safe `<session-dir>/plans/` directory. |
| <span id="exit_plan_mode"></span>[`exit_plan_mode`](tools/agents-and-planning.md#exit_plan_mode) | `Session` | Safe read; allowed. | Submits an existing plan document for user review and exits `plan` mode upon approval. |
| <span id="load_skill"></span>[`load_skill`](tools/workspace-resources.md#load_skill) | `Session`, `Workspace` | Safe read; allowed. | Loads domain guidelines, coding conventions, or workflows for a specified skill on demand. |
| <span id="load_rule"></span>[`load_rule`](tools/workspace-resources.md#load_rule) | `Session`, `Workspace` | Safe read; allowed. | Loads the full instructions of a conditional workspace rule by name. Auto-activated when the agent has at least one conditional rule. |
| <span id="read_mcp_resource"></span>[`read_mcp_resource`](tools/workspace-resources.md#read_mcp_resource) | `Net`, `Workspace` | Safe read; allowed. | Reads one resource URI from a configured MCP server. Auto-activated when a connected server exposes a permitted resource. |
| <span id="invoke_agent"></span>[`invoke_agent`](tools/agents-and-planning.md#invoke_agent) | `Session` | Safe read; allowed. | Spawns a specialized subagent asynchronously in an isolated child session. |
| <span id="manage_agents"></span>[`manage_agents`](tools/agents-and-planning.md#manage_agents) | `Session` | List/status/models allowed; send/kill prompt. | Lists subagents, checks subagent status, sends messages, terminates running subagents, or discovers subagent models. |
| <span id="tool_search"></span>[`tool_search`](tools/workspace-resources.md#tool_search) | `Session`, `Workspace` | Safe read; allowed. | Searches deferred (not eagerly loaded) tools by name, description, or parameters via BM25 and loads the matches into context. Auto-activated when the session has at least one deferred tool. |

---

## 2. Permission Modes & Safety Annotations

TaskSmith evaluates every tool invocation against the active session **Permission Mode** and the tool's declarative safety annotations:

### Safety Annotations

Every built-in tool defines safety properties in its manifest:

- **`isReadOnly`**: When `true`, tool execution does not modify filesystem or external system state.
- **`isIdempotent`**: When `true`, repeating the same operation does not introduce additional effects.
- **`isDangerous`**: When `true`, execution could alter environment, process state, or system integrity.
- **`isOpenWorld`**: When `true`, tool interacts with unconstrained inputs, shell interpreters, or network hosts.
- **`userHint`**: Template string displayed in confirmation prompts (e.g., `"Run command: %s"`).

### Behavior across permission modes

These defaults apply after policy checks, in a trusted workspace without matching rules. Built-ins prepare a plan for the actual target and action; their manifest annotations alone do not decide approval.

| Mode | Allowed without prompting | Other plans |
| :--- | :--- | :--- |
| `default` | Safe read-only plans | Prompt |
| `accept_edits` | Safe read-only plans and writes not classified unsafe (including `download`) | Prompt, including ordinary deletes |
| `plan` | Safe read-only plans | Prompt |
| `ask` | Safe read-only plans | Prompt |
| `auto` | Safe non-delete plans; non-unsafe plans with an explicit allow rule | Send for review, not unconditional approval |

- Explicit denies prevail. Restricted workspaces deny mutations except session plan files. Writes, edits and removals in `<session-dir>/plans/` are normally allowed in all five modes.
- Safe shell reads can run without a prompt. `autoAllowBash` can also allow a safe command inside a native sandbox in `default` or `accept_edits`; it does not authorize a host retry.
- Outside-workspace and sensitive paths are assessed separately. See [permissions](../guides/permissions.md) for rules, policy ceilings and grants.

## 4. Custom Tool Execution Model

Custom tools are command-backed tools declared as [`*.tool.md` manifests](manifests/tool.md). They register into the same container as built-ins and are evaluated by the same permission pipeline, but their security posture is **declared, never inferred**.

### Declarative Annotations As The Security Baseline

The approval plan for a custom tool is derived strictly from `spec.annotations`. TaskSmith does not inspect argument values, guess file paths from argument key names, or pattern-match the command string to decide whether a call is safe. The same mapping drives [MCP tools](#6-mcp-tool-execution-model), which carry annotations instead of a command:

| Annotation state | Plan action | Safety | Read-only |
| :--- | :--- | :--- | :--- |
| `isDangerous: true` | `exec` | `unsafe` | `false` |
| `isReadOnly: true` (and not `isOpenWorld`) | `read` | `safe` | `true` |
| `isReadOnly: true` and `isOpenWorld: true` | `read` | `unknown` | `true` |
| `isOpenWorld: true` only | `net` | `unknown` | `false` |
| No annotations set | `exec` | `unknown` | `false` |

Approval prompts show the rendered command line as the plan target, and `annotations.userHint` overrides the summary when set (`%s` is replaced with the rendered command line). Because safety defaults to `unknown`, an unannotated custom tool is never treated as auto-allowed safe; annotations must reflect real behavior and do not replace sandboxing or permission checks. See [Permission Modes & Safety Annotations](#2-permission-modes--safety-annotations) for how each mode resolves these plans.

### Argument Delivery: Template Arguments Or Stdin JSON

Arguments reach a custom tool's process in exactly one of two ways, never both:

| Manifest authoring | Delivery |
| :--- | :--- |
| A `spec.command` element interpolates a supplied argument via `{{.Input.<field>}}` (or the `{{.<field>}}` shorthand) | The rendered argv is the only channel; standard input is left closed. |
| No `spec.command` element consumes an argument | The validated arguments are marshalled as one JSON object on standard input. |

Two consequences matter when authoring a manifest:

- **Templates are opt-in.** An explicit `{{ … }}` action is what enables command rendering. A command containing only shell-style references (`$var`, `${var}`, `$1`) is executed verbatim, so conventional CLI scripts keep their own positional parameters instead of having them rewritten as templates.
- **A template referencing an argument the model did not supply fails the call** as an argument validation error rather than executing with a literal `<no value>` placeholder, which the model can correct and retry.

Full template reference and schema semantics live in the [Tool manifest reference](manifests/tool.md#command-templating).

### Execution And Output

- Execution runs through the sandbox process API and is subject to the permission system. The process environment is the sandbox configuration plus `spec.env`; the host daemon environment is never inherited.
- A custom tool call blocks for the same 60-second wait window the built-in `bash` tool uses. A tool still running when the window elapses is moved to the background like a bash task: the call returns a `<background_task>` notice with a `task_id`, the process keeps running killable through the [`tasks`](tools/execution-and-tasks.md#tasks) tool, and the agent is woken with a completion notification when it finishes. A hung tool therefore no longer blocks its turn indefinitely. Unlike a background bash task, a backgrounded custom tool does not accept interactive input — its standard input is fixed at launch (the JSON payload, or closed when arguments were templated), so `tasks` `send_input` reports it as unsupported.
- Because execution shares the standard operations path with `bash`, tracked files a custom tool modifies or deletes are detected and recorded in the session change journal as external changes, reviewable through [`/changes`](cli/keybindings.md#changes-browser-changes) and revertible through `/revert`.
- Standard output is returned to the model as text. When it exceeds the output limits, the full content is spilled to a file and the model receives a head-plus-tail preview pointing at that file.
- A nonzero exit code reports standard error, then standard output if standard error is empty, then the exit code alone.
- `spec.outputSchema` is advertised metadata. Output is not parsed or validated against it, and an absent or unparseable schema defaults to a string schema.

---

## 5. Tool Partitioning: Eager vs Deferred

Every session partitions its policy-allowed toolset into an eager set and a deferred set. The partition is computed per agent, so the same tool can be eager for one agent and deferred for another.

| Partition | Determined by | Model visibility |
| :--- | :--- | :--- |
| **Eager** | Named in the agent's `spec.tools` | Present in the initial tool context. |
| **Deferred** | Policy-allowed but not named in `spec.tools` | Registered in the session container and withheld from the initial context. |
| **Denied** | Blocked by workspace, agent, or MCP server tool policy | Not registered. |
| **Inactive** | Neither loaded, deferred, nor denied (for example, a conditional built-in the session does not activate) | Not registered. |

Custom (`*.tool.md`) and MCP tools are **deferred by default**, because they are not listed in an agent's `spec.tools` unless the author opts them in. The three conditional built-ins — `tool_search`, `load_rule`, and `read_mcp_resource` — are the exception: TaskSmith activates them itself when the workspace gives them something to do, so they may be eager without appearing in `spec.tools`.

### MCP Tools

Tools discovered from a connected [MCP server](manifests/mcp.md) are named `<server>__<tool>` and are attributed to that server in listings, whose scope reads `MCP (<server>)`.

They stay deferred even when an agent lists `spec.tools: ["*"]`, because a server can expose dozens of tools and their schemas are large. To load one on the first request, name it outright using its canonical `<server>__<tool>` name:

```yaml
spec:
  tools:
    - "*"
    - github__get_issue
```

A tool the server's own `spec.policies.tools` prohibits is never registered and reports status `denied`, so it stays visible in [`/tools`](cli/slash-commands.md#tools-browser-tools) with an explanation rather than disappearing. See [Scoping MCP resources](../guides/tools-and-mcp.md#use-mcp-prompts-and-resources).

Discovery is asynchronous, so a server that is still `connecting` when a session starts contributes no tools to its partition. When the connection completes, open sessions re-run the partition and the server's tools become invocable without a restart or a model change; the same refresh activates [`read_mcp_resource`](tools/workspace-resources.md#read_mcp_resource) if the server publishes a permitted resource.

How a discovered tool is executed and how its annotations drive approvals is covered in [MCP Tool Execution Model](#6-mcp-tool-execution-model).

### Activation Through `tool_search`

When a session resolves at least one deferred tool, TaskSmith automatically activates the built-in [`tool_search`](tools/workspace-resources.md#tool_search) tool as an eager tool and renders a `<deferred_tools>` directory in the system prompt. `tool_search` runs BM25 keyword matching over deferred tool names, descriptions, and parameters; matched tools become directly invocable on subsequent turns. When a session has no deferred tools, `tool_search` is not registered at all.

Deferring a tool therefore trades first-turn availability for context cost: the tool's full schema is charged to the context only after the agent demonstrates it needs it.

### Inspecting The Partition

Run [`/tools`](cli/slash-commands.md#tools-browser-tools) in the CLI to browse every workspace and built-in tool with its resolved status (`[Eager]`, `[Deferred]`, `[Denied]`, `[Inactive]`), scope, safety annotations, and JSON schemas for the active agent.

---

## 6. MCP Tool Execution Model

Every tool a connected [MCP server](manifests/mcp.md) advertises is registered as a first-class tool in the same container as built-ins and custom tools, under the name `<server>__<tool>`. It is validated, permission-checked, and output-truncated by the same pipeline; what differs is where its definition and its safety posture come from.

### Definition Sources

| Facet | Source |
| :--- | :--- |
| Name | `<server>__<raw tool name>`, so two servers exposing the same tool name never collide. |
| Description | The description the server sent during discovery; a tool that sends none gets a synthesized fallback naming the server and the tool. An MCP manifest cannot supply a per-tool description — its `metadata.description` describes the server, not its tools. |
| Input schema | The JSON schema the server advertised; call arguments are validated against it before the request is sent. A tool whose schema is absent or unparseable is advertised as taking an arbitrary object, so it stays callable rather than dropping out of the catalog. |
| Output schema | The schema the server advertised, else a string schema. Output is not validated against it. |
| Safety annotations | The server manifest's `overrides` entry for the tool, else its `annotations`, else what the server itself reported during discovery — so an author can raise or lower what a server claims. See [MCP Server Manifest](manifests/mcp.md). |

### Approval Plans

The plan is derived from the resolved annotations through the [same mapping as custom tools](#declarative-annotations-as-the-security-baseline), so a tool declared read-only and not open-world auto-allows in `default` mode while an unannotated tool surfaces a prompt. Declaring a tool read-only is not on its own enough to be auto-allowed: a tool that is also open-world keeps `unknown` safety and still prompts. Two details differ:

| Aspect | Custom tool | MCP tool |
| :--- | :--- | :--- |
| Plan target | Rendered command line | `<server>/<raw tool name>` |
| Default summary | `Run custom tool: <name>` | `Run MCP tool: <name>` |

Keying the target on the server name means an approval granted to one server's tools can never cover a same-named tool on another server, and a grant can be scoped to a single server with a trailing glob.

### Prohibited Tools Are A Hard Block

A tool the server manifest blocks under `spec.policies.tools` is rejected before the request leaves TaskSmith, with an error naming the policy section and the manifest path relative to the workspace root. The check runs against the live workspace catalog on every call rather than against a snapshot taken when the container was built, so a reload that tightens `spec.policies` takes effect immediately for sessions that already hold the tool. There is no per-call override for a prohibited tool: widening access means editing `spec.policies.tools`. The `allowProhibited` escape hatch covers prompts and resource reads only — available to a user through the `/mcp` confirmation or the [MCP HTTP endpoints](daemon-api.md#mcp-http-endpoints) — and the agent-facing tool path never sets it, so an agent cannot escalate its own access. See [Scoping MCP resources](../guides/tools-and-mcp.md#use-mcp-prompts-and-resources).

### Result Aggregation

An MCP call streams content back as chunks, which TaskSmith flattens into a single tool result:

| Chunk content | Handling |
| :--- | :--- |
| Text | Appended to the result text; the whole is spilled to a file when it exceeds output limits, leaving the model a head-plus-tail preview. |
| Image, audio, video, document | Passed through to the model unmodified, since re-truncating binary payloads would corrupt them. |
| Structured content | The last non-empty value is retained. When the server returned only structured content, it is serialized as JSON in the result text. |
| Chunk flagged `isError` | Marks the whole result as a tool error, so the model sees the server's own failure message instead of a transport exception. |

A transport failure is reported the same way — as an error result carrying the failure text — so a broken server never surfaces as an empty successful result. A call that yields nothing at all still returns a result rather than silence: `MCP tool returned no content`, or `MCP tool reported an error` when the server flagged the chunk as an error without sending text.
