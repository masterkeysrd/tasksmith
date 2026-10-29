# Tools, toolkits, and MCP

Tools expose operations to an agent. TaskSmith has built-in tools and can register command-backed Tool resources. Toolkits group tool references or inline tool definitions. MCP resources describe external servers, but their discovery does not currently connect those servers in TaskSmith.

## Custom tools

Tool manifests are discovered in the usual resource scopes, commonly under `.agents/tools/`. TaskSmith registers effective tools with a nonempty `command` when permitted by the agent's tool selection. An already registered built-in with the same name takes precedence over a custom tool.

| `spec` field | Type | Meaning |
| --- | --- | --- |
| `command` | string[] | Executable and static arguments; required for a command-backed tool. |
| `env` | map | Environment values supplied to the process. |
| `inputSchema` | object | JSON Schema for the arguments supplied by the model. |
| `outputSchema` | object | JSON Schema describing the output in the tool definition. |
| `annotations` | object | Safety and approval metadata described below. |
| `description` | string | Description for YAML or inline definitions when metadata has no description. |
| `name` | string | Tool name for an inline toolkit entry; standalone tools use `metadata.name`. |

For a Markdown Tool, the body becomes its instructions and is used as the description presented to the model. If it is empty, the metadata description is used, with `spec.description` as a fallback; otherwise a generic description is generated.

A command is an executable followed by static arguments, not a shell command line. TaskSmith validates call arguments against the input schema and sends the JSON object to the process on standard input. Arguments are **not** automatically appended to the command line: the executable must read JSON from standard input if it needs them. A missing or unparseable input schema falls back to a generic object schema; an unresolvable schema also falls back for validation.

Standard output is returned as text. A nonzero exit reports standard error, standard output if standard error is empty, or the exit code if neither contains an error. The output schema is metadata, not output parsing or validation; an absent or unparseable schema defaults to a string schema. Execution is subject to the sandbox and permission system.

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

## Toolkits

A `Toolkit` groups tools in its `spec.tools` array. Each entry is either a `$ref` to an existing Tool resource or an inline Tool definition with a `name`, `command`, and optional Tool fields such as `description`, schemas, `env`, and `annotations`. Inline tools inherit the toolkit's namespace and directory during WARP expansion. A reference groups an existing tool rather than creating another implementation; WARP expansion skips references that cannot be resolved to a Tool.

TaskSmith exposes standalone tools and inline toolkit tools through its workspace tool list. A standalone tool shadows an inline tool with the same name. Inline command-backed tools are subject to the same registration, execution, and permission checks as standalone tools.

The following example references the `project-check` Tool above, assuming both resources are in the `workspace` namespace, and adds an inline tool with no dynamic arguments:

```yaml
apiVersion: warp/v1alpha1
kind: Toolkit
metadata:
  name: project-tools
spec:
  tools:
    - $ref: workspace/Tool/project-check
    - name: go-version
      description: Show the installed Go version.
      command: [go, version]
      inputSchema:
        type: object
        properties: {}
      annotations:
        isReadOnly: true
```

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
