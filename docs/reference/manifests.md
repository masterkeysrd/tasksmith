# Resource Manifest Reference

TaskSmith uses declarative YAML manifests and Markdown frontmatter to configure workspaces, projects, agents, rules, skills, commands, decisions, tools, and external MCP servers.

Model `cost` pricing and tiers (`label`, `tier.type`, `tier.size`) drive the cost estimates recorded for every LLM call (shown by `/usage` and the status line); a model without `cost` and without a built-in profile price is recorded as unpriced.

For model provider configurations (`kind: ModelProvider`), including `cost` and tier precedence over built-in pricing, see the [Providers Reference](./providers.md).

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

### Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `projects` | array of strings | Relative paths to registered subprojects or packages. |
| `defaultProvider` | string | Default model provider name (e.g. `openai`, `ollama`). |
| `defaultModels` | map[string]string | Role-based model assignments: `completion`, `decision`, `embedding`. |
| `modelAliases` | map[string]string | Named model shorthands resolved to a canonical `<provider>/<model-id>`; referenced as `@<alias>`, `@default`, or `<provider>/@<alias>`. |
| `defaultAgent` | string | Persona activated for new sessions (defaults to `main`). |
| `plugins` | array of objects | External plugin resources to load: `source`, `version`, `namespace`, `imports.include`, `imports.exclude`. |
| `policies.tools.include` | array of strings | Tool grant glob patterns allowed in this workspace. |
| `policies.tools.exclude` | array of strings | Tool denial glob patterns blocked in this workspace. |
| `policies.tools.dangerous` | string | Policy for `isDangerous` tools: `allow`, `ask`, or `deny` (default `ask`). |
| `policies.tools.openWorld` | string | Policy for `isOpenWorld` tools: `allow`, `ask`, or `deny` (default `allow`). |
| `policies.tools.readOnly` | boolean | When `true`, only `isReadOnly` tools are allowed. |
| `policies.tools.ask` | array of strings | Tool glob patterns that require approval. |

*The Markdown body below the frontmatter serves as workspace-level system instructions inherited by all agents.*

---

## 3. Project Context Manifest (`AGENTS.md`)

Configures subproject-specific instructions in monorepos. Placed at `<subproject>/AGENTS.md`.

### Syntax

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

### Spec Reference

`Context` defines no YAML `spec` fields. Its `spec.instructions` is populated automatically from the Markdown body below the closing `---`, which becomes the project-specific instructions injected into the context. If `AGENTS.md` omits frontmatter, WARP infers `apiVersion`, `kind: Context`, and `metadata.name` automatically.

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
    - human
    - agent
  models:
    - anthropic/claude-opus-5
    - openai/o3
  temperature: 0.2
  thinking:
    type: effort
    effort: high
  tools:
    - view
    - grep
    - glob
  skills:
    - go-security
    - owasp-checklist
  commands:
    - audit
  subagents:
    - code-searcher
  rules:
    - go-conventions
  policies:
    tools:
      include:
        - view
        - grep
        - glob
      exclude:
        - write
        - edit
        - remove
        - bash
---

