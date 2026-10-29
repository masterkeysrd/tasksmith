# Resource Manifest Reference

TaskSmith uses declarative YAML manifests and Markdown frontmatter to configure workspaces, projects, agents, skills, commands, decisions, tools, toolkits, and external MCP servers.

For model provider configurations (`kind: ModelProvider`), see the [Providers Reference](./providers.md).

---

## 1. Resource Hierarchy & Metadata

Resources can be defined across four priority tiers:

| Tier | Priority | Namespace | Location |
| :--- | :--- | :--- | :--- |
| **System** | 1 (Base) | `system` | Embedded in the TaskSmith binary; workspace built-ins are mirrored under `$XDG_DATA_HOME/tasksmith/builtin/`. |
| **User** | 2 | `user` | `~/.config/tasksmith/` (`$XDG_CONFIG_HOME`). |
| **Workspace** | 3 | `workspace` | `<workspace-root>/WORKSPACE.md` and `<workspace-root>/.agents/`. |
| **Local** | 4 (Highest)| `local` | `<subproject>/AGENTS.md` and `<subproject>/.agents/`. |

### Common Metadata Fields

Every manifest includes a standard `metadata` block:

```yaml
metadata:
  name: reviewer                 # Unique resource identifier within the namespace (required)
  displayName: Code Reviewer     # Human-readable title for UI and pickers
  description: Pull request reviewer for Go and TypeScript
  labels:
    tier: production
    category: engineering
```

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `name` | string | **Yes** | Unique identifier within this resource kind and namespace. |
| `displayName` | string | No | Human-readable title displayed in interactive pickers. |
| `description` | string | No | Short summary explaining what this resource does. |
| `labels` | map[string]string | No | Key-value pairs for metadata categorization. |

---

## 2. Workspace Manifest (`WORKSPACE.md`)

Anchors a repository as a TaskSmith workspace. Placed at `<workspace-root>/WORKSPACE.md`.

### Syntax

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
  defaultModel: gpt-5.5
  defaultModels:
    completion: openai/gpt-5.5
    decision: typesafe/jev-latest
    embedding: openai/text-embedding-3-small
  defaultAgent: main
  policies:
    tools:
      include:
        - "*"
      exclude:
        - "bash(rm -rf /*)"
      allowDangerous: false
      allowOpenWorld: true
---

# Workspace Guidelines

Always run tests before completing code generation tasks.
Ensure error handling wraps sentinels using `%w`.
```

### Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `projects` | array of strings | Relative paths to registered subprojects or packages. |
| `defaultProvider` | string | Default model provider name (e.g. `openai`, `ollama`). |
| `defaultModel` | string | Default completion model ID. |
| `defaultModels` | map[string]string | Role-based model assignments: `completion`, `decision`, `embedding`. |
| `defaultAgent` | string | Persona activated for new sessions (defaults to `main`). |
| `policies.tools.include` | array of strings | Tool grant glob patterns allowed in this workspace. |
| `policies.tools.exclude` | array of strings | Tool denial glob patterns blocked in this workspace. |
| `policies.tools.allowDangerous` | boolean | Set `false` to prevent dangerous tool calls without approval. |
| `policies.tools.allowOpenWorld` | boolean | Set `false` to restrict open-world network or external tools. |

*The Markdown body below the frontmatter serves as workspace-level system instructions inherited by all agents.*

---

## 3. Project Context Manifest (`AGENTS.md`)

Configures subproject-specific instructions and resource references in monorepos. Placed at `<subproject>/AGENTS.md`.

### Syntax

```markdown
---
apiVersion: warp/v1alpha1
kind: Context
metadata:
  name: backend
  displayName: Backend Microservice
  description: Go gRPC microservice conventions
spec:
  resources:
    - .agents/skills/proto.skill.md
    - .agents/tools/migrate.tool.md
---

# Backend Conventions

