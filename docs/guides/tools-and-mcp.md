# Tools & MCP Servers

TaskSmith provides a robust tool execution system that equips agents with capabilities to inspect files, execute commands, manipulate code, and integrate external services. You can extend this foundation with custom project tools and Model Context Protocol (MCP) servers.

---

## Built-in Tools

TaskSmith ships with core tools built directly into the daemon binary and executed within an isolated, path-confined sandbox:

| Tool | Action Type | Safety | Description |
| :--- | :--- | :--- | :--- |
| `view` | `read` | `safe` | Read file contents or specific line ranges with line numbering. Supports images, PDFs, and syntax highlighting. |
| `write` | `write` | `outside` / `unsafe` | Create new files or overwrite existing files in the workspace. |
| `edit` | `write` | `outside` / `unsafe` | Perform precise, surgical find-and-replace edits across files. |
| `bash` | `exec` | Evaluated by AST | Execute shell commands in the workspace environment or spawn long-running background tasks. |
| `grep` | `read` | `safe` | Search for regex patterns across files with line numbers and file path filtering. |
| `glob` | `read` | `safe` | Find files matching wildcard patterns (e.g., `**/*.go`, `src/**/*.tsx`). |
| `web_fetch`| `net` | `outside` | Fetch web pages or REST API endpoints, converting HTML into clean Markdown for context injection. |
| `ask_question` | `read` | `safe` | Prompt the user with interactive single-select, multi-select, or free-form questionnaires. |
| `todos` | `write` | `safe` | Create and update the session's structured task checklist. |
| `load_skill` | `read` | `safe` | Load specialized domain instructions and runbooks from a workspace or built-in skill. |
| `invoke_agent` | `exec` | `safe` | Spawn an asynchronous child subagent session (optionally in an isolated Git worktree). |
| `manage_agents` | `exec` | `safe` | List child subagents, check their status and output, send follow-up messages, or kill them. |

For complete parameter schemas and tool options, see the [Built-in Tools Reference](../reference/tools.md).

---

## Creating Custom Tools (`*.tool.md`)

Custom tools allow agents to execute project-specific scripts, test runners, linters, or external utilities. Tools are authored as Markdown files with YAML frontmatter specifying their parameters and sandbox behavior.

### Where Tools Live

- **Project Scope**: `.agents/tools/<name>.tool.md` (or `.agents/tools/<name>.tool.yaml`)
- **Global User Scope**: `~/.config/tasksmith/tools/<name>.tool.md`

### Example: Docker Service Inspector Tool

Create `.agents/tools/docker-service-logs.tool.md`:

```markdown
---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: docker-service-logs
  displayName: Docker Service Logs
  description: Fetches recent container logs for a running docker-compose service.
spec:
  command:
    - docker
    - compose
    - logs
    - "--tail"
    - "{{.tail}}"
    - "{{.service}}"
  inputSchema:
    type: object
    properties:
      service:
        type: string
        description: Name of the docker-compose service (e.g., "db", "redis", "api").
      tail:
        type: integer
        description: Number of recent log lines to retrieve.
        default: 50
    required:
      - service
  outputSchema:
    type: string
  annotations:
    isReadOnly: true
    isDangerous: false
    isOpenWorld: false
    userHint: "Fetch logs for docker service: %s"
---

# Docker Service Logs

Use this tool when debugging containerized services or database connection issues in local development.
Always inspect logs before attempting to restart failing containers.
```

### Tool Annotations
- **`isReadOnly`**: Marks the tool as safe for read-only exploration modes (such as `plan` mode).
- **`isDangerous`**: Flags potentially destructive tools, ensuring they trigger confirmation even in lenient permission modes.
- **`userHint`**: Defines the prompt displayed to the user when requesting authorization in the CLI.

### How Arguments Reach Your Script

A tool whose `spec.command` contains an explicit `{{ … }}` action is rendered per call. When at least one command element interpolates a supplied argument, the rendered argv is the only channel and standard input is left closed. When no command element consumes an argument, the validated arguments are sent as a single JSON object on standard input instead, so a plain CLI script can simply read and parse stdin:

