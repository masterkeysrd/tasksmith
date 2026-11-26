# Workspace Manifest (`WORKSPACE.md`)

Anchors a repository as a TaskSmith workspace. Placed at `<workspace-root>/WORKSPACE.md`.

## Example

This workspace registers three projects and imports selected agents and skills from `acme`. The `dangerous: deny` setting applies to tools marked dangerous. The separate `exclude` entry illustrates a pitfall: `bash(rm -rf /*)` is a permission pattern, not a tool-name glob that blocks that shell command.

```markdown
---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: my-app
spec:
  projects:
    - .
    - packages/backend
    - packages/frontend
  defaultProvider: openai
  defaultModels:
    completion: openai/gpt-5.5
    decision: typesafe/jev-latest
    embedding: openai/text-embedding-3-small
  modelAliases:
    fast: openai/gpt-5.5
    default: openai/gpt-5.5
  defaultAgent: main
  plugins:
    - source: github.com/acme/shared
      version: v1.2.0
      namespace: acme
      imports:
        include:
          - "agents/*"
          - "skills/*"
        exclude:
          - "agents/experimental/*"
  policies:
    tools:
      include:
        - "*"
      exclude:
        - "bash(rm -rf /*)"
      dangerous: deny
      openWorld: allow
---

# Workspace Guidelines

Always run tests before completing code generation tasks.
Ensure error handling wraps sentinels using `%w`.
```

## Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `projects` | array of strings | Relative paths to registered subprojects or packages. |
| `defaultProvider` | string | Default model provider name (e.g. `openai`, `ollama`). |
| `defaultModels` | map[string]string | Role-based model assignments: `completion`, `decision`, `embedding`. |
| `modelAliases` | map[string]string | Named model shorthands resolved to a canonical `<provider>/<model-id>`; referenced as `@<alias>`, `@default`, or `<provider>/@<alias>`. Shadows per-model `aliases`. The F2 alias editor leaves these entries untouched, and preset installs defer to existing entries. |
| `defaultAgent` | string | Persona activated for new sessions (defaults to `main`). |
| `plugins` | array of objects | External plugin resources to load: `source`, `version`, `namespace`, `imports.include`, `imports.exclude`. |
| `policies.tools.include` | array of strings | Tool grant glob patterns allowed in this workspace. |
| `policies.tools.exclude` | array of strings | Tool denial glob patterns; matching tools are unavailable in this workspace (never registered, listed `[Disabled]` in `/tools`, toggleable there). |
| `policies.tools.dangerous` | string | Policy for `isDangerous` tools: `allow`, `ask`, or `deny` (default `ask`). |
| `policies.tools.openWorld` | string | Policy for `isOpenWorld` tools: `allow`, `ask`, or `deny` (default `allow`). |
| `policies.tools.readOnly` | boolean | When `true`, only `isReadOnly` tools are allowed. |
| `policies.tools.ask` | array of strings | Tool glob patterns that require approval. |
| `policies.mcps.exclude` | array of strings | MCP server glob patterns; matching servers never connect, expose no tools, and list `[Disabled]` in `/mcp` (toggleable there). |

*The Markdown body below the frontmatter serves as workspace-level system instructions inherited by all agents.*

---

## Project Context Manifest (`AGENTS.md`)

Configures subproject-specific instructions in monorepos. Placed at `<subproject>/AGENTS.md`.

### Context example

```markdown
---
apiVersion: warp/v1alpha1
kind: Context
metadata:
  name: backend
  displayName: Backend Microservice
  description: Go gRPC microservice conventions
---

# Backend Conventions

- All network endpoints must pass through the gRPC interceptor chain.
- Database access is handled exclusively through pgx pools.
```

### Context fields

`AGENTS.md` needs no YAML `spec` block. It may be plain Markdown or use the [common metadata envelope](../manifests.md#common-metadata-envelope).

| Field | Type | Source |
| --- | --- | --- |
| `apiVersion`, `kind`, `metadata.name` | strings | Explicit frontmatter, or inferred when frontmatter is absent (`warp/v1alpha1`, `Context`, project identity). |
| `metadata.displayName`, `metadata.description`, `metadata.labels` | strings; string map for labels | Optional title, summary and categorization. |
| `spec.instructions` | string | Populated automatically from the Markdown body below the closing `---` (or the entire plain Markdown file). It supplies project-specific instructions to the agent template. |

## Discovery and defaults

| Setting | Resolution |
| --- | --- |
| Projects | An empty list means `["."]`; `["*"]` alone selects non-hidden immediate subdirectories. Explicit paths must be existing directories. |
| Model role | Explicit selection, then workspace role default, then default provider's role default, then effective providers' defaults or a matching model capability. |
| `plugins[]` | See [plugin imports and lock verification](./plugin.md) for the entry fields, defaults and matching rules. |
| `policies.mcps.include` | WARP string-array inclusion filter; TaskSmith's workspace MCP toggle edits `exclude`. |
| Tool policies | Match tool names, not shell command lines. The example's `bash(rm -rf /*)` is a permission-pattern spelling, not a shell restriction enforced by `policies.tools.exclude`. Use `permissions.json` for command-specific rules. |
| Context | The active project supplies context, not every ancestor's `AGENTS.md`. A custom agent can include `{{.Context.Instructions}}` in its persona. |

### WARP sandbox fields

These are WARP schema settings, not replacements for TaskSmith's daemon-enforced sandbox configuration. Fields below `policies.sandbox` describe ceilings, not grants.

| Field | Type | Meaning |
| --- | --- | --- |
| `defaultSandbox` | string | Sandbox reference for agents, tools and stdio servers without a selection. |
| `policies.sandbox.required` | boolean | Require a sandbox for command tools and stdio servers. |
| `policies.sandbox.drivers.include`, `.exclude` | string arrays | Filter driver names. |
| `policies.sandbox.hostPaths` | string array | Allowed host path globs; defaults to `["$WorkspaceDir/**"]`. |
| `policies.sandbox.hostWrite` | string | `allow` (default) or `deny` for writable host mounts. |
| `policies.sandbox.rootfs.mode` | string | Root filesystem: `rw`, `ro`, or `none`. |
| `policies.sandbox.access.read`, `.write`, `.deny` | string arrays | Path filters; denied paths cannot be read or written. |
| `policies.sandbox.exec.mode`, `.allow` | string; string array | `none`, `allowlist`, or `any`; command names or paths for the allowlist. |
| `policies.sandbox.network.mode`, `.allow`, `.deny` | string; string arrays | `none`, `allowlist`, or `open`; host globs for exceptions and blocks. |
| `policies.sandbox.env.passthrough` | string array | Host variable name globs; only passthrough is valid in the ceiling. |
| `policies.sandbox.limits.cpu`, `.memory`, `.timeout`, `.processes` | strings; integer for `processes` | CPU quota, memory cap, maximum lifetime and concurrent processes. |

## See Also

- [Workspaces](../../guides/workspaces.md)
- [Manifest hierarchy and metadata](../manifests.md)
