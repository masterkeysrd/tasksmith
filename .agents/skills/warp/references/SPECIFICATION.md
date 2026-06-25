# Workspace Agent Resource Protocol (WARP)

**Version:** `warp/v1alpha1`

Warp is a language-agnostic, declarative format for defining AI agents and
their supporting resources. Resources are primarily plain Markdown files: a
YAML front-matter block carries structured metadata and configuration, while the
Markdown body below it becomes the resource's instruction text. Resources that
are pure configuration may also be defined as plain `.yaml` or `.yml` files.

Any runtime that can read a filesystem and parse YAML + Markdown can load and
execute Warp resources without modification.

---

## Table of Contents

1. [File Format](#1-file-format)
2. [Common Fields](#2-common-fields)
3. [Resource Kinds](#3-resource-kinds)
   - [3.1 Workspace](#31-workspace)
   - [3.2 Context](#32-context)
   - [3.3 Agent](#33-agent)
   - [3.4 Skill](#34-skill)
   - [3.5 Command](#35-command)
   - [3.6 ModelProvider](#36-modelprovider)
   - [3.7 Tool](#37-tool)
   - [3.8 MCP](#38-mcp)
   - [3.9 Toolkit](#39-toolkit)
4. [Cross-References](#4-cross-references)
5. [Discovery and Loading](#5-discovery-and-loading)
   - [5.1 Phase 1: Workspace Discovery](#51-phase-1-workspace-discovery)
   - [5.2 Phase 2: Project Mapping](#52-phase-2-project-mapping)
   - [5.3 Phase 3: Contextual Loading](#53-phase-3-contextual-loading)
   - [5.4 Resource Resolution Hierarchy](#54-resource-resolution-hierarchy)
6. [Directory Layout](#6-directory-layout)
7. [Validation Rules](#7-validation-rules)
8. [Parsing Algorithm](#8-parsing-algorithm)
9. [Plugins & Adapters](#9-plugins--adapters)
10. [Standard Namespaces](#10-standard-namespaces)
11. [Templating](#11-templating)

---

## 1. File Format

Every Warp resource is either a `.md` file or a `.yaml`/`.yml` file.

### Markdown Resources

Standard resources follow this structure:

```markdown
---
<YAML front-matter>
---

<Markdown body — becomes the resource's instruction text>
```

- The file **must** begin with a line containing only `---`.
- The YAML block **must** be closed by a second line containing only `---`.
- Everything after the closing `---` is treated as the instruction body.
- **Exception:** Files named `WORKSPACE.md` or `AGENT.md` (case-insensitive) may omit the YAML
  front-matter entirely. In this case, the entire file content is treated as the instruction body,
  and metadata is inferred by the loader.

### YAML Resources

Resources that are pure configuration (e.g. `ModelProvider`, `Tool`, `MCP`, `Toolkit`)
may omit the Markdown body and instruction delimiters. These files must have a
`.yaml` or `.yml` extension and contain only a valid YAML document.

---

## 2. Common Fields

Every resource kind shares the following top-level YAML fields.

| Field                  | Type   | Required | Description                                                                                |
|------------------------|--------|:--------:|--------------------------------------------------------------------------------------------|
| `apiVersion`           | string | ✅       | Schema version. Currently always `warp/v1alpha1`.                                          |
| `kind`                 | string | ✅       | Resource type. One of `Workspace`, `Context`, `Agent`, `Skill`, `Command`, `ModelProvider`, `Tool`, `MCP`, `Toolkit`. |
| `metadata.name`        | string | ✅       | Unique identifier for the resource within its kind.                                        |
| `metadata.description` | string |          | Short human-readable summary.                                                              |
| `metadata.displayName` | string |          | Pretty-printed label for UIs.                                                              |
| `metadata.labels`      | map    |          | Arbitrary key-value pairs for categorisation and filtering.                                |

---

## 3. Resource Kinds

### 3.1 Workspace

A `Workspace` is the **root authority** for the entire agentic environment. It defines the
boundaries of the session: which directories are active projects and any workspace-wide instruction
text. There is at most one `Workspace` per session.

The canonical filename is `WORKSPACE.md` (case-insensitive). A conforming loader locates this file
by climbing the directory tree from the current working directory (see
[Section 5.1](#51-phase-1-workspace-discovery)). If no file is found, a **Synthetic Workspace** is
created automatically with its root set to `$CWD`.

#### `spec` fields

| Field             | Type     | Required | Default  | Description                                                                                                                   |
|-------------------|----------|:--------:|----------|-------------------------------------------------------------------------------------------------------------------------------|
| `projects`        | string[] |          | `[]`     | Directories to load as active projects. `["*"]` discovers all non-hidden immediate subdirectories. An empty value or omission defaults to `["."]`. |
| `defaultProvider` | string   |          |          | Provider to use when an agent omits `spec.model`. May reference either a `ModelProvider.metadata.name` or `ModelProvider.spec.type`. |
| `policies`        | object   |          | —        | Security and access policies (e.g., tool execution restrictions).                                                             |

#### `Policies` fields

| Field   | Type   | Description                                           |
|---------|--------|-------------------------------------------------------|
| `tools` | object | Restrictions on which tools agents are allowed to use. |

#### `ToolPolicies` fields

| Field            | Type     | Default | Description                                                                                 |
|------------------|----------|---------|---------------------------------------------------------------------------------------------|
| `allowDangerous` | bool     | `true`  | If false, rejects any tool with `annotations.isDangerous: true`.                            |
| `allowOpenWorld` | bool     | `true`  | If false, rejects any tool with `annotations.isOpenWorld: true`.                            |
| `include`        | string[] | `[]`    | List of patterns for tools explicitly allowed. If set, tools not matching are forbidden.    |
| `exclude`        | string[] | `[]`    | List of patterns for tools explicitly forbidden.                                            |

Patterns in `include` and `exclude` are glob-matched against three target strings:
1. The tool's short name (e.g. `git-clone`).
2. The tool's namespace and name (e.g. `workspace/git-*`), allowing easy scope/namespace limits without specifying the Kind.
3. The tool's fully qualified name (e.g. `workspace/Tool/git-clone`).

#### Full example

```markdown
---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: my-workspace
  description: Monorepo workspace for the platform team.
spec:
  projects:
    - services/api
    - services/auth
    - libs/core
  defaultProvider: genai
---
```

# Platform Workspace

All agents in this workspace follow the platform coding standards documented in
`docs/standards.md`. Always prefer incremental, reversible changes.

---

### 3.2 Context

A `Context` defines the **identity and instructions** for a single project scope. It is the
authoritative entry point for the directory it lives in.

The canonical filename is `AGENT.md` (case-insensitive). A loader that finds an `AGENT.md` file
inside a project directory will automatically parse it as a `Context` resource. If the file lacks
YAML front-matter, the loader infers the `apiVersion`, `kind`, and `metadata.name` fields
automatically.

#### `spec` fields

_None beyond shared metadata._ The Markdown body becomes the `instructions` field.

#### Full example

```markdown
---
apiVersion: warp/v1alpha1
kind: Context
metadata:
  name: services-api
  description: Contextual rules for the API service project.
---

# API Service

You are working inside the `services/api` directory. This is a Go gRPC service.

## Rules
- Never modify generated `.pb.go` files directly.
- All new endpoints must have a corresponding integration test.
```

---

### 3.3 Agent

An `Agent` describes an autonomous actor: its LLM configuration, its persona
(the Markdown body), and the set of skills and commands it may invoke at
runtime.

#### `spec` fields

| Field         | Type     | Required | Default | Description                                                                                        |
|---------------|----------|:--------:|---------|----------------------------------------------------------------------------------------------------|
| `extends`     | string   |          | —       | Qualified Name (`namespace/Agent/name`) or Short Name of another Agent to extend. When set, the engine merges the parent's `skills` and `policies` blocks. |
| `triggers`    | string[] |          | `[]`    | Defines what architectural entities can invoke this agent (e.g. `["human"]`, `["agent"]`, `["system"]`). An empty list means it can be triggered by anything. |
| `models`      | string[] |          | `[]`    | A prioritized list of LLM model identifiers (e.g. `["gpt-4o", "claude-3-5-sonnet"]`). The runtime should use the first available model. |
| `temperature` | float    |          | `0.0`   | Sampling temperature in the range `0.0`–`2.0`. Higher values produce more varied output.          |
| `skills`      | string[] |          | `[]`    | Names or qualified refs of `Skill` resources this agent is allowed to use.                        |
| `commands`    | string[] |          | `[]`    | Names or qualified refs of `Command` resources this agent can invoke.                             |
| `policies`    | object   |          | —       | Security and access policies (specifically `tools` execution restrictions) for this agent.         |

> `instructions` is **not** written in the YAML front-matter. It is populated
> automatically from the Markdown body below the closing `---`.

#### Inheritance Merge Rules

When `spec.extends` is set the engine performs a **recursive merge**:

1. The parent agent is resolved (which may itself extend another agent, forming a chain).
2. A deep copy of the resolved parent is made as the merge base.
3. The child's `skills` lists are **appended** to the parent's lists.
4. The child's `policies` block is merged with the parent's:
   - Boolean options (`allowDangerous`, `allowOpenWorld`) in the child override those of the parent if specified.
   - Slice arrays (`include`, `exclude`) are combined using a set union.
5. The child's Markdown instructions are **concatenated** after the parent's, separated by a blank line.
6. Circular inheritance chains (including self-extension) are detected and return an error.

#### Tool Access Resolution and Policy Filtering

To support secure tool execution, the engine performs the following step during agent resolution:
1. **Policy Filter Check**: The complete list of effective tools is filtered against the agent's merged `spec.policies.tools` policy block. Only tools satisfying the policy are exposed to the agent.

#### Global Override Example

A user can augment the built-in `system/Agent/main` agent without replacing its
system prompt by placing an agent file in the `user` namespace:

```markdown
---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: main
  description: My customised main agent.
spec:
  extends: "system/Agent/main"
  skills:
    - go
    - git
---

## Additional Rules

Always prefer table-driven tests in Go.
```

Because `user` namespace has higher priority than `system`, `ResolveAgent("main")`
returns the merged agent: the system prompt prepended, then the user's extra rules
appended, with the skill lists combined.

#### Full example

```markdown
---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: analyst
  description: Senior financial analysis agent.
  displayName: Analyst
spec:
  models: ["gpt-4o", "claude-3-5-sonnet"]
  temperature: 0.2
  skills:
    - skills/finance.md
    - skills/sql.md
  commands:
    - commands/generate-report.md
---

# Analyst

You are a senior financial analyst with deep expertise in equity research and
SQL-based data retrieval. Always cite the data source before drawing any
conclusion.

## Rules
- Never fabricate figures. If data is unavailable, say so.
- Prefer concise bullet-point summaries unless a full narrative is requested.
```

---

### 3.4 Skill

A `Skill` bundles expertise guidelines for a specific domain. Agents load a skill's instruction
body to adopt a persona or follow a set of conventions. Skills carry no configuration beyond the
shared metadata header.

#### `spec` fields

_None._ The only content is the Markdown body, which becomes `instructions`.

#### Full example

```markdown
---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: finance
  description: Core financial analysis conventions and terminology.
---

# Finance Skill

## Terminology
- **EPS** — Earnings per share.
- **P/E ratio** — Price-to-earnings ratio.
- **EBITDA** — Earnings before interest, taxes, depreciation, and amortisation.

## Conventions
- Always express monetary values in USD unless the user specifies otherwise.
- Round percentages to two decimal places.
- Cite the reporting period (e.g. Q1 2026) when quoting financial figures.
```

---

### 3.5 Command

A `Command` encapsulates a discrete, reusable operation that an agent can invoke. Commands carry no
configuration beyond the shared metadata header; their behaviour is described entirely in the
Markdown body.

#### `spec` fields

_None._ The only content is the Markdown body, which becomes `instructions`.

#### Full example

```markdown
---
apiVersion: warp/v1alpha1
kind: Command
metadata:
  name: generate-report
  description: Produce a structured equity research report.
  displayName: Generate Report
spec:
  models: ["gpt-4o-mini"]
  tools: ["local/Tool/read-file"]
  hints: ["ticker", "year"]
---

# Generate Report

Produce a structured equity research report for the given ticker symbol.

## Output Format

<example>
## {TICKER} — Equity Research Report ({DATE})

### Summary
{Two-sentence executive summary}

### Key Metrics
| Metric | Value |
|--------|-------|
| ...    | ...   |

### Recommendation
{Buy / Hold / Sell} — {One-paragraph rationale}

Include a disclaimer at the end of every report.
</example>
```

---

### 3.6 ModelProvider

A `ModelProvider` resource describes an LLM provider configuration, such as API
endpoints and default models.

#### `spec` fields

| Field          | Type   | Required | Description                                     |
|----------------|--------|:--------:|-------------------------------------------------|
| `type`         | string | ✅       | e.g., `ollama`, `openai`, `anthropic`.          |
| `endpoint`     | string | ✅       | API base URL.                                   |
| `defaultModel` | string |          | Model to use if none specified.                 |
| `auth`         | object |          | Authentication config (e.g., `type: bearer`, `env: OPENAI_API_KEY`). |
| `models`       | object[]|          | Available models from this provider.            |

#### `ProviderAuth` fields

| Field    | Type   | Description                                                                 |
|----------|--------|-----------------------------------------------------------------------------|
| `type`   | string | The auth scheme (e.g., `bearer`, `api-key`, `basic`).                       |
| `header` | string | Custom HTTP header name to use if `type` is `api-key` (e.g., `x-api-key`).  |
| `env`    | string | Read the secret credential from this environment variable.                  |
| `file`   | string | Read the secret credential from this file path.                             |

#### `ProviderModel` fields

| Field    | Type   | Required | Description                                     |
|----------|--------|:--------:|-------------------------------------------------|
| `id`     | string | ✅       | Unique model ID (e.g., `gpt-4`).                |
| `name`   | string | ✅       | Model name (e.g., `gpt-4`).                     |
| `label`  | string | ✅       | Human-friendly label (e.g., `GPT-4`).           |
| `limits` | object | ✅       | Context and output token limits.                |

#### `ProviderModelLimits` fields

| Field     | Type | Required | Description                          |
|-----------|------|:--------:|--------------------------------------|
| `context` | int  | ✅       | Max context length in tokens.        |
| `output`  | int  | ✅       | Max output length in tokens.         |

#### Full example (`providers/ollama.yaml`)

```yaml
apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: local-ollama
  description: Local Ollama instance.
spec:
  type: ollama
  endpoint: http://localhost:11434
  defaultModel: llama3
  models:
    - id: llama3
      name: llama3
      label: Llama 3
      limits:
        context: 8192
        output: 4096
```

---

### 3.7 Tool

A `Tool` resource describes a custom tool that an agent can invoke.

#### `spec` fields

| Field         | Type     | Required | Description                                                                 |
|---------------|----------|:--------:|-----------------------------------------------------------------------------|
| `command`     | string[] | ✅       | Executable and static args (e.g., `["python", "script.py"]`).               |
| `env`         | map      |          | Environment variables injected into the process.                            |
| `parameters`  | map      |          | JSON Schema object defining arguments the LLM must pass.                    |
| `outputSchema`| map      |          | JSON Schema object defining the tool's output.                              |
| `annotations` | object   |          | Safety profile for Tool Execution Security.                                 |

> `instructions` is **not** written in the YAML front-matter. It is populated
> automatically from the Markdown body below the closing `---`. This Markdown body
> serves as the detailed description of what the tool does (sent to the LLM).

#### `ToolAnnotation` fields

| Field          | Type   | Default | Description                                               |
|----------------|--------|---------|-----------------------------------------------------------|
| `isOpenWorld`  | bool   | `false` | Interacts with external resources (network, etc.).         |
| `isDangerous`  | bool   | `false` | Can perform destructive actions (delete, format, etc.).    |
| `isReadOnly`   | bool   | `false` | Does not modify state.                                    |
| `isIdempotent` | bool   | `false` | Safe to retry on failure.                                 |
| `userHint`     | string | —       | Human-readable hint for approval prompts.                 |

#### Full example (`tools/ls.yaml`)

```yaml
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: list-files
  description: List files in a directory.
spec:
  command: ["ls", "-F"]
  description: List files in the current directory with type indicators.
  parameters:
    type: object
    properties:
      path:
        type: string
        description: Directory path to list.
```

---

### 3.8 MCP

An `MCP` resource describes an [Model Context Protocol](https://modelcontextprotocol.io)
server.

#### `spec` fields

| Field         | Type     | Required | Description                                     |
|---------------|----------|:--------:|-------------------------------------------------|
| `type`        | string   |          | Transport type: `stdio` (default) or `sse`.     |
| `command`     | string[] | *        | Command to start the MCP server (required if `type: stdio`). |
| `endpoint`    | string   | *        | HTTP URL for the MCP server (required if `type: sse`). |
| `env`         | map      |          | Environment variables for the MCP server.       |
| `annotations` | object   |          | Default safety profile for all exposed tools.   |
| `tools`       | object   |          | Controls which tools are exposed by this server. |
| `overrides`   | map      |          | Tool-specific annotation overrides.             |

#### `MCPFilter` fields

| Field     | Type     | Description                                           |
|-----------|----------|-------------------------------------------------------|
| `include` | string[] | Glob patterns for tools to expose.                    |
| `exclude` | string[] | Glob patterns for tools to block (applied after include).|

#### Full example (`mcps/sqlite.yaml`)

```yaml
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: sqlite-mcp
spec:
  type: stdio
  command: ["npx", "-y", "@modelcontextprotocol/server-sqlite", "--db", "data.db"]
```

#### Full example SSE (`mcps/remote.yaml`)

```yaml
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: remote-mcp
spec:
  type: sse
  endpoint: "http://localhost:8080/sse"
```

---

### 3.9 Toolkit

A `Toolkit` resource groups multiple tools together. Tools can be defined inline
or referenced by file path.

#### `spec` fields

| Field   | Type     | Required | Description                          |
|---------|----------|:--------:|--------------------------------------|
| `tools` | object[] | ✅       | Array of tool references or inline definitions. |

#### Full example (`toolkits/default.yaml`)

```yaml
apiVersion: warp/v1alpha1
kind: Toolkit
metadata:
  name: default-toolkit
spec:
  tools:
    - $ref: tools/ls.yaml
    - name: echo
      command: ["echo"]
      description: Print a message.
      parameters:
        type: object
        properties:
          msg: { type: string }
```

### 3.10 Plugin

A `Plugin` resource (typically named `PLUGIN.md` or `PLUGIN.yaml`) is placed at the root of a repository to declare it as a distributable WARP package. It acts as the manifest for the repository, telling the loader where to find the resources and which ones are meant for public consumption.

#### `spec` fields

| Field         | Type     | Required | Default    | Description                                                                                             |
|---------------|----------|:--------:|------------|---------------------------------------------------------------------------------------------------------|
| `resourceDir` | string   |          | `.agents/` | The relative path within the repository where the loader should look for resources.                     |
| `exports`     | string[] |          | `["*"]`    | Glob patterns defining which resources are exposed to consumers. Resources not matching are private.    |

#### Full example (`PLUGIN.md`)

```markdown
---
apiVersion: warp/v1alpha1
kind: Plugin
metadata:
  name: acme-finance
  description: Official financial analysis skills and tools for Acme Corp.
spec:
  resourceDir: "src/agents"
  exports:
    - skills/analysis
    - commands/*
---

# Acme Finance Plugin
This repository provides standard financial analysis skills.
```

---

## 4. Cross-References

An `Agent` resource may declare lists of skill and command references under `spec.skills` and
`spec.commands`. A reference can be either a **Qualified Name** or a **Short Name**.

- **Qualified Name**: `namespace/kind/name` (e.g. `system/Skill/python`). Directs the loader to exactly one resource in the registry.
- **Short Name**: `name` (e.g. `python`). The loader will iterate through the standard namespaces in priority order to find the first matching kind and name.

```yaml
spec:
  skills:
    - system/Skill/finance   # Qualified Name: Explicitly targets the system-level finance skill
  commands:
    - report                 # Short Name: Resolves using Search Path Priority
```

A conforming loader **must** verify that every referenced path resolves to a loaded resource of the
correct kind, applying the [resolution hierarchy](#54-resource-resolution-hierarchy). A reference
that cannot be satisfied is a validation error.

---

## 5. Discovery and Loading

Loading happens in three sequential phases. The result is a fully-populated **Workspace** that
contains one or more **Projects**, each with their own resource registries.

### 5.1 Phase 1: Workspace Discovery

The loader establishes the **Root Authority** for the session.

1. **Immediate Check:** Look for `WORKSPACE.md` (case-insensitive) in `$CWD`.
2. **Parent Climb:** If not found, move to the parent directory and repeat.
3. **Terminal Fallback:** If the filesystem root is reached without finding a `kind: Workspace`
   resource, the loader initialises a **Synthetic Workspace** with `RootPath = $CWD` and
   `Synthetic = true`.

> **Note:** The loader intentionally ignores `.git`, `go.mod`, and other structural anchors.
> Discovery is strictly tied to WARP files.

### 5.2 Phase 2: Project Mapping

Once `WORKSPACE_PATH` is established, the loader enumerates the active project directories.

| Value of `spec.projects`  | Behaviour                                                                                      |
|---------------------------|-----------------------------------------------------------------------------------------------|
| `["*"]`                   | Shallow scan of `WORKSPACE_PATH`. Every immediate subdirectory that **does not** start with a dot (`.`) is registered as an **Implicit Project**. |
| Explicit list             | Only the listed paths (e.g. `["services/api", "libs/core"]`) are loaded. If a path does not exist, the loader returns an error. |
| Empty / omitted           | Defaults to `["."]`. The workspace root itself is treated as the single project.              |

### 5.3 Phase 3: Contextual Loading

The loader enters each project directory and builds its **Identity** and **Resource Registry**.

**Per project:**

1. **Context Resolution:**
   - Check for `AGENT.md` (case-insensitive) in the project directory.
   - **Found:** Parse it as a `Context` resource.
   - **Missing:** No implicit context is created; the project has no instruction body.

2. **Resource Discovery:**
   - Walk the `.agents/` subdirectory relative to the project path.
   - Register all `.md`, `.yaml`, and `.yml` files of known kinds into the project's local
     registry.

3. **Project Naming (The Slug Rule):**
   - If the project path equals `"."`, the name is the workspace root folder's base name.
   - Otherwise, the name is the **Slugified Relative Path** of the project directory relative to
     `WORKSPACE_PATH` (e.g. `services/auth` → `services-auth`).

**Workspace-Global Resources (sub-directory projects only):**
The loader also walks `WORKSPACE_PATH/.agents/` and stores those resources in the workspace's own
global registry. These are made available as a fallback to all sub-directory projects.

#### The Authority Rule (Flat Root)

When `WORKSPACE_PATH == PROJECT_PATH` (i.e. the only project is `"."`), the `.agents/` directory
at the root **belongs exclusively to the Project**. The workspace's global library is treated as
empty. This prevents resources from being loaded twice and keeps attribution unambiguous.

| Topology | Owns `WORKSPACE_PATH/.agents/` | Global library |
|---|---|---|
| Sub-directory projects | Workspace (global scope) | Non-empty |
| Root-is-project (`"."`") | Project (local scope) | Empty |

### 5.4 Resource Resolution Hierarchy

Resources are identified by a **Qualified Name** (`namespace/Kind/name`) or a **Short Name**
(`name`). The Search Path Priority determines which resource wins when two namespaces provide a
resource with the same short name.

**Search Path Priority** (highest → lowest):

| Priority | Namespace   | Description                                                         |
|----------|-------------|---------------------------------------------------------------------|
| 100      | `local`     | Resources from the active project's own `.agents/` directory.       |
| 80       | `workspace` | Resources from `WORKSPACE_PATH/.agents/` (sub-directory projects).  |
| 60       | `user`      | User-level resources (e.g. `~/.config/warp/`).                      |
| 40       | `system`    | System/builtin resources embedded or installed globally.             |
| 0        | _(plugins)_ | Any other namespace contributed by a registered plugin.              |

**Short-Name Resolution Algorithm:**

1. Iterate namespaces in descending priority order.
2. For each namespace, check whether the registry contains any resource whose
   `metadata.name` matches the short name.
3. Return the first match found.

**Qualified-Name Resolution:** A reference containing `/` is treated as an exact qualified name
(`namespace/Kind/name`) and is looked up directly in the registry — no namespace search occurs.

**Shadowing (Effective View):** When the same short name exists in multiple namespaces, the
higher-priority namespace version **shadows** the lower-priority ones. The `Effective` query mode
returns only the winning resource per short name.

---

## 6. Directory Layout

A typical multi-project workspace:

```
WORKSPACE.md              # Root Authority — defines projects
.agents/                  # Workspace-global resources
│   ├── defs/
│   │   └── analyst.md
│   ├── skills/
│   │   └── coding.md
│   └── commands/
│       └── review.md
services/
├── api/
│   ├── AGENT.md          # Context — identity for the api project
│   └── .agents/          # Project-local resources
│       ├── skills/
│       │   └── grpc.md
│       └── commands/
│           └── gen-proto.md

└── auth/
    ├── AGENT.md
    └── .agents/
        └── skills/
            └── jwt.md
```

A minimal single-project setup (no `WORKSPACE.md` needed):

```
AGENT.md                  # Context for the single project
.agents/
├── skills/
└── commands/
```

---

## 7. Validation Rules

A conforming implementation **must** enforce the following rules when `Validate()` is called on a
loaded Workspace.

### 7.1 All resources

| Rule                         | Error condition                                                    |
|------------------------------|--------------------------------------------------------------------|
| `apiVersion` is present      | Field is empty or absent.                                          |
| `kind` is present            | Field is empty or absent.                                          |
| `metadata.name` is present   | Field is empty or absent.                                          |
| `kind` is a known value      | Value is not one of `Workspace`, `Context`, `Agent`, `Skill`, `Command`, `ModelProvider`, `Tool`, `MCP`, `Toolkit`. |

### 7.2 Agent-specific

| Rule                              | Error condition                                                              |
|-----------------------------------|------------------------------------------------------------------------------|
| All `spec.skills` paths resolve   | A listed path does not match any loaded `Skill` in the resolution hierarchy. |
| All `spec.commands` paths resolve | A listed path does not match any loaded `Command` in the resolution hierarchy. |

### 7.3 Workspace-specific

| Rule                                | Error condition                                                |
|-------------------------------------|----------------------------------------------------------------|
| All `spec.projects` paths exist     | A listed path (when not `"*"`) does not exist on the filesystem. |

### 7.4 Plugin-specific

| Rule                                | Error condition                                                |
|-------------------------------------|----------------------------------------------------------------|
| No structural resources in exports  | The `spec.exports` list matches a resource of kind `Workspace`, `Context`, or `Plugin`. |

---

## 8. Parsing Algorithm

The following algorithm describes how a conforming parser handles standard resources and the two
special inferred-manifest filenames.

```
function parse(filePath, fileContent):
  1. fileName = basename(filePath)
  2. isWorkspaceFile = (fileName.toLowerCase() == "workspace.md")
  3. isContextFile   = (fileName.toLowerCase() == "agent.md")

  4. Split fileContent on "---" into segments.

  5. IF segments.length < 3:
     IF isWorkspaceFile:
        # Infer Workspace resource
        resource.apiVersion  = "warp/v1alpha1"
        resource.kind        = "Workspace"
        resource.metadata.name = slugifyPath(filePath, workspaceRoot)
        resource.instructions = trim(fileContent)
        return resource
     ELSE IF isContextFile:
        # Infer Context resource
        resource.apiVersion  = "warp/v1alpha1"
        resource.kind        = "Context"
        resource.metadata.name = slugifyPath(filePath, workspaceRoot)
        resource.instructions = trim(fileContent)
        return resource
     ELSE:
        return error "missing front-matter delimiters"

  6. Decode segments[1] as YAML.
  7. IF kind is not in {Workspace, Context, Agent, Skill, Command, ModelProvider, Tool, MCP, Toolkit}:
     return error "unsupported kind"

  8. Populate resource from YAML and segments[2] (instructions).
  9. Return resource
```

### Path Slugification Rule

`slugifyPath(filePath, workspaceRoot)` produces a stable resource name:

1. Make `filePath` relative to `workspaceRoot`.
2. Remove the `.md` extension.
3. Replace all path separators (`/` or `\`) with a single hyphen `-`.
4. Convert to lowercase.

**Examples** (workspace root = `/home/user/repos/platform`):

| Input path                                         | Slug              |
|----------------------------------------------------|-------------------|
| `/home/user/repos/platform/AGENT.md`               | `agent`           |
| `/home/user/repos/platform/services/api/AGENT.md`  | `services-api-agent` |
| `/home/user/repos/platform/WORKSPACE.md`           | `workspace`       |

---

## 9. Modules and Package Management

### 9.1 Overview

WARP supports a robust module system for sharing resources across repositories. Instead of relying purely on dynamic runtime fetching, WARP uses semantic module paths, global caching, and cryptographic lock files to ensure fast, deterministic, and secure resource loading.

A repository becomes a WARP package simply by including a `kind: Plugin` manifest at its root (e.g., `PLUGIN.md`).

### 9.2 Declaring Plugins in a Workspace

Consumers import external plugins by declaring them in the `spec.plugins` list of their `Workspace` resource.

```yaml
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: my-workspace
spec:
  projects: ["services/api"]
  plugins:
    - source: github.com/acme/finance-skills
      version: v1.2.0
      namespace: ext-finance  # Explicitly override the namespace
    - source: github.com/shared/dev-tools
      version: latest
      # If namespace is omitted, WARP infers it from the last segment of the URI.
      # Here, the namespace will automatically be 'dev-tools'.
```

#### `WorkspacePlugin` fields

| Field       | Type   | Required | Description                                                                                 |
|-------------|--------|:--------:|---------------------------------------------------------------------------------------------|
| `source`    | string | ✅       | Semantic URI of the repository (e.g., `github.com/org/repo`).                               |
| `version`   | string | ✅       | The git tag, branch, or commit hash to pin (e.g., `v1.2.0`, `main`).                        |
| `namespace` | string |          | Namespace under which these resources are registered. Defaults to the last URI segment.     |

### 9.3 Resolution and the Default Namespace Rule

When a workspace declares a plugin, all exported resources from that plugin are registered in the loader's registry under a specific namespace.

**The Default Namespace Rule:**
If the consumer does not explicitly provide a `namespace` in the `WORKSPACE.md` definition, the loader **must** infer the namespace using the last segment of the `source` URI.

*Example:*
- `source: github.com/acme/finance-skills` → inferred namespace: `finance-skills`
- Resources inside the plugin will be accessible via `finance-skills/Skill/analysis`.

This ensures that plugin creators do not dictate namespaces (preventing collisions), while providing a zero-configuration fallback for consumers.

### 9.4 Caching and the Lock File

To guarantee reproducibility, security, and performance, WARP implements a global caching and lock file mechanism.

1. **Global Cache**: When the loader encounters a plugin, it checks a global host cache (e.g., `~/.warp/pkg/mod/`). If the specific version is missing, it is downloaded and cached. Subsequent loads are instantaneous from disk.
2. **`warp.lock`**: Tooling generates a machine-readable `warp.lock` file adjacent to the `WORKSPACE.md`. This file must be committed to version control. It records the exact version and a cryptographic hash (e.g., SHA-256) of the downloaded package's directory tree.
3. **Validation**: During the load phase, the loader verifies the cached package against the hash in `warp.lock`. If the hash does not match (indicating tampering or a network failure), the loader aborts, preventing supply-chain attacks.

#### Example `warp.lock`
```text
# This file is automatically generated by warp. DO NOT EDIT.
github.com/acme/finance-skills v1.2.0 h1:9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b
github.com/acme/finance-skills v1.2.0/PLUGIN.md h1:1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b
```

### 9.5 CLI Workflow (`warp get`)

The WARP CLI provides an ergonomic developer experience for managing plugins, actively assisting the user in curating their workspace.

When a user runs `warp get <source>@<version>`, the CLI performs the following steps:

1. **Fetch**: Downloads the repository to the global cache and parses the `PLUGIN.md` to identify available `exports`.
2. **Interactive Filtering**: If the plugin exports multiple resources, the CLI presents an interactive prompt allowing the user to select exactly which skills, commands, or tools they wish to import.
3. **Mutation**: The CLI automatically rewrites the `WORKSPACE.md` file, appending the plugin definition and scaffolding the `imports` filter block based on the user's selections.
4. **Locking**: The CLI computes the cryptographic hashes and updates the `warp.lock` file.

To perform operations non-interactively (e.g., in CI/CD environments), the CLI supports flag-based filtering:
```bash
warp get github.com/acme/finance-skills@v1.2.0 --include="Skill/analysis,Command/*"
```

---

## 11. Templating

WARP provides a unified templating system for rendering the `instructions` field of any resource. Runtimes **should** expand these templates before sending prompts to the LLM. 

The reference implementation uses Go's `text/template` engine, but with a preprocessing step to support a developer-friendly shorthand syntax.

### Shorthand Substitution

You can use shell-style variables directly in your Markdown instructions:

- **Named Variables:** `$Name`, `$DisplayName`, `${Description}`
- **Positional Arguments:** `$1`, `$2` (typically used in Commands)
- **Command Hints:** If a Command defines `hints: ["ticker"]`, you can use `$ticker` to refer to the first positional argument.
- **Escaping:** Use `$$` to output a literal `$`.

_Under the hood, the runtime translates `$Var` into Go template syntax `{{.Var}}` and `$1` into `{{index . "1"}}`._

### Flattened Resource Context

To minimize boilerplate, the template context is "flattened." You do not need to write `.Metadata.Name` or `.Spec.Models`. The following variables are lifted to the top level:

*   `Name` (from `metadata.name`)
*   `DisplayName` (from `metadata.displayName`, falls back to `Name`)
*   `Description` (from `metadata.description`)
*   `Labels` (from `metadata.labels`)
*   `Models`, `Skills`, `Tools`, `Commands`, `Triggers`, `Hints`, `Temperature` (lifted from the respective `spec` fields).

### Advanced Templating

Because it is backed by Go templates, you can write complex logic for arrays or conditionals:

```markdown
I am $DisplayName.
My skills are:
{{range .Skills}}
- {{.}}
{{end}}
```

### System Globals

Implementors (runtimes) can inject a `Globals` map during rendering. WARP restricts globals to project/workspace-related context (e.g., `PWD`, `WorkspaceRoot`, `CurrentProject`) rather than arbitrary OS environment variables to maintain portability and security. You access them just like any other variable: `$PWD`.

The following namespace identifiers are reserved and carry fixed priorities in the resolution
hierarchy. Plugins and adapters **must not** register resources under these names.

| Namespace   | Priority | Typical Source                                       |
|-------------|----------|------------------------------------------------------|
| `local`     | 100      | Project-local `.agents/` directory.                  |
| `workspace` | 80       | Workspace-global `.agents/` directory.               |
| `user`      | 60       | User configuration directory (e.g. `~/.config/warp/`).|
| `system`    | 40       | Embedded builtin resources shipped with the runtime.  |

Any namespace not listed above receives priority **0**. Resources in higher-priority namespaces
shadow same-named resources in lower-priority namespaces when queries use `Effective = true`.

