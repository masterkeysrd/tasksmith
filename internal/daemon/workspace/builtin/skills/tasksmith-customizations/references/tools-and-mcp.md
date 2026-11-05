# Tools and MCP

Tools expose operations to an agent. TaskSmith has built-in tools and can register command-backed Tool resources. MCP resources describe external servers, but their discovery does not currently connect those servers in TaskSmith.

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

## MCP definitions

An `MCP` resource describes a Model Context Protocol server. TaskSmith discovers these definitions as workspace metadata, but **does not currently connect to the servers or load their tools, prompts, or resources**. The filters and annotation overrides below therefore do not enforce access restrictions in TaskSmith today.

| `spec` field | Type | Meaning in the WARP definition |
| --- | --- | --- |
| `type` | string | Transport: `stdio` (default), `sse`, `http`, or `streamable-http`. |
| `command` | string[] | Executable and arguments for a `stdio` server. |
| `endpoint` | string | Server URL for an HTTP-based transport. |
| `env` | map | Environment variables for the server process. |
| `auth` | object | Remote authentication: `type`, `header`, `env`, and `file`, as in [provider authentication](providers.md). |
| `headers` | map | Custom HTTP request headers for a remote server. |
| `annotations` | object | Default safety annotations for exposed tools, using the fields above. |
| `policies` | object | Filters for the server's `tools`, `prompts`, and `resources`. |
| `overrides` | map | Tool-specific annotation overrides keyed by tool name. |

Each policy filter has `include` and `exclude` arrays of glob patterns. The WARP definition applies exclusions after inclusions. Use environment- or file-based authentication rather than embedding credentials in manifests.

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