- All network endpoints must pass through the gRPC interceptor chain.
- Database access is handled exclusively through pgx pools.
```

### Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `resources` | array of strings | Relative file paths to subproject-scoped agents, skills, or tools. |

*The Markdown body below the frontmatter provides project-specific instructions injected into the context.*

---

## 4. Agent Manifest (`*.agent.md`)

Defines autonomous agent personas, allowed tools, subagents, and cognitive configuration. Placed under `.agents/agents/<name>.agent.md` or `~/.config/tasksmith/agents/`.

### Syntax

```markdown
---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: security-auditor
  displayName: Security Auditor
  description: Audits code for vulnerabilities, injection flaws, and auth gaps
spec:
  extends: main
  triggers:
    - "audit security"
    - "check vulnerabilities"
  models:
    - anthropic/claude-opus-5
    - openai/o3
  temperature: 0.2
  thinking:
    effort: high
  skills:
    - go-security
    - owasp-checklist
  commands:
    - audit
  subagents:
    - code-searcher
  tools:
    - view
    - grep
    - glob
  policies:
    tools:
      exclude:
        - "write"
        - "edit"
        - "remove"
        - "bash"
---

You are an expert application security auditor.
Your goal is to inspect code for OWASP Top 10 vulnerabilities, unauthorized path traversals, and cryptographic weaknesses.
Never attempt to modify code directly; report your findings with precise line references and remediation steps.
```

### Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `extends` | string | Qualified or short name of a base agent to inherit prompts and capabilities from. |
| `triggers` | array of strings | Phrases or patterns that trigger automatic agent suggestion or routing. |
| `models` | array of strings | Prioritized list of model identifiers to use for this agent. |
| `temperature` | float | Sampling temperature (0.0 to 2.0). Sent when supported; omitted with a session warning when unsupported. |
| `thinking.effort` | string | Model-supported effort level for the agent. |
| `thinking.type` | string | `effort` when specifying the level through `thinking.default`. |
| `thinking.default` | string | Default effort level when `thinking.type` is `effort`. |
| `thinking.budget_tokens` | integer | Thinking token budget for models that support budgets. |
| `thinking.enabled` | boolean | Thinking toggle for models that support it. |
| `skills` | array of strings | Names of domain skills made available to this agent. |
| `commands` | array of strings | Names of invokable slash commands made available to this agent. |
| `subagents` | array of strings | Names of subagents this agent is permitted to invoke. |
| `tools` | array of strings | Names of tools granted to this agent. |
| `policies.tools` | object | Agent-level tool restrictions (`include`, `exclude`, `allowDangerous`). |

*The Markdown body below the frontmatter contains the system persona prompt.*

---

## 5. Skill Manifest (`*.skill.md` or `SKILL.md`)

Bundles domain expertise, procedural instructions, and reference patterns. Placed under `.agents/skills/<name>.skill.md` or `.agents/skills/<name>/SKILL.md`.

### Syntax

```markdown
---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: postgres-migrations
  displayName: PostgreSQL Migrations
  description: Guidelines for authoring zero-downtime database migrations
spec:
  useWhen: "Authoring or modifying database schemas, table definitions, or SQL migrations."
  keywords:
    - postgres
    - sql
    - migration
    - schema
  argumentHint: "Usage: /postgres-migrations [table_name]"
  arguments:
    - name: table_name
      description: Target database table being modified
      required: false
      default: "users"
---

# PostgreSQL Migration Rules

1. Always add new columns as `NULL` or provide a default with `DEFAULT ... NOT NULL` on PostgreSQL 11+.
2. Create indexes concurrently: `CREATE INDEX CONCURRENTLY idx_{{.table_name}}_email ON {{.table_name}}(email);`.
3. Never drop columns in the same release that removes the application code reading them.
```

### Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `useWhen` | string | Natural language condition explaining when agents should load this skill. |
| `keywords` | array of strings | List of keywords that trigger skill discovery. |
| `argumentHint` | string | Usage signature hint displayed during autocomplete and validation. |
| `arguments` | array of objects | Structured parameter declarations accepted by this skill. |

*The Markdown body below the frontmatter contains the domain instructions.*

---

## 6. Command Manifest (`*.command.md`)

Defines reusable command prompt templates invokable via `/name` or agent delegation. Placed under `.agents/commands/<name>.command.md`.

### Syntax

```markdown
---
apiVersion: warp/v1alpha1
kind: Command
metadata:
  name: refactor
  displayName: Refactor Module
  description: Refactors a module for improved readability and modularity
