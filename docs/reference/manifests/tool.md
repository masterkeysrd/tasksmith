# Tool Manifest (`*.tool.md`)

Defines a custom CLI script or executable wrapper as a callable agent tool. Placed under `.agents/tools/<name>.tool.md`.

Tool manifests are discovered like any other resource, so a project manifest can override a workspace or user manifest with the same name. An already registered built-in tool with the same name always wins over a custom tool.

## Example

The final argv entry consumes `path`, so the linter receives the rendered path as one argument and no JSON on stdin. `outputSchema` describes the expected diagnostics; it does not validate them.

```markdown
---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: linter
  displayName: Project Linter
  description: Executes golangci-lint on the codebase and returns structured JSON diagnostics
spec:
  command:
    - golangci-lint
    - run
    - --out-format=json
    - "{{.Workspace.Dir}}/{{.Input.path}}"
  env:
    NO_COLOR: "1"
  annotations:
    isReadOnly: true
    isIdempotent: true
    isDangerous: false
    isOpenWorld: false
    userHint: "Run linter: %s"
  inputSchema:
    type: object
    properties:
      path:
        type: string
        description: "Subdirectory path to lint."
    required:
      - path
  outputSchema:
    type: object
    properties:
      issues:
        type: array
        items:
          type: object
---

Executes project linting rules and returns diagnostic reports.
```

## Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `enabled` | boolean | Set `false` to opt the tool out everywhere its manifest loads; it lists `[Disabled]` in `/tools` and can be re-enabled from there (which removes the flag). Omitted means enabled. |
| `command` | array of strings | Executable binary and argument list. Each element is an independent argv entry — never a shell command line. Elements may contain command templates (see below). |
| `env` | map[string]string | Environment variables injected into the process. Values are used verbatim (not templated). The host daemon environment is never inherited. |
| `inputSchema` | object | JSON Schema describing the tool's input arguments. Call arguments are validated against it before execution. |
| `outputSchema` | object | JSON Schema describing the tool's structured output. Metadata only: it is advertised to the model and is not used to parse or validate output. |
| `annotations.isReadOnly` | boolean | Set `true` if tool execution does not mutate state. |
| `annotations.isIdempotent` | boolean | Set `true` if repeated runs produce identical outputs. Does not enable automatic retries. |
| `annotations.isDangerous` | boolean | Set `true` if execution can modify system configuration. |
| `annotations.isOpenWorld` | boolean | Set `true` if tool interacts with unconstrained network/shell. |
| `annotations.userHint` | string | Display pattern for interactive approval prompts. `%s` is replaced with the rendered command line. |
| `sandbox` | string | WARP sandbox reference, or `none`. This schema field does not override TaskSmith daemon isolation. |

*The Markdown body below the frontmatter becomes the tool's instructions. If it is empty, `metadata.description` is used as the description; otherwise a description is generated and the rendered body is attached as instructions.*

## Command Templating

A tool whose `spec.command` contains an explicit `{{ … }}` action is rendered per call against the workspace, project, invoking agent, and call arguments. The instruction body is rendered the same way, once per session. `spec.env` values are not templated.

| Reference | Value |
| :--- | :--- |
| `{{.Input.<field>}}` | Call argument `<field>` from `spec.inputSchema`. |
| `{{.<field>}}` | Shorthand for the same call argument. |
| `{{.Workspace.Dir}}` | Workspace root directory. |
| `{{.Workspace.Path}}` | Path of the workspace `WORKSPACE.md` file. |
| `{{.WorkspaceDir}}` | Shorthand for `{{.Workspace.Dir}}`. |
| `{{.Project.Name}}` | Current project name. |
| `{{.Project.DisplayName}}` | Project display name, falling back to its name. |
| `{{.Project.Dir}}` | Absolute path of the current project. |
| `{{.ProjectDir}}` | Shorthand for `{{.Project.Dir}}`. |
| `{{.Agent.Name}}` | Name of the agent invoking the tool. |
| `{{.Agent.Description}}` | Description of the invoking agent. |
| `{{.Agent.Dir}}` | Directory containing the invoking agent manifest. |

Behavioral rules:

- **Templates are opt-in.** Only an explicit `{{ … }}` action marks a command as templated. Shell-style references (`$var`, `${var}`, `$1`) in a command with no `{{ … }}` action are passed through untouched, so a conventional CLI script keeps its own positional parameters and environment.
- **A missing argument is an error, not a literal.** WARP renders an absent template value as a `<no value>` placeholder; TaskSmith rejects that call as an argument validation error the model can correct and retry, rather than handing the executable a bogus literal.
- **An empty executable is rejected.** If rendering blanks out the first command element, the call fails instead of attempting to execute an empty program.
- **Instruction templates render once per session**, so they may reference the workspace, project, and agent, but not per-call arguments. An instruction body that interpolates arguments is left raw, and a template failure degrades to the raw text rather than dropping the tool.

## Argument Delivery

Arguments reach the process in exactly one of two ways, never both:

| Manifest authoring | Delivery |
| :--- | :--- |
| At least one command element consumes an argument (`{{.Input.<field>}}` or the `{{.<field>}}` shorthand for a supplied key) | Rendered argv only. Standard input is left closed. |
| No command element consumes an argument | The validated arguments are marshalled as a single JSON object on standard input. |

An absent or unparseable `inputSchema` falls back to a generic object schema, and arguments are still delivered on standard input.

## Execution Model

- Standard output is returned to the model as text. When it exceeds the output limits, the full content is spilled to a file and the model receives a head-plus-tail preview pointing at that file.
- A nonzero exit code reports standard error, then standard output if standard error is empty, then the exit code alone.
- Execution runs inside the sandbox and is subject to the permission system. The process environment is built from the sandbox configuration plus `spec.env`; the host daemon's environment is never inherited.

## Status: Eager, Deferred, Denied, or Inactive

Whether a tool is visible to the model is decided per session from the agent's tool selection and tool policy, not by the manifest:

| Status | Meaning |
| :--- | :--- |
| `eager` | Listed in the agent's `spec.tools`; present in the initial model context. |
| `deferred` | Registered in the session but withheld from the initial context; activated by a `tool_search` match on a previous turn. |
| `denied` | Blocked by workspace, agent, or MCP server tool policy. |
| `inactive` | Neither loaded, deferred, nor denied (for example, a conditional built-in that the current session does not activate). |

Custom and MCP tools are deferred by default. MCP tools stay deferred even under `spec.tools: ["*"]`; to load one eagerly, name it outright as `<server>__<tool>`. Inspect the resolved status of every tool in a session with the [`/tools` browser](../cli/slash-commands.md#tools-browser-tools).

## See Also

- [Tools and MCP](../../guides/tools-and-mcp.md)
- [Manifest hierarchy and metadata](../manifests.md)