```markdown
spec:
  command: [python, scripts/format.py]
```

Templates are opt-in: a command containing only shell-style references (`$var`, `${var}`, `$1`) is executed verbatim, so conventional scripts keep their own positional parameters. A template that references an argument the model did not supply fails the call as an argument validation error rather than executing with a literal `<no value>` placeholder.

The security posture of a custom tool comes strictly from its annotations — TaskSmith never infers risk from argument values or command strings — so an unannotated tool is never treated as auto-allowed safe. See [Custom Tool Execution Model](../reference/tools.md#4-custom-tool-execution-model) for the full annotation-to-permission mapping.

### Checking What The Agent Actually Sees

Run `/tools` to browse every built-in and custom tool with its status for the active agent. A tool that is `[Deferred]` is registered but withheld from the initial context until the agent finds it through `tool_search`; to make a custom tool `[Eager]`, list its name in the agent's `spec.tools`. `[Denied]` means a workspace or agent tool policy blocks it, and `[Inactive]` means the current session does not activate it at all. Press `i` in the browser to inspect a tool's rendered command, annotations, and JSON schemas.

---

## Connecting Model Context Protocol (MCP) Servers

TaskSmith supports the open **Model Context Protocol (MCP)**, allowing agents to access tools, resources, and prompt templates exposed by local or remote MCP servers.

### Where MCP Configurations Live

- **Project Scope**: `<subproject>/.agents/mcps/<name>.mcp.yaml`
- **Workspace Scope**: `.agents/mcps/<name>.mcp.yaml`
- **Global User Scope**: `~/.config/tasksmith/agents/mcps/<name>.mcp.yaml` (`$XDG_CONFIG_HOME/tasksmith/agents/mcps/`)

### 1. Stdio MCP Server (Local Process)

Stdio MCP servers run as child processes communicating over standard input and output:

```yaml
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: github
  displayName: GitHub MCP
  description: Access GitHub issues, pull requests, and repository metadata.
spec:
  type: stdio
  command:
    - npx
    - "-y"
    - "@modelcontextprotocol/server-github"
  env:
    GITHUB_PERSONAL_ACCESS_TOKEN: "${GITHUB_TOKEN}"
  annotations:
    isReadOnly: false
    isDangerous: false
  policies:
    tools:
      include:
        - "get_issue"
        - "list_issues"
        - "create_issue"
      exclude:
        - "delete_repo"
```

### 2. HTTP / SSE MCP Server (Remote Endpoint)

Remote MCP servers connect over HTTP using Server-Sent Events (SSE):

```yaml
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: sentry
  displayName: Sentry Error Monitoring
  description: Search and inspect production exceptions and error traces.
spec:
  type: sse
  endpoint: https://mcp.sentry.internal.acme.com/sse
  auth:
    type: header
    header: Authorization
    env: SENTRY_AUTH_TOKEN
  annotations:
    isReadOnly: true
  policies:
    tools:
      include:
        - "*"
```

### Scoping MCP Resources

Use `policies.tools` to control which tools from the MCP server are exposed to agents:
- **`include: ["*"]`**: Expose all tools provided by the server.
- **`include: ["get_*", "list_*"]`**: Use wildcards to expose only safe inspection tools.
- **`exclude: [...]`**: Blacklist dangerous operations (e.g., delete actions).

Exclusions apply after inclusions, so a wildcard `exclude` still blocks a name that `include` lists. The same `policies` block accepts `prompts` and `resources`, filtering what the server offers for prompt rendering and resource reads.

A policy filter is a **hard block for the agent**, not a prompt it can talk its way past. A blocked tool is never registered, and calling it anyway — by name, through a stale session, or after a reload that tightened the filter — fails with an error naming the policy section and the manifest path. To call a blocked tool you must edit `spec.policies.tools`; there is no override for a single tool call, and the agent is never granted one on your behalf.

*You* can still reach a blocked prompt or resource when you decide it is worth it. Invoking a prohibited item — from the composer with `/mcp:<server>:<prompt>` or `@mcp:<server>:<uri>`, or from the [`/mcp` browser](../reference/cli.md#mcp-browser-mcp) — opens a confirmation naming the item, the policy section, and the manifest path, with three choices:

| Choice | What it does |
| :--- | :--- |
| **`Cancel`** *(default)* | Does nothing. `Enter` lands here, so an accidental keypress changes nothing. |
| **`Use once`** | Sends this one request with `allowProhibited`, leaving the manifest and every future turn untouched. |
| **`Enable in manifest`** | Adds the item to `spec.policies`, refreshes the server, then runs it as an allowed item — the durable choice. |

`Enable in manifest` appears only for a workspace- or project-scoped manifest with a known path; a user- or builtin-scope server offers `Use once` alone, because rewriting it would edit files outside your project. Tools are deliberately absent from this prompt: the override exists for reading content you asked for, not for handing the agent a destructive operation it was filtered out of mid-task. Widening a tool is a deliberate act instead — you read what it does and what its schema expects in the [tool detail view](../reference/cli.md#mcp-tool-drill-down) and press `e` there, which writes the same policy section for the same scopes. The same override is available programmatically by passing `allowProhibited: true` to the [MCP HTTP endpoints](../reference/cli.md#mcp-http-endpoints), which never widens the filter and is never set on the agent's behalf.

Prohibited items stay visible wherever they are counted — the overview card reports `2 prompts (1 allowed, 1 prohibited)` rather than hiding the gap, tool rows carry a red `✘` and a `[Prohibited]` badge, and completion menus list prohibited items labeled `[Prohibited]` — so a policy that hides something never hides the fact that it exists.

### Reading Server Resources

A server may publish resources alongside its tools: documentation, templates, an index of what it can do. The built-in [`read_mcp_resource`](../reference/tools.md#read_mcp_resource) tool reads one of them by URI:

```json
{ "server": "notes", "uri": "notes://specs/spec-1" }
```

It is activated automatically while any connected server exposes a resource your policies permit, and it costs nothing when none do, so there is nothing to declare. Text resources that exceed the output limits arrive as a head-plus-tail preview pointing at a file you can open with `view`.

To attach a resource to your own turn instead of leaving the choice to the agent, type `@mcp:` in the composer and pick one, or open the server in [`/mcp`](../reference/cli.md#mcp-server-overview) and press `u`. Both keep the resource out of the agent's hands until you decide it belongs in the conversation.

Resources are server content, not repository files — `view` and `grep` remain the tools for the workspace itself.

### Server Lifecycle

The daemon connects each declared server in the background when its workspace opens, so a slow or offline server never delays a session. Run [`/mcp`](../reference/cli.md#mcp-browser-mcp) to see where things stand — servers are grouped under their scope heading (`Workspace MCPs (.agents/mcps)`, `User MCPs (~/.config/tasksmith/agents/mcps)`, …) with a `✔`, `⚠`, `✘`, `⟳`, or `○` glyph and a plain-language status to the right — or ask the agent to inspect `GET /v1/workspace/mcps`:

| Status | Meaning |
| :--- | :--- |
| `connecting` | The connection is in progress. |
| `connected` | Tools, prompts, and resources are discovered and available. |
| `auth_required` | The server could not authenticate. With `authUrl` set, an interactive authorization flow is waiting; otherwise a static credential is missing or was rejected. |
| `error` | The last attempt failed; `error` explains why. |
| `disconnected` | No connection has been attempted or the workspace has no MCP runtime. |

Because connections happen in the background, a server that is still `connecting` when a session starts contributes no tools yet. When it finishes connecting, open sessions pick the server's tools up automatically and they become invocable on the next turn — no restart, no reload, and no need to mention them in the agent's `spec.tools`. The same refresh activates `read_mcp_resource` if the server publishes a permitted resource.

Reloading the workspace (`.agents/`, `.env`, or manifest changes) reconnects servers whose definition, environment, or render context changed, and re-publishes the tools of servers that stayed the same. A reload that **tightens** `spec.policies` also takes effect immediately: tools an in-flight session already holds stop being callable.

A server that advertises it can change its own tool, prompt, or resource lists triggers a re-sync automatically, so tools it adds or removes mid-session become available or stop being offered without a restart. Server-pushed change notifications need a live server-to-client stream: they arrive over `sse` and `stdio`, but `type: http` and `streamable-http` disable that stream per the transport specification, so those servers are only refreshed on reload or restart.

Discovered tools are named `<server>__<tool>` and are **deferred by default**, so they cost context only once the agent finds them through `tool_search`. List one in an agent's `spec.tools` to load it on the first request. See [Tool partitioning](../reference/tools.md#5-tool-partitioning-eager-vs-deferred).

What a discovered tool looks like to the model — its schema, description, and safety annotations — comes from the server, and those annotations decide whether calls need approval: a server or manifest declaring `isReadOnly: true` lets read-style tools run without prompting, while an unannotated tool always surfaces a prompt. Annotate servers you trust in the manifest, and use `overrides` to correct a single tool the server describes wrongly. See [MCP Tool Execution Model](../reference/tools.md#6-mcp-tool-execution-model).

Credentials for `auth.type: oauth2` servers are stored in `~/.local/share/tasksmith/mcp/credentials.json` (`$XDG_DATA_HOME/tasksmith/mcp/credentials.json`) with owner-only permissions, and are reused across daemon restarts.

To authorize a server stuck at `auth_required`, highlight it in `/mcp` and press `a` (or run `/mcp auth <name>`): TaskSmith opens an authorization screen that names the server, opens the authorization page in your browser, and keeps checking until the server reports `connected`, then leaves the outcome in the conversation timeline. The screen also prints the URL, so if no browser can be launched you can press `c` to copy it and open it yourself.

The screen covers the case where the browser cannot hand the login back to TaskSmith. TaskSmith listens for the redirect on a loopback port of its own, but a browser on another machine, a blocked `localhost`, or a redirect page that failed to load leaves the code stuck in the address bar — so the screen offers a field to paste that URL back, and `Enter` hands it to the waiting flow. The full address-bar URL, a bare query string, or the authorization code alone all work. Pressing `Esc` leaves the screen without cancelling the login: the daemon keeps its callback open on a timer, so finishing in the browser afterwards still connects the server. See [CLI Interface](../reference/cli.md#mcp-authentication) for the screen's layout and shortcuts.

Whatever the browser does, it is answered with a TaskSmith page rather than a wall of text: a successful login shows an `Authentication Complete` card naming the server, and a rejected one shows `Authentication Failed` with the reason the identity provider gave.

A server that authenticates with static credentials has no authorization page to open, so the same action tells you which credential to provide instead — the environment variable to set, the header to render, or the credential file to point at — and you run `/mcp restart <name>` once it is in place. Unresolved `${VAR}` placeholders in `spec.headers` or `spec.env` are reported the same way.

You do not have to go looking for broken servers. The first time the CLI sees a server settle into a broken state it writes a warning into the conversation timeline naming the server and the command to run — `MCP server "linear" requires authentication — run /mcp auth linear`, or `MCP server "search" failed to connect (connection refused) — run /mcp to inspect` — and again if the same server later breaks for a different reason. It is said once per problem, so refreshing the listing never repeats it.

That line lives in the transcript rather than the startup banner deliberately. The banner is printed once and frozen when the conversation starts, so a server that authorizes or fails minutes later would either go unnoticed or leave a warning on screen long after it stopped being true. A timeline line stays where it was written: scroll back to read it, and nothing stale is held in front of you. See [CLI Interface](cli.md#1-header-banner--workspace-warnings).

---

## Scaffolding Tools with the Customizations Skill

Ask the active agent to create a custom tool or MCP server. It can load `tasksmith-customizations`, inspect the tools and MCP reference, and write the appropriate manifest without switching agents. Describe the command, input schema, and intended side effects so safety annotations are accurate.

---

For complete YAML frontmatter specifications and manifest schemas, see the [Manifests Reference](../reference/manifests.md).