spec:
  models:
    - openai/gpt-5.5
  tools:
    - view
    - edit
    - write
    - bash
  argumentHint: "Usage: /refactor <target_file> [focus]"
  arguments:
    - name: target_file
      description: Source file to refactor
      required: true
    - name: focus
      description: Refactoring focus area (e.g. modularity, concurrency)
      required: false
      default: "readability"
---

Please analyze the file {{$target_file}} focusing on {{$focus}}:
1. Identify code duplication, high cyclomatic complexity, or violation of single-responsibility.
2. Formulate a refactoring plan and present a unified diff using `edit`.
3. Verify that all tests pass using `bash(go test ./...)`.
```

### Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `models` | array of strings | Preferred model identifiers for executing this command. |
| `tools` | array of strings | Tools made accessible during command execution. |
| `argumentHint` | string | Usage signature hint displayed during autocomplete and validation. |
| `arguments` | array of objects | Structured parameter declarations accepted by this command. |

### Argument Object Reference

Every parameter declared in `spec.arguments` supports:

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `name` | string | **Yes** | Identifier used for template variable substitution (`{{.name}}` or `$name`). |
| `description` | string | No | Guidance describing the purpose and expected format of the argument. |
| `required` | boolean | No | When `true`, invocation fails validation if the argument is omitted and has no default. |
| `default` | string | No | Fallback value automatically injected by the renderer when omitted. |

*The Markdown body below the frontmatter contains the execution instructions.*

---

## 7. Decision Manifest (`*.decision.md`)

Configures probabilistic System 1 decision classifiers for deterministic routing or rubric scoring. Placed under `.agents/decisions/<name>.decision.md`.

### Syntax

```markdown
---
apiVersion: warp/v1alpha1
kind: Decision
metadata:
  name: code-review-gate
  displayName: Code Review Quality Gate
  description: Classifies pull requests for security and stability risks
spec:
  models:
    - typesafe/jev-latest
  argumentHint: "Usage: evaluate code diff against security criteria"
  arguments:
    - name: diff
      description: Unified diff to evaluate
      required: true
  questions:
    - name: introduces_vulnerability
      type: predicate
      instructions: "Determine if the diff introduces a SQL injection, XSS, or unauthorized path traversal."
      threshold: 0.85
      minConfidence: 0.90
      criteria:
        true: "Contains unescaped user input passed to SQL or shell commands."
        false: "Input is fully validated, parameterized, and sanitized."
    - name: test_coverage_tier
      type: choice
      instructions: "Evaluate the test coverage included with this change."
      options:
        - value: complete
          description: "Unit and integration tests cover all modified code paths."
        - value: partial
          description: "Basic smoke tests present, but edge cases omitted."
        - value: none
          description: "No automated tests included with the diff."
    - name: architecture_score
      type: score
      instructions: "Rate conformity to clean architecture principles."
      levels:
        - label: low
          description: "Tight coupling, business logic mixed into presentation."
        - label: medium
          description: "Reasonable modularity with minor boundary leaks."
        - label: high
          description: "Exemplary decoupling and clear inversion of control."
---