You are an expert application security auditor.
Your goal is to inspect code for OWASP Top 10 vulnerabilities, unauthorized path traversals, and cryptographic weaknesses.
Never attempt to modify code directly; report your findings with precise line references and remediation steps.
```

### Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `extends` | string | Qualified or short name of a base agent to inherit prompts and capabilities from. |
| `triggers` | array of strings | Allowed caller types (`human`, `agent`, `system`). Empty allows all callers. Agents without `human` cannot be selected by human users via `--agent` or `/agent`. |
| `models` | array of strings | Ordered model preferences (`provider/model` or workspace model alias). |
| `temperature` | float | Sampling temperature override (`0.0` to `2.0`). Sent when supported; omitted with a session warning when unsupported. |
| `thinking.type` | string | Reasoning strategy: `toggle`, `effort`, `budget`, or `adaptive`. |
| `thinking.effort` | string | Effort level for the `effort` strategy (e.g. `low`, `medium`, `high`). |
| `thinking.budgetTokens` | integer | Thinking token budget for the `budget` strategy. |
| `thinking.enabled` | boolean | Thinking toggle for the `toggle` strategy. |
| `tools` | array of strings | Eager tools loaded on the first request (`*`, `!pattern`, or explicit tool names). Defaults to `[]` when omitted. Policy-allowed tools not listed in `spec.tools` (and MCP tools by default) are loaded as deferred tools discoverable via `tool_search`. Advising more than 20 eager tools emits a warning. |
| `skills` | array of strings | Allowed skills (`*`, `!pattern`, or explicit skill names). |
| `commands` | array of strings | Allowed slash commands (`*`, `!pattern`, or explicit command names). |
| `subagents` | array of strings | Allowed subagents (`*`, `!pattern`, or explicit agent names). Only agents allowing the `agent` trigger can be invoked as subagents. |
| `rules` | array of strings | Rule references (`*`, qualified/short names, or globs). Each explicitly listed rule is attached as an always-on rule for this agent. Rules with `triggers.always: true` attach regardless of this field. `["*"]` also surfaces conditional rules for on-demand loading; omitted or `[]` attaches only always-on rules. |
| `policies.tools` | object | Unbypassable security policy ceiling for eager tools, deferred tools (`tool_search`), and skill/command tools (`include`, `exclude`, `dangerous`, `openWorld`, `readOnly`, `ask`). The resolved tool list is an explicit allowlist; an agent whose resolved tools are empty receives zero tools. |

*The Markdown body below the frontmatter contains the system persona prompt.*

Three built-ins are activated automatically when the workspace gives them something to do, so they may appear in the model's context without being listed in `spec.tools`: `tool_search` when any tool is deferred, `load_rule` when the agent resolves at least one conditional rule, and `read_mcp_resource` when a connected MCP server publishes a permitted resource. `policies.tools` still governs them, so denying one removes it.

---

## 5. Rule Manifest (`*.rule.md`)

Ambient constraints, coding standards, and tool/path guardrails that govern agent behavior. Placed under `.agents/rules/<name>.rule.md` (workspace) or `<subproject>/.agents/rules/<name>.rule.md` (project). A rule is a behavioral instruction, not a permission rule; enforcement of tool and file access still follows [Permissions](../architecture/permissions.md).

### Injection Model

| Rule kind | How it reaches the model |
| :--- | :--- |
| **Always-on** (`triggers.always: true`) | Rendered into a `<workspace_rules>` block in the static system prompt, so it stays in the cached prefix on every turn. |
| **Conditional** (any trigger other than `always`) | Listed by name in an `<available_rules>` discovery block only. The agent loads the full body on demand with `load_rule`, or the harness auto-injects it when a trigger fires. |

Conditional rules activate through three deterministic paths plus one advisory path, each injecting the full rendered body into a hidden `<system_reminders>` message and emitting a visible `● Loaded Rule …` timeline line:

| Trigger | Evaluated when | Effect |
| :--- | :--- | :--- |
| `@rule:<name>` in a user prompt | Any user message | Loads the full rule immediately (`manual`). |
| `triggers.tools` | A matching tool is invoked | Loads the full rule (`tool`). |
| `triggers.globs` | A tool reads, writes, searches, or executes against a matching workspace-relative path | Loads the full rule (`glob`). |
| `triggers.keywords` | A keyword appears in a user message | Advisory only: suggests the rule so the agent may call `load_rule`. |

Each rule is injected at most once per compaction epoch; `LoadedRules` and `SuggestedRules` reset on compaction, so long sessions do not accumulate stale rule bodies.

### Syntax

```markdown
---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: go-error-handling
  displayName: Go Error Handling Conventions
  description: Enforces sentinel error wrapping and prohibits panics in Go packages
  labels:
    category: backend
spec:
  priority: 10
  triggers:
    globs:
      - "**/*.go"
      - "!**/testdata/**"
    tools:
      - write
      - edit
    keywords:
      - error
      - sentinel
      - panic
    useWhen: "Writing, refactoring, or reviewing error handling logic in Go source files."
