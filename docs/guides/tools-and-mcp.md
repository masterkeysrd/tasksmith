# Give agents tools and connect MCP servers

Control which tools an agent sees, wrap a project executable as a tool, or
connect a service that publishes tools, prompts and resources over MCP.

## Inspect and toggle built-in tools (`/tools`)

Open `/tools`, select a tool and press `i` to inspect its schemas,
annotations and, for custom tools, rendered command. Start here if one
agent can use a tool but another can't.

| Status | What it means for the active agent |
| --- | --- |
| `[Eager]` | The definition is present from the first request. |
| `[Deferred]` | Registered; the agent discovers it with `tool_search` when needed. |
| `[Denied]` | An agent tool policy blocks it. |
| `[Inactive]` | This session doesn't activate the tool. |
| `[Disabled]` | Opted out in configuration; it never enters model context. |

`view`, `glob` and `grep` inspect the workspace. `write` and `edit` change
files; `bash` executes commands subject to shell inspection. `web_fetch`
and `web_search` access the network. Other built-ins handle questions,
checklists, skills and delegation. See the [tool catalog](../reference/tools.md)
for parameters, safety annotations and approval behavior.

Press `Space` to toggle availability. This is a manifest edit, not a hidden
runtime switch. For a workspace-owned tool TaskSmith can patch its own
`spec.enabled: false`. To disable a built-in or inherited user tool for just
this repository, the equivalent `WORKSPACE.md` frontmatter fragment is:

```yaml
spec:
  policies:
    tools:
      exclude: [web_search]
```

Re-enabling removes the applicable opt-out. Disabling `tool_search` also
removes deferred tools from the agent's prompt and tool list. To make a
permitted tool eager instead, list its name in the Agent's `spec.tools`.
Availability does not grant permission for every call.

## Create a custom tool (`*.tool.md`)

For a Git project, expose a narrow operation rather than a general shell.
This tool lists tracked files under one of two known directories:

```sh
mkdir -p .agents/tools
```

Save `.agents/tools/tracked-files.tool.md`:

```markdown
---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: tracked-files
  description: List files tracked by Git under a selected project directory.
spec:
  command: [git, ls-files, "--", "{{.directory}}"]
  inputSchema:
    type: object
    properties:
      directory:
        type: string
        enum: [docs, scripts]
        description: Project directory to list.
    required: [directory]
  outputSchema:
    type: string
  annotations:
    isReadOnly: true
    isDangerous: false
    isOpenWorld: false
    userHint: List tracked project files
---
Use to inventory tracked documentation or scripts without listing untracked files.
```

Try it in the composer: `Use tracked-files to list the docs directory.`
The tool receives `{"directory":"docs"}`. It runs inside the session
sandbox; discovery and validation alone don't make it safe to execute.

- **Arguments on the command line:** an explicit `{{…}}` that consumes an
  argument renders an argv element; stdin stays closed. Missing template
  values fail the call rather than reaching the executable as placeholders.
- **Arguments on stdin:** with `command: [python, scripts/format.py]` and
  no consumed argument, the script receives one JSON object on stdin.
  Shell-style `$var`, `${var}` or `$1` alone don't opt in to templating.
- **Long-running calls:** after the 60-second wait window the task continues
  in the background. Use `tasks` to inspect or kill it; completion wakes the
  agent. Stdin is fixed at launch, so it can't accept interactive input.

Describe real effects: `isReadOnly` for inspection, `isDangerous` for
potentially destructive operations, `isOpenWorld` for external access and
`userHint` for a human-readable action. Risk isn't inferred from argv. An
unannotated tool is not automatically safe. Project tool definitions are
trust-fingerprinted: after a change, inspect the diff before accepting a
new trust prompt. User tools belong under `~/.config/tasksmith/agents/tools/`.

## Connect an MCP server (`*.mcp.yaml`)

Place project or workspace definitions in `.agents/mcps/`, or personal
ones under `~/.config/tasksmith/agents/mcps/`. Choose the transport that
your server actually offers:

| Transport | What you supply | When discovery updates |
| --- | --- | --- |
| `stdio` | A local executable and argument array. | On connection and supported server change notifications. |
| `sse` | An SSE endpoint. | Also supports pushed tool, prompt and resource list changes. |
| `http` / `streamable-http` | A Streamable HTTP endpoint. | Reload or restart to refresh; no server-pushed list change stream. |

For example, if your team ships a local notes MCP executable, save this as
`.agents/mcps/notes.mcp.yaml`, replacing the command with its actual path:

