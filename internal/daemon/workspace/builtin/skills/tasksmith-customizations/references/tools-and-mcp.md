# Tools and MCP

Tools expose operations to an agent. TaskSmith has built-in tools, can register command-backed Tool resources, and connects to the external servers described by MCP resources, exposing each discovered server tool as `<server>__<tool>`.

## Custom tools

Tool manifests are discovered in the usual resource scopes, commonly under `.agents/tools/`. TaskSmith registers effective tools with a nonempty `command` when permitted by the agent's tool selection. An already registered built-in with the same name takes precedence over a custom tool.

| `spec` field | Type | Meaning |
| --- | --- | --- |
| `command` | string[] | Executable and static arguments; required for a command-backed tool. |
| `env` | map | Environment values supplied to the process. |
| `inputSchema` | object | JSON Schema for the arguments supplied by the model. |
| `outputSchema` | object | JSON Schema describing the output in the tool definition. |
| `annotations` | object | Safety and approval metadata described below. |

For a Markdown Tool, the body becomes its instructions and is used as the description presented to the model. If it is empty, the metadata description is used; otherwise a generic description is generated.

A command is an executable followed by arguments, not a shell command line. TaskSmith validates call arguments against the input schema, then delivers them in exactly one of two ways: as rendered command-line arguments when the manifest interpolates them with `{{ … }}` templates, or as the complete JSON object on standard input when no command element consumes an argument. Arguments are never sent both ways. A missing or unparseable input schema falls back to a generic object schema; an unresolvable schema also falls back for validation.

Standard output is returned as text. A nonzero exit reports standard error, standard output if standard error is empty, or the exit code if neither contains an error. The output schema is metadata, not output parsing or validation; an absent or unparseable schema defaults to a string schema. Execution is subject to the sandbox and permission system.

For command templating references (`{{.Input.<field>}}`, `{{.Workspace.Dir}}`, `{{.Project.Dir}}`, `{{.Agent.Name}}`), schema authoring, and the eager/deferred partitioning rules, see [Custom tool manifests](tools.md).

### Safety annotations

All boolean annotations default to `false`.

| Field | Type | Meaning |
| --- | --- | --- |
| `isOpenWorld` | bool | The tool interacts with external resources, such as a network. |
| `isDangerous` | bool | The tool can perform destructive or otherwise dangerous actions. |
| `isReadOnly` | bool | The tool does not modify state. |
| `isIdempotent` | bool | The operation is safe to repeat; this does not enable automatic retries. |
| `userHint` | string | Approval summary; `%s` is replaced with the static command and arguments. |

For custom tools, read-only tools are classified as safe only when neither open-world nor dangerous. A non-read-only open-world tool is classified as a network action unless dangerous; dangerous tools are unsafe and not treated as read-only. Without these annotations, safety is unknown. Annotations must reflect actual behavior and do not replace sandboxing or permission checks.

```markdown
---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: project-check
  description: Run project checks
spec:
  command: [go, test, ./...]
  inputSchema:
    type: object
    properties: {}
  annotations:
    isReadOnly: false
    isDangerous: false
    isOpenWorld: false
---
Run the project checks and report failures.
```

## Deferred tools and `tool_search`

Custom and MCP tools are not listed in an agent's `spec.tools` by default, so they load as **deferred** tools (`DeferLoading: true`): they are registered in the session's tool container but withheld from the initial model context. When a session resolves at least one deferred tool, TaskSmith automatically activates the built-in `tool_search` tool as an eager tool. `tool_search` runs a BM25 keyword search over the deferred tools' names, descriptions, and parameters and returns the matching tool names, which then become available for direct invocation on subsequent turns. When a session has no deferred tools, `tool_search` is not registered.

To make a custom or MCP tool eager instead of deferred, list its name in the agent's `spec.tools`. Run `/tools` in the CLI to see each tool's resolved status for the active agent (`[Eager]`, `[Deferred]`, `[Denied]`, `[Inactive]`) and to hot-reload manifests edited on disk.

Three built-ins are exceptions to "eager only if listed": `tool_search`, `load_rule`, and `read_mcp_resource` activate themselves when the workspace gives them something to do (a deferred tool, a conditional rule, a connected server with a permitted resource). `policies.tools` still governs them, so denying one removes it.

## MCP definitions

An `MCP` resource describes a Model Context Protocol server. The daemon connects each declared server in the background when its workspace opens, so a slow server never delays session start, and open sessions pick up a server's tools automatically once its connection completes.

