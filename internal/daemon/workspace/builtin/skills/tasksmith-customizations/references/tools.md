# Custom Tool Manifests

A `Tool` resource exposes an operation to an agent. Custom tools are command-backed: they run an executable inside the session sandbox. They register into the same container as built-in tools and go through the same permission pipeline, so their schema and annotations have to be authored deliberately.

Custom tool manifests live under `tools/<name>.tool.md` in a resource directory (project `.agents/tools/`, workspace `.agents/tools/`, or the user resource directory). A project manifest overrides a workspace or user manifest with the same name; an already registered built-in with the same name always wins over a custom tool.

## Anatomy

```markdown
---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: migration-status
  displayName: Migration Status
  description: Reports pending and applied database migrations for the current project.
spec:
  command:
    - python
    - "{{.Project.Dir}}/scripts/migrations.py"
    - --mode
    - "{{.Input.mode}}"
  env:
    NO_COLOR: "1"
  inputSchema:
    type: object
    properties:
      mode:
        type: string
        enum: [pending, applied, summary]
        description: Which migration set to report.
    required:
      - mode
  outputSchema:
    type: object
    properties:
      pending:
        type: array
        items:
          type: string
  annotations:
    isReadOnly: true
    isIdempotent: true
    isOpenWorld: false
    isDangerous: false
    userHint: "Inspect migrations: %s"
---

Use this tool before writing a migration so the naming sequence and the last
applied revision are known. Report pending revisions verbatim.
```

| `spec` field | Type | Meaning |
| --- | --- | --- |
| `command` | string[] | Executable followed by arguments. Each element is an independent argv entry, never a shell command line. Elements may contain command templates. |
| `env` | map | Environment values injected into the process. Values are used verbatim (not templated). The host daemon environment is never inherited. |
| `inputSchema` | object | JSON Schema for the arguments the model supplies. Calls are validated against it before execution. |
| `outputSchema` | object | JSON Schema advertised to the model. Metadata only: output is not parsed or validated against it. |
| `annotations` | object | Safety and approval metadata; see below. |

The Markdown body becomes the tool's instructions. If the body is empty, `metadata.description` is used as the description; otherwise a description is generated and the rendered body is attached as instructions. Keep the body focused on *when* to use the tool and what to do with the result — the schema already describes the parameters.

## Command Templating

A command is rendered per call only when the manifest authors an explicit `{{ … }}` action in a command element.

| Reference | Value |
| --- | --- |
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

Rules that matter when authoring:

- **Templates are opt-in.** A command containing only shell-style references (`$var`, `${var}`, `$1`) is executed verbatim, so a conventional CLI script keeps its own positional parameters and environment instead of having them rewritten as templates.
- **Prefer absolute paths from templates** (`{{.Project.Dir}}/scripts/…`) over relative paths, so the tool works regardless of the process working directory.
- **A missing argument fails the call.** An absent template value would render as a `<no value>` placeholder; that is surfaced as an argument validation error the model can correct and retry, rather than executing with a bogus literal. Declare arguments `required` and give sensible `default`s in the schema.
- **An empty executable is rejected.** If rendering blanks the first command element, the call fails.
- **Instruction templates render once per session**, so they may reference the workspace, project, and agent but not per-call arguments. A body that interpolates arguments is left raw, and a template failure degrades to the raw text rather than dropping the tool.

## Argument Delivery

Arguments reach the process in exactly one of two ways, never both:

| Manifest authoring | Delivery |
| --- | --- |
| A command element interpolates a supplied argument (`{{.Input.<field>}}` or the `{{.<field>}}` shorthand) | Rendered argv only. Standard input is left closed. |
| No command element consumes an argument | The validated arguments are marshalled as one JSON object on standard input. |

So a script that does not interpolate arguments reads its parameters from stdin:

```markdown
spec:
  command: [python, scripts/format.py]
  inputSchema:
    type: object
    properties:
      path:
        type: string
      width:
        type: integer
    required:
      - path
```

```python
import json, sys

args = json.load(sys.stdin)
```

An absent or unparseable `inputSchema` falls back to a generic object schema, and arguments are still delivered on stdin.

## Input And Output Schemas

`inputSchema` is enforced; `outputSchema` is advisory.

- Write `inputSchema` as a JSON Schema `object` with `properties`, `required`, per-property `description`, `enum` for closed sets, and `default` for optional values. The model sees this schema verbatim, so descriptions are the main lever for correct calls.
- Keep argument counts and nesting small. Deeply nested objects are harder for the model to fill correctly and awkward to interpolate into a command line.
- Use `outputSchema` to describe the shape the script prints so the model can rely on it. It is never used to parse or validate output, and an absent or unparseable schema defaults to a string schema.

## Safety Annotations

All boolean annotations default to `false`, and the plan for a custom tool is derived **only** from them — risk is never inferred from argument values, argument key names, or the command string.

| Field | Type | Meaning |
| --- | --- | --- |
| `isReadOnly` | bool | The tool does not modify state. |
| `isIdempotent` | bool | Safe to repeat; this does not enable automatic retries. |
| `isOpenWorld` | bool | The tool touches external resources, such as a network. |
| `isDangerous` | bool | The tool can perform destructive actions. |
| `userHint` | string | Approval summary; `%s` is replaced with the rendered command line. |

The resulting approval plan:

| Annotation state | Action | Safety | Read-only |
| --- | --- | --- | --- |
| `isDangerous: true` | `exec` | `unsafe` | `false` |
| `isReadOnly: true`, not open-world | `read` | `safe` | `true` |
| `isReadOnly: true` and `isOpenWorld: true` | `read` | `unknown` | `true` |
| `isOpenWorld: true` only | `net` | `unknown` | `false` |
| Nothing set | `exec` | `unknown` | `false` |

An unannotated tool is therefore never treated as auto-allowed safe. Annotations must describe real behavior; they do not replace sandboxing or permission checks, and only read-only non-open-world tools are usable in read-only exploration modes.

## Execution Results

- Standard output is returned to the model as text. When it exceeds the output limits, the full content is spilled to a file and the model receives a head-plus-tail preview pointing at that file.
- A nonzero exit code reports standard error, then standard output if standard error is empty, then the exit code alone. Write diagnostics to standard error so the model gets something actionable.
- Execution runs inside the sandbox and is subject to the permission system. The process environment is the sandbox configuration plus `spec.env`; the host daemon environment is never inherited.

## Eager, Deferred, Denied, Or Inactive

Whether the model can see a tool is decided per session from the agent's tool selection and tool policy, not by the manifest:

| Status | Meaning |
| --- | --- |
| `eager` | Named in the agent's `spec.tools`; present in the initial model context. |
| `deferred` | Registered but withheld from the initial context; activated by a `tool_search` match on a previous turn. |
| `denied` | Blocked by workspace or agent tool policy. |
| `inactive` | Neither loaded, deferred, nor denied (for example, a conditional built-in the session does not activate). |

Custom tools are deferred by default. Listing the tool's name in the agent's `spec.tools` makes it eager; leaving it deferred keeps its schema out of the context until the agent demonstrates it is needed, which is the right default for niche tools. Run `/tools` in the CLI to inspect the resolved status, scope, annotations, and schemas per agent, and to hot-reload manifests edited on disk.

## MCP Tools

`MCP` resources describe external servers and are covered in [tools-and-mcp.md](tools-and-mcp.md). Tools exposed by an MCP server follow the same eager/deferred partitioning as custom tools and are deferred by default.
