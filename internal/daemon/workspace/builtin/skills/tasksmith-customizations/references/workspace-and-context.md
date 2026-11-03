# Workspace and project context

A Workspace defines the session's root, projects, defaults, policies, and shared guidance. A Context supplies identity and guidance for a particular project. These files have special discovery behavior and are not ordinary resources found by scanning `.agents/`.

## Workspace discovery

A session has one workspace root. Starting from the current directory, WARP searches upward for a valid `WORKSPACE.md` manifest; invalid or non-Workspace files are skipped. The loader looks for that exact filename, subject to filesystem case sensitivity. It does not use `.git` or `go.mod` as workspace anchors. If no valid manifest is found, it creates a synthetic workspace rooted at the starting directory.

`spec.projects` maps active project directories relative to the root. An omitted or empty list defaults to `["."]`; `["*"]` alone discovers non-hidden immediate subdirectories. Explicit paths must exist and be directories. Each mapped project has its own context and local resource namespace.

For multi-project workspaces, root `.agents/` resources are workspace-wide and each project's `.agents/` resources are project-local. If the only project is `.`, the root `.agents/` belongs to that project and is not also loaded as workspace-global. TaskSmith can additionally recognize a nested current directory with its own `.agents/` as a project.

## Workspace definition

A Workspace manifest has WARP YAML frontmatter and an optional Markdown body. Its rendered body supplies workspace-wide instructions in the session prompt. All `spec` fields below are optional.

| Field | Type | Meaning |
| --- | --- | --- |
| `projects` | string[] | Project paths or the standalone `"*"` discovery selector; defaults to `["."]` when empty. |
| `defaultAgent` | string | Default Agent when none is selected. |
| `defaultProvider` | string | Provider reference, resolved by resource name or provider type. |
| `defaultModels` | object | Role-specific model defaults for `completion`, `decision`, and `embedding`. |
| `modelAliases` | object | Named model shorthands (`@<alias>`, `@default`, or `<provider>/@<alias>`) resolved to a canonical `<provider>/<model-id>`. |
| `plugins` | object[] | External plugin resources to load into the workspace. |
| `policies` | object | Workspace-wide tool execution restrictions. |

### Model defaults

`defaultModels.completion` selects a text-generation model, `decision` selects a decision model, and `embedding` selects a vector-embedding model. TaskSmith resolves a role in this order:

1. Explicit model identifiers supplied by the caller, such as an agent's model preferences.
2. The workspace's role default.
3. The default provider's role default.
4. If none of those yields a result, TaskSmith checks effective providers for their defaults and then for a model with a matching capability.

An unqualified explicit or workspace-default model is prefixed with `defaultProvider` when that setting is a short provider name. A provider's own unqualified default is prefixed with its resource name. A selected identifier is configuration, not a guarantee that the model is available. See `providers.md` for provider definitions.

### Plugins

Each `spec.plugins` entry describes resources provided by an external plugin:

| Field | Type | Meaning |
| --- | --- | --- |
| `source` | string | Required plugin source path or repository. |
| `version` | string | Version to load; defaults to `latest`. |
| `namespace` | string | Resource namespace; inferred from the source if omitted. Standard namespaces and `local` are reserved. |
| `imports` | object | Optional `include` and `exclude` glob lists that filter exported resources. |

Only resources permitted by both the plugin's exports and workspace imports are loaded. Remote plugin sources are cached by source and version; existing local directory sources bypass the cache and are read directly. A relative local source beginning with `.` is resolved from the workspace root. Pin a version rather than relying on `latest` when stability matters, but caching alone does not guarantee reproducibility.

If matching entries exist in `warp.lock`, WARP verifies the resource directory and manifest hashes during loading; mismatches fail loading. Missing entries do not require a lock or trigger hash verification. This applies to local sources too, so edits to a locked local plugin can fail verification. Plugin resources are separate from the standard system, user, workspace, and project namespaces.

### Plugin manifest

A Plugin identifies a distributable repository and its public resources. WARP reads `PLUGIN.md` at the repository root, falling back to `PLUGIN.yaml` if the Markdown file is absent. A Markdown manifest can contain a body, which becomes the Plugin's instructions; it is not automatically added to the workspace prompt.