The filters and annotation overrides below are **enforced**. A tool excluded by `policies.tools` is never registered and reports status `Denied`; a prohibited prompt or resource read is rejected naming the policy section and the manifest path. The check reads the live catalog on every call, so tightening a filter also blocks a tool an in-flight session already holds. Only a user acting interactively may authorize a single prohibited prompt or resource read; nothing on the agent's tool path can.

| `spec` field | Type | Meaning in the WARP definition |
| --- | --- | --- |
| `type` | string | Transport: `stdio` (default), `sse`, `http`, or `streamable-http`. |
| `command` | string[] | Executable and arguments for a `stdio` server. |
| `endpoint` | string | Server URL for an HTTP-based transport. |
| `env` | map | Environment variables for the server process. |
| `auth` | object | Authentication for a remote server; see [Remote authentication](#remote-authentication). |
| `headers` | map | Custom HTTP request headers for a remote server. |
| `annotations` | object | Default safety annotations for exposed tools, using the fields above. |
| `policies` | object | Filters for the server's `tools`, `prompts`, and `resources`. |
| `overrides` | map | Tool-specific annotation overrides keyed by tool name. |

Each policy filter has `include` and `exclude` arrays of glob patterns. The WARP definition applies exclusions after inclusions. Use environment- or file-based authentication rather than embedding credentials in manifests.

Manifests live under `mcps/<name>.mcp.yaml` in a resource directory: a project's `.agents/mcps/`, a workspace's `.agents/mcps/`, or the user resource directory `~/.config/tasksmith/agents/mcps/`. A project manifest overrides a workspace or user manifest with the same name. `metadata.name` both prefixes every exposed tool as `<server>__<tool>` and names the server in user-facing MCP commands.

```yaml
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: local-server
spec:
  type: stdio
  command: [example-server]
  policies:
    tools:
      include: ["*"]
      exclude: ["delete-*"]
```

The `sse`, `http`, and `streamable-http` types all use the same HTTP client transport; only `sse` keeps a server-to-client stream open for notifications. Hosted endpoints that speak Streamable HTTP, such as GitHub's, use `streamable-http`.

### Remote authentication

For an HTTP-based transport, `auth` selects one scheme. A static scheme renders one header on every request; an interactive scheme delegates to the OAuth handler and renders no header. Custom `spec.headers` are applied first, so authentication never overwrites a header you configured explicitly. Header values and `auth.file` paths expand `$VAR` and `${VAR}` from the workspace environment.

| `auth.type` | Header rendered | Credential source |
| --- | --- | --- |
| `bearer` or empty | `Authorization: Bearer <token>` | `env` or `file` |
| `api-key` | `<header>: <token>`, no scheme prefix | `env` or `file` |
| `basic` | `Authorization: Basic base64(<token>)`, where the token is `user:password` | `env` or `file` |
| `oauth2` | none; authorization code with PKCE | optional pre-registered client: `env` holds the client ID, `file` the client secret; otherwise the client is registered dynamically |
| `client-credentials` | none; client credentials grant | `env` holds the client ID, `file` the client secret |
| `enterprise` | none; organization-specific interactive flow | as for `oauth2` |

`auth.header` renames the header a static scheme writes and defaults to `Authorization`. When both `env` and `file` are set, `env` wins with no fallback to the file; a missing or empty variable, or an unreadable or empty file, is a connection error naming the source. An `auth` block with a static type but no source sends no credential header.

Bearer token from an environment variable:

```yaml
spec:
  type: streamable-http
  endpoint: https://mcp.example.com/mcp
  auth:
    type: bearer
    env: EXAMPLE_MCP_TOKEN
```

API key in a named header:

```yaml
spec:
  type: http
  endpoint: https://mcp.example.com/mcp
  auth:
    type: api-key
    header: X-Api-Key
    file: ~/.config/tasksmith/example-mcp.key
```

Interactive authorization, with no credential in the manifest:

```yaml
spec:
  type: streamable-http
  endpoint: https://mcp.example.com/mcp
  auth:
    type: oauth2
```

Interactive tokens are persisted by the daemon under `$XDG_DATA_HOME/tasksmith/mcp/credentials.json` (normally `~/.local/share/tasksmith/mcp/credentials.json`) with owner-only permissions, and survive daemon restarts.

### Verifying a server

Workspace loading does not validate manifests, so an unrecognized transport or a missing endpoint produces a loader warning rather than an error, and a server that needs credentials connects only once one resolves. Connection status is user-facing: the user browses servers and their status, authorizes an interactive server, and restarts one after a manifest edit from the CLI. None of that is available to an agent, so after writing or editing a manifest, report the path and the credential source you referenced and ask the user to confirm the server's status rather than claiming it connected.