```yaml
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: notes
  description: Read the team's engineering notes
spec:
  type: stdio
  command: [./scripts/notes-mcp]
  policies:
    tools:
      include: ["get_*", "list_*"]
    resources:
      include: ["notes://specs/*"]
```

For a remote server, replace `type` and `command` with its network settings;
for example, this fragment is for a server that advertises SSE:

```yaml
type: sse
endpoint: https://mcp.sentry.internal.acme.com/sse
auth:
  type: bearer
  token: "${env:SENTRY_AUTH_TOKEN}"
```

Server tools have canonical names like `notes__list_notes` (`<server>__<tool>`).
They're deferred by default. In the MCP's own filter, use the **server's**
name, such as `list_notes`; in the agent's eager list, use `notes__list_notes`.
Exclusions win over inclusions. A filter can also limit prompts and resources.
A tool's schema and description come from the server. Check its annotations:
server manifest defaults override remote annotations; use `overrides` to
correct one tool. Don't mark a whole server read-only if it can write.

### Check the connection and control its lifecycle

Open `/mcp`. Servers are grouped by scope; `connecting` is normal while
background discovery runs. `connected` means discovery succeeded. `error`
has failure details; `disconnected` means no connection was attempted or the
workspace has no MCP runtime. `auth_required` needs attention (see below).
Programmatic clients can inspect `GET /v1/workspace/mcps`.

Tools become available to open sessions on the next turn after connection.
Reload reconnects changed servers (including environment and render-context
changes) and republishes unchanged ones. A tightened filter blocks even
previously discovered tools immediately. Restart a single enabled server:

```text
/mcp restart notes
```

Press `e` or `Space` on the **server** to toggle it. A workspace manifest
can set `spec.enabled: false`; a repo-only opt-out for an inherited server
uses `WORKSPACE.md` `spec.policies.mcps.exclude`. A disabled server stays
listed, never starts and exposes no tools. Enable it before restart or auth.

## Authenticate OAuth MCP servers

For `auth.type: oauth2`, select the server in `/mcp` and press `a`, or run:

```text
/mcp auth notes
```

1. Check the server name on the authorization screen. Complete sign-in in
   the browser, or press `Ctrl+Y` to copy the URL to a browser you can access.
2. If the loopback redirect can't reach the daemon, paste the final URL,
   query string or authorization code into the screen and press `Enter`.
3. Look for `connected` and the outcome in the timeline. `Esc` dismisses
   the view without cancelling the pending, time-limited callback.

The browser reports **Authentication Complete** or **Authentication Failed**.
OAuth credentials persist with owner-only permissions under
`$XDG_DATA_HOME/tasksmith/mcp/credentials.json`. Don't edit that file.
For static auth there's no browser page: supply the missing environment
variable, header or credential file, then restart the server. Unresolved
placeholders in `spec.env` or `spec.headers` can cause the same warning.
Passive refresh updates `/warnings` and the status-line count without adding
transcript noise; explicit auth and restart actions still report outcomes.

## Use MCP prompts and resources

Invoke a published prompt with `/mcp:<server>:<prompt>`. To attach content
yourself, select `@mcp:<server>:<uri>` from composer completion or press `u`
on the server overview. For an agent-initiated read, `read_mcp_resource`
activates automatically when a connected server has a permitted resource:

```json
{ "server": "notes", "uri": "notes://specs/spec-1" }
```

Large text resources produce a head-and-tail preview with a file to open
using `view`. For repository files use `view` or `grep` directly instead.

Prohibited items remain visible in counts and completion lists. A blocked
tool is never registered for the agent. A stale call fails with the policy
section and manifest path; the agent cannot grant itself an exception.
When **you** request a blocked prompt or resource, choose deliberately:

| Choice | Effect |
| --- | --- |
| **Cancel** (default) | Don't read the item. |
| **Use once** | Authorize this request only (`allowProhibited: true` in the HTTP API). |
| **Enable in manifest** | Update a writable workspace/project policy, refresh, then read it. |

A user- or built-in-scope item has no durable enable option here. Tools have
no one-call override: inspect the tool detail and press `e` there to change
its writable manifest policy. For help scaffolding an integration, ask the
`tasksmith-customizations` skill and describe its intended side effects.

## Next steps

- [Check tool parameters and permission behavior](../reference/tools.md).
- [Look up custom Tool fields](../reference/manifests/tool.md).
- [Configure MCP transports, filters and authentication](../reference/manifests/mcp.md).
- [Review workspace trust before enabling executable resources](./trust.md).