Evaluate the provided pull request diff against the defined criteria.
```

### Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `models` | array of strings | Preferred decision models (e.g. `typesafe/jev-latest`). |
| `argumentHint` | string | Guidance string for invocation syntax. |
| `arguments` | array of objects | Structured parameter declarations accepted by this decision. |
| `questions` | array of objects | Structured evaluation questions. |

#### Question Object Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `name` | string | Unique question identifier. |
| `type` | string | Question kind: `predicate` (boolean), `choice` (categorical), or `score` (ordinal). |
| `instructions` | string | Specific evaluation instructions for this question. |
| `threshold` | float | Minimum decision probability threshold (0.0 to 1.0) for `predicate`. |
| `minConfidence` | float | Minimum required model confidence score. |
| `criteria.true` | string | Explicit positive condition description for `predicate`. |
| `criteria.false` | string | Explicit negative condition description for `predicate`. |
| `options` | array of `{value, description}` | Allowed categories for `choice` questions. |
| `levels` | array of `{label, description}` | Ordinal rubric levels for `score` questions. |

---

## 8. Tool Manifest (`*.tool.md`)

Defines a custom CLI script or executable wrapper as a callable agent tool. Placed under `.agents/tools/<name>.tool.md`.

### Syntax

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
  env:
    GOROOT: "/usr/local/go"
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

### Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `command` | array of strings | Executable binary and default argument list. |
| `env` | map[string]string | Custom environment variables injected during execution. |
| `inputSchema` | object | JSON Schema describing the tool's input arguments. |
| `outputSchema` | object | JSON Schema describing the tool's structured output. |
| `annotations.isReadOnly` | boolean | Set `true` if tool execution does not mutate state. |
| `annotations.isIdempotent` | boolean | Set `true` if repeated runs produce identical outputs. |
| `annotations.isDangerous` | boolean | Set `true` if execution can modify system configuration. |
| `annotations.isOpenWorld` | boolean | Set `true` if tool interacts with unconstrained network/shell. |
| `annotations.userHint` | string | Display pattern for interactive approval prompts. |

---

## 9. Toolkit Manifest (`*.toolkit.yaml`)

Groups related tools into a single reusable bundle. Placed under `.agents/toolkits/<name>.toolkit.yaml`.

### Syntax

```yaml
apiVersion: warp/v1alpha1
kind: Toolkit
metadata:
  name: dev-tools
  displayName: Development Tools
  description: Essential developer inspection and execution tools
spec:
  tools:
    - ref: view
    - ref: edit
    - ref: write
    - ref: bash
    - name: test-runner
      description: Runs unit tests
      command:
        - go
        - test
        - ./...
      annotations:
        isReadOnly: true
        isDangerous: false
```

### Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `tools` | array of objects | Tool entries, each containing either a `ref` string pointing to an existing tool, or an inline tool definition (`name`, `command`, `annotations`, `inputSchema`). |

---

## 10. MCP Server Manifest (`*.mcp.yaml`)

Configures an external Model Context Protocol (MCP) server integration. Placed under `.agents/mcps/<name>.mcp.yaml`.

### Syntax

```yaml
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: sqlite-explorer
  displayName: SQLite Database Explorer
  description: Model Context Protocol server exposing database inspection and queries
spec:
  type: stdio
  command:
    - npx
    - -y
    - "@modelcontextprotocol/server-sqlite"
    - "--db-path"
    - "./data/app.db"
  env:
    DEBUG: "mcp:*"
  annotations:
    isReadOnly: true
    isDangerous: false
    isOpenWorld: false
  policies:
    tools:
      include:
        - "read_query"
        - "list_tables"
        - "describe_table"
      exclude:
        - "write_query"
    prompts:
      include:
        - "*"
    resources:
      include:
        - "schema://*"
  overrides:
    write_query:
      isReadOnly: false
      isDangerous: true
```

### Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `type` | string | Transport protocol: `stdio`, `sse`, or `http`. |
| `command` | array of strings | Command line and arguments for `stdio` process execution. |
| `endpoint` | string | HTTP/SSE server URL for network-based MCP servers. |
| `env` | map[string]string | Environment variables passed to the child process. |
| `auth` | object | Authentication config (`type`, `env`, `header`, `file`) for HTTP/SSE endpoints. |
| `headers` | map[string]string | Custom HTTP headers sent with requests. |
| `annotations` | object | Default safety annotations applied to all tools exported by this MCP. |
| `policies.tools` | object | Inclusion/exclusion filter globs for exported tools (`include`, `exclude`). |
| `policies.prompts` | object | Inclusion/exclusion filter globs for exported prompts. |
| `policies.resources` | object | Inclusion/exclusion filter globs for exported resources. |
| `overrides` | map of objects | Per-tool safety annotation overrides keyed by tool name. |
