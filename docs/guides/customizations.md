# Customizations: Agents, Rules, Skills, Commands & Decisions

TaskSmith allows you to tailor your AI development environment by creating custom agent personas, ambient workspace rules, reusable workflow skills, prompt slash commands, and calibrated decision classifiers. Customizations can be scoped locally to a single project in `.agents/` or stored globally in `~/.config/tasksmith/agents/`.

---

## 1. Custom Agents (`*.agent.md`)

Agents define autonomous personas with dedicated system instructions, model preferences, tool authorizations, and delegation policies.

### Where Agents Live

- **Project Scope**: `.agents/agents/<name>.agent.md` (or `.agents/defs/<name>.agent.md`)
- **Global User Scope**: `~/.config/tasksmith/agents/<name>.agent.md`

### Authoring an Agent Manifest

Agent definitions are written as Markdown documents with YAML frontmatter:

```markdown
---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: security-auditor
  displayName: Security Auditor
  description: Audits code changes for vulnerabilities, secret leaks, and security regressions.
spec:
  # Optional: Inherit base instructions and tools from another agent
  extends: system:main

  # Cognitive configuration
  models:
    - anthropic/claude-sonnet-4-5
  temperature: 0.2
  thinking:
    type: effort
    effort: high

  # Scoped capabilities
  skills:
    - owasp-top-10
  subagents: [] # Prohibit delegation to subagents
  policies:
    tools:
      include:
        - view
        - grep
        - glob
        - bash
      exclude:
        - write
        - edit
---

# Security Auditor Persona

You are a senior application security engineer conducting rigorous code reviews.

### Review Checklist
1. Inspect input validation and sanitization on all HTTP boundaries.
2. Ensure no secrets, tokens, or private keys are hardcoded in source files.
3. Check SQL and database queries for injection vulnerabilities.
4. Verify authentication and authorization checks on every endpoint.

### Output Style
- Group findings by severity: Critical, High, Medium, Low, Informational.
- Provide a clear code reproduction or explanation for each vulnerability.
- Suggest remediations using secure coding best practices.
```

TaskSmith checks the agent's temperature and thinking settings against the selected model when a session is created or its agent or model changes. Unsupported settings are omitted and shown as warnings in the chat. Use `/clear` to dismiss them; warnings may appear again when the configuration changes or the session is reopened. If temperature support is unknown, the value is sent with a warning; the provider may reject it. You can correct model capability metadata in the provider manifest and reload the workspace. If a reload makes an explicit session thinking override unsupported, the override is retained and requests fail until you change or reset it. Title generation uses the title-generator agent's settings rather than the session's thinking override. See [Providers](../reference/providers.md).

### Specifying Inheritance (`extends`)
- **Extending Built-in Agents (`extends: system:main`)**: Inherits base prompt instructions, core coding workflows, and tool access from TaskSmith's default developer agent while layering your specialized guidelines on top.
- **Standalone Agents**: Omit `extends` for specialized roles (like code reviewers or documentation writers) where you want a pure, unpolluted prompt focused entirely on that domain.