| `spec` field | Type | Meaning |
| --- | --- | --- |
| `resourceDir` | string | Repository-relative resource directory; defaults to `.agents/` when empty. |
| `exports` | string[] | Glob patterns for public resources; an omitted or empty list exports all resources, as does `["*"]`. |
| `hooks` | object | Optional lifecycle hook declarations. |

Export patterns match the resource name, `Kind/name`, or its path relative to the resource directory, including a path with the final extension removed. Matching is case-insensitive; `*` alone matches all resources. Workspace import filters further restrict the exported set. Workspace, Context, and Plugin are structural resources and cannot be explicitly exported.

`hooks.postInstall` is an array of command arrays, each containing an executable and its arguments. Although the schema describes post-install hooks and WARP discovery returns them as metadata, current WARP installation and TaskSmith workspace loading do not execute them. Do not rely on these declarations for setup.

```markdown
---
apiVersion: warp/v1alpha1
kind: Plugin
metadata:
  name: finance
  description: Shared financial analysis resources
spec:
  resourceDir: src/agents
  exports:
    - Skill/analysis
    - Command/*
  hooks:
    postInstall:
      - [go, install, ./...]
---
# Finance plugin
Shared analysis skills and commands.
```

### Workspace tool policies

`spec.policies.tools` controls execution independently of agent instructions. Omitted actions impose no restriction from that field; they do not bypass other permission or sandbox checks.

| Field | Type | Behavior |
| --- | --- | --- |
| `dangerous` | string | `deny` denies unsafe or delete plans and tools annotated dangerous; `ask` requires approval. |
| `openWorld` | string | `deny` denies network plans and tools annotated open-world; `ask` requires approval. |
| `readOnly` | boolean | When `true`, only `isReadOnly` tools are allowed; non-read-only plans are denied. |
| `include` | string[] | When nonempty, a tool name must match an included pattern. |
| `exclude` | string[] | Matching tool names are denied, even if included. |
| `ask` | string[] | Matching tool names require approval even when otherwise allowed. |

TaskSmith evaluates these workspace patterns against the called tool before permission modes and allow rules using WARP's resource-level policy matcher, which supports short names, `namespace/name`, and fully qualified tool patterns as well as `readOnly`, `ask`, `dangerous`, and `openWorld` actions; passing the policy does not automatically grant execution. A workspace `ask` action forces approval unless a matching user allow rule grants the plan. Agent tool policies are resolved separately and combined with the workspace ceiling; see `agents.md`.

## Workspace example

```markdown
---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: platform
  description: Platform projects
spec:
  projects: [services/api, services/auth, libs/core]
  defaultAgent: main
  defaultProvider: example
  defaultModels:
    completion: example/chat-model
    embedding: example/embedding-model
  policies:
    tools:
      exclude: [remove]
---
# Workspace guidance
Follow the platform standards in `docs/standards.md`.
```

The project directories and configured provider/models in this example are illustrative and must exist in the workspace configuration.

## Project context

WARP reads `AGENTS.md` at each mapped project root. It may be plain Markdown or a WARP `Context` manifest with frontmatter and a Markdown body. This is project context, not an automatic cascade of every ancestor directory's `AGENTS.md`. A `Context` defines no YAML `spec` fields: its `spec.instructions` is populated automatically from the Markdown body below the closing `---`, and frontmatter is optional on `AGENTS.md` (when omitted, WARP infers `apiVersion`, `kind: Context`, and `metadata.name`).

The workspace prompt and the selected agent prompt are rendered separately. The built-in `main`, `general`, and `explore` agents include the current project's context instructions (`{{.Context.Instructions}}` / `{{.Context.Path}}`); a custom agent that does not extend `main` can include `{{.Context.Instructions}}` in its own template. Context instructions are not automatically appended to every agent prompt.

```markdown
---
apiVersion: warp/v1alpha1
kind: Context
metadata:
  name: example-project
---
# Project guidance
Document verified build, test, and architecture conventions here.
```