---

# Go Error Handling Rules

1. Always wrap lower-level errors with `%w` and a package-prefixed context string.
2. Define exported sentinel errors (`ErrInvalidConfig`, `ErrNotFound`) in `types.go` or `errors.go`.
3. Never use `panic` for recoverable runtime errors.
```

### Spec Reference

| Field | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `priority` | integer | `0` | Ordering when several rules resolve together; higher wins, ties break by `metadata.name`. |
| `triggers.always` | boolean | `false` | Injects the full body into `<workspace_rules>` for every agent in scope. |
| `triggers.globs` | array of strings | `nil` | Workspace-relative path patterns supporting `**` and `!` negation. A pathless pattern also matches on base name. |
| `triggers.tools` | array of strings | `nil` | Tool names or glob patterns (`*`, `mcp__*`); matched case-insensitively, also against short names of qualified tools. |
| `triggers.keywords` | array of strings | `nil` | Case-insensitive word-boundary keywords or phrases that produce a suggestion, never an automatic load. |
| `triggers.useWhen` | string | `""` | Natural-language condition shown to the agent in `<available_rules>`. |
| `triggers.decision` | string | `""` | **Unsupported.** Decision triggers are not evaluated for rules; setting this emits a startup warning that the reference is ignored. |

*The Markdown body below the frontmatter contains the rule instructions. Unlike skills, rules declare no `spec.arguments`; the body renders only against resource-level template globals such as `{{.WorkspaceDir}}`, `{{.ProjectDir}}`, and `{{.Agent}}`.*

### Attaching Rules to an Agent (`spec.rules`)

`spec.rules` in an Agent manifest adds rules on top of always-on activation, and its behavior is frequently misunderstood:

| `spec.rules` | Effect |
| :--- | :--- |
| Omitted or `[]` | Only rules with `triggers.always: true` apply. Conditional rules are not visible to the agent. |
| Explicit names or globs | The matched rules are attached as always-on rules for this agent, in addition to any `triggers.always: true` rule. There is no way to opt out of an always-on rule. |
| `["*"]` | Attaches every rule in scope. Always-on rules stay always-on; conditional rules are attached as conditional, so they appear in `<available_rules>` and can be loaded with `load_rule`. |

Because a workspace-wide or user-wide `triggers.always: true` rule reaches every agent, reserve `always` for constraints that must never be skipped. Prefer conditional triggers for anything narrower, so the body only enters context when relevant.

---

## 6. Skill Manifest (`*.skill.md` or `SKILL.md`)

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
  tools:
    - view
    - bash
    - grep
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
| `tools` | array of strings | Tool references or glob patterns required or used by this skill. Deferred tools in this list are automatically activated into context when the skill is loaded, subject to workspace and agent tool policy ceilings. |

*The Markdown body below the frontmatter contains the domain instructions.*

---

## 7. Command Manifest (`*.command.md`)

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
  agent: main
  context: inline
  models:
    - openai/gpt-5.5
  skills:
    - go-refactoring
  rules:
    - coding-standards
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
| `agent` | string | Preferred or required agent persona to execute this command. Spawns a delegated subagent session when different from the active session agent. |
| `context` | string | Conversational execution mode: `inline` (default, runs in the current session), `clean` (runs in a subagent session with empty history), or `fork` (runs in a subagent session with a snapshot of the current history). |
| `models` | array of strings | Prioritized model identifiers or aliases for executing this command. Overrides the active model for the duration of the command turn and restores the previous model afterward. |
| `skills` | array of strings | Skill references or globs eagerly embedded into context when executing this command. |
| `rules` | array of strings | Rule references or globs eagerly loaded (`always: true`) when executing this command. |
| `tools` | array of strings | Tool references or globs required by this command. Deferred tools in this list (and in `spec.skills`) are automatically activated into context, subject to workspace and agent tool policy ceilings. |
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

## 8. Decision Manifest (`*.decision.md`)

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

## 9. Tool Manifest (`*.tool.md`)

Defines a custom CLI script or executable wrapper as a callable agent tool. Placed under `.agents/tools/<name>.tool.md`.

Tool manifests are discovered like any other resource, so a project manifest can override a workspace or user manifest with the same name. An already registered built-in tool with the same name always wins over a custom tool.

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
    - {{.Workspace.Dir}}/{{.Input.path}}
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

### Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `command` | array of strings | Executable binary and argument list. Each element is an independent argv entry — never a shell command line. Elements may contain command templates (see below). |
| `env` | map[string]string | Environment variables injected into the process. Values are used verbatim (not templated). The host daemon environment is never inherited. |
| `inputSchema` | object | JSON Schema describing the tool's input arguments. Call arguments are validated against it before execution. |
| `outputSchema` | object | JSON Schema describing the tool's structured output. Metadata only: it is advertised to the model and is not used to parse or validate output. |
| `annotations.isReadOnly` | boolean | Set `true` if tool execution does not mutate state. |
| `annotations.isIdempotent` | boolean | Set `true` if repeated runs produce identical outputs. Does not enable automatic retries. |
| `annotations.isDangerous` | boolean | Set `true` if execution can modify system configuration. |
| `annotations.isOpenWorld` | boolean | Set `true` if tool interacts with unconstrained network/shell. |
| `annotations.userHint` | string | Display pattern for interactive approval prompts. `%s` is replaced with the rendered command line. |

*The Markdown body below the frontmatter becomes the tool's instructions. If it is empty, `metadata.description` is used as the description; otherwise a description is generated and the rendered body is attached as instructions.*

### Command Templating

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

### Argument Delivery

Arguments reach the process in exactly one of two ways, never both:

| Manifest authoring | Delivery |
| :--- | :--- |
| At least one command element consumes an argument (`{{.Input.<field>}}` or the `{{.<field>}}` shorthand for a supplied key) | Rendered argv only. Standard input is left closed. |
| No command element consumes an argument | The validated arguments are marshalled as a single JSON object on standard input. |

An absent or unparseable `inputSchema` falls back to a generic object schema, and arguments are still delivered on standard input.

### Execution Model

- Standard output is returned to the model as text. When it exceeds the output limits, the full content is spilled to a file and the model receives a head-plus-tail preview pointing at that file.
- A nonzero exit code reports standard error, then standard output if standard error is empty, then the exit code alone.
- Execution runs inside the sandbox and is subject to the permission system. The process environment is built from the sandbox configuration plus `spec.env`; the host daemon's environment is never inherited.

### Status: Eager, Deferred, Denied, or Inactive

Whether a tool is visible to the model is decided per session from the agent's tool selection and tool policy, not by the manifest:

| Status | Meaning |
| :--- | :--- |
| `eager` | Listed in the agent's `spec.tools`; present in the initial model context. |
| `deferred` | Registered in the session but withheld from the initial context; activated by a `tool_search` match on a previous turn. |
| `denied` | Blocked by workspace, agent, or MCP server tool policy. |
| `inactive` | Neither loaded, deferred, nor denied (for example, a conditional built-in that the current session does not activate). |

Custom and MCP tools are deferred by default. MCP tools stay deferred even under `spec.tools: ["*"]`; to load one eagerly, name it outright as `<server>__<tool>` or `mcp__<server>__<tool>`. Inspect the resolved status of every tool in a session with the [`/tools` browser](cli.md#tools-browser-tools).

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

A `policies` filter is enforced as a hard block against the agent: a prohibited tool is never registered and reports status `denied`, while a prohibited prompt or resource read is rejected with `403` `mcp_policy_prohibited` naming the item, the policy section, and the manifest path. Tightening a filter takes effect immediately, including for sessions that already discovered the tool. Only a user may authorize a single prohibited prompt or resource read with `allowProhibited` — through the [`/mcp`](cli.md#mcp-item-confirmation) confirmation or the [MCP HTTP endpoints](cli.md#mcp-http-endpoints) — and only a user editing the manifest can permit a prohibited tool. See [MCP Tool Execution Model](tools.md#6-mcp-tool-execution-model).