### Scoping Capabilities
- **`tools`**: Restrict the agent's actions using `policies.tools.include` or `exclude`. For example, a code reviewer can be restricted to read-only tools (`view`, `grep`, `glob`).
- **`skills`**: Explicitly list which domain skills this agent should load (`["owasp-top-10"]`), or use `["*"]` to allow all skills in the workspace.
- **`rules`**: Attach additional rules as always-on constraints for this agent (see [Ambient Rules](#2-ambient-rules-rulemd)). Always-on rules apply to every agent regardless of this field.
- **`subagents`**: Define which agents this persona is allowed to invoke for subtasks (e.g., built-in `explore` for read-only codebase research, `general` for general-purpose engineering tasks, or `["*"]` for all agent-triggerable personas). When `subagents` resolves to at least one target agent, TaskSmith automatically provides `invoke_agent` and `manage_agents` to the session. Set to `[]` to prevent recursive delegation.

---

## 2. Ambient Rules (`*.rule.md`)

Rules are standing instructions — coding standards, architectural invariants, and guardrails — that apply whenever their conditions match. Unlike a skill, which an agent deliberately loads as a runbook, a rule is expected to be obeyed, so it is written as constraints rather than procedure.

### Where Rules Live

- **Project Scope**: `<subproject>/.agents/rules/<name>.rule.md`
- **Workspace Scope**: `.agents/rules/<name>.rule.md`
- **Global User Scope**: `~/.config/tasksmith/agents/rules/<name>.rule.md`

### Always-On vs. Conditional

Choose the injection mode deliberately, because it determines what every turn costs:

- **Always-on** (`triggers.always: true`) places the full body in the system prompt for every agent in scope, on every turn. Use it only for constraints that must never be skipped, and keep the body short.
- **Conditional** (`globs`, `tools`, or `keywords`) lists just the rule's name and triggers up front, and brings the body into context only when a trigger fires or the agent calls `load_rule`.

A path or tool trigger injects the rule automatically and deterministically. A `keywords` trigger is advisory only: it nudges the agent to consider the rule, which is the right choice for guidance you would not want enforced on a false positive.

### Authoring a Rule

```markdown
---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: go-error-handling
  description: Enforces sentinel error wrapping and prohibits panics in Go packages
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
      - panic
    useWhen: "Writing, refactoring, or reviewing error handling logic in Go source files."
---

# Go Error Handling Rules

1. Always wrap lower-level errors with `%w` and a package-prefixed context string.
2. Define exported sentinel errors in `types.go` or `errors.go`.
3. Never use `panic` for recoverable runtime errors.
```

### Inspecting and Attaching Rules

Run `/rules` to see every rule the workspace resolved with its status (`[Always]`, `[Loaded]`, `[Standby]`), scope, triggers, and token estimate. Press `i` to read a rule's full body and `r` to hot-reload rules edited on disk into the active session.

To force a rule into the current turn, type `@rule:<name>` anywhere in your prompt; it loads immediately instead of waiting for a trigger. When a rule loads automatically, the transcript shows a single `● Loaded Rule <name> (matched <path>)` line rather than the raw injected text.

Because an always-on rule reaches every agent and cannot be opted out of per agent, prefer conditional triggers for anything narrower than a workspace-wide mandate.

---

## 3. Reusable Skills (`*.skill.md` / `<name>/SKILL.md`)

Skills are on-demand knowledge runbooks, coding conventions, and specialized domain workflows that agents load when relevant to a task.

### Where Skills Live

- **Single-file Skill**: `.agents/skills/<name>.skill.md`
- **Skill Package with Resources**: `.agents/skills/<name>/SKILL.md`

### Authoring a Skill

```markdown
---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: postgres-migrations
  displayName: PostgreSQL Migrations
  description: Guidelines and runbook for writing zero-downtime database migrations.
spec:
  useWhen: Writing, modifying, or testing database schema migrations
  keywords:
    - postgres
    - sql
    - migrations
    - schema
  argumentHint: "Usage: /postgres-migrations [table]"
  arguments:
    - name: table
      description: Target table name
      required: false
      default: "users"
---

# PostgreSQL Zero-Downtime Migrations

Follow these non-negotiable rules whenever authoring database migrations in this repository:

### Safe Index Creation
- Always use `CREATE INDEX CONCURRENTLY` instead of standard `CREATE INDEX`.
- Never wrap concurrent index creation inside a transaction block (`BEGIN` / `COMMIT`).

### Adding Columns
- Add new columns as `NULL` first.
- If a default value is required on PostgreSQL 11+, specify it with `DEFAULT <val>`.
- Never lock large tables with long-running transactions.

### Running Migrations Locally
To test migrations before committing:
```bash
./scripts/db-test-migrate.sh
```
```

### Complex Skill Packages with Scripts & Resources

When a skill needs supporting helper scripts or templates, organize it as a directory:

```text
.agents/skills/deploy-kubernetes/
├── SKILL.md                 # Main instructions and workflow runbook
├── scripts/
│   ├── validate-helm.sh     # Validation script used during the workflow
│   └── diff-release.sh
└── resources/
    └── values-template.yaml # Scaffold configuration template
```

Agents can read these supporting files using `view` or execute helper scripts using `bash`.

### Triggering & Loading Skills
- **User Invocation**: Developers can invoke any skill directly in the CLI composer:
  ```text
  /postgres-migrations accounts
  ```
- **Autonomous Agent Loading**: Agents load skill instructions on demand using the built-in `load_skill` tool:
  ```json
  {
    "skill": "postgres-migrations",
    "arguments": {
      "table": "accounts"
    }
  }
  ```

---

## 4. Custom Slash Commands (`*.command.md`)

Commands provide reusable prompt templates that developers or agents can trigger directly in the CLI composer.

### Where Commands Live

- **Project Scope**: `.agents/commands/<name>.command.md`
- **Global User Scope**: `~/.config/tasksmith/commands/<name>.command.md`

### Authoring a Command

```markdown
---
apiVersion: warp/v1alpha1
kind: Command
metadata:
  name: benchmark
  displayName: Benchmark Function
  description: Scaffolds a high-precision benchmark for a Go function and runs performance analysis.
spec:
  models:
    - anthropic/claude-sonnet-4-5
  tools:
    - view
    - edit
    - write
    - bash
  argumentHint: "Usage: /benchmark <package_path> <function_name>"
  arguments:
    - name: package_path
      description: Package import path or directory
      required: true
    - name: function_name
      description: Target function name to benchmark
      required: true
---

# Benchmark Workflow

Please benchmark the function specified by the user:

1. Locate the file containing the function under the specified package: `{{.package_path}}`.
2. Create or update the corresponding `_test.go` file with a standard Go `Benchmark{{.function_name}}(b *testing.B)` implementation.
3. Run `go test -benchmem -run=^$ -bench=^Benchmark{{.function_name}}$ {{.package_path}}` using the `bash` tool.
4. Report the allocations per operation (`allocs/op`) and nanoseconds per operation (`ns/op`), identifying any obvious performance bottlenecks.
```

Trigger your custom command in the CLI:

```text
/benchmark ./internal/daemon/permissions MatchRule
```

For commands contributed by plugins, use the namespaced syntax:

```text
/<plugin-name>/<command> [args...]
```

---

## 5. System 1 Decisions (`*.decision.md`)

Decisions represent calibrated, low-latency probabilistic gates used to evaluate conditions, route tasks, or triage pull requests without invoking a heavy multi-turn conversational model.

### Where Decisions Live

- **Project Scope**: `.agents/decisions/<name>.decision.md`
- **Global User Scope**: `~/.config/tasksmith/decisions/<name>.decision.md`

### Authoring a Decision Manifest

Decisions support three question types:
1. **`predicate`**: Evaluates a binary true/false condition with explicit criteria and a confidence threshold.
2. **`choice`**: Categorizes input across discrete multiple-choice options.
3. **`score`**: Rates input against an ordinal rubric scale.

```markdown
---
apiVersion: warp/v1alpha1
kind: Decision
metadata:
  name: pr-risk-classifier
  description: Evaluates the architectural risk level of incoming pull requests.
spec:
  models:
    - ollama/nimble-v3
    - anthropic/claude-haiku-4-5
  questions:
    - name: contains_breaking_changes
      type: predicate
      instructions: Does this change alter public API signatures or database schemas in a backwards-incompatible way?
      threshold: 0.8
      criteria:
        true: Changes remove exported fields, alter HTTP endpoint contracts, or drop database columns.
        false: Changes are additive, internal-only, or backwards-compatible bug fixes.

    - name: risk_tier
      type: score
      instructions: Rate the operational risk of deploying this change to production.
      levels:
        - label: low
          description: Documentation, test additions, or trivial style fixes.
        - label: medium
          description: Internal business logic modifications with unit test coverage.
        - label: high
          description: Core authentication changes, payment processing, or database migrations.
---

# PR Risk Evaluation Guidelines

Focus strictly on production stability, backward compatibility, and customer impact.
```

---

## 6. Using the Built-in Customizations Skill

Ask the active agent to create or update a TaskSmith resource. It can load `tasksmith-customizations` and consult its focused references for agents, skills, rules, tools, MCP servers, providers, and workspace context without switching personas. Built-in skill files are materialized under `$XDG_DATA_HOME/tasksmith/builtin/` (normally `~/.local/share/tasksmith/builtin/`) and can be read by the agent. If that directory cannot be written, TaskSmith falls back to embedded resources; supporting files may then be unavailable.

---

For complete YAML frontmatter specifications and schema definitions, see the [Manifests Reference](../reference/manifests.md).
