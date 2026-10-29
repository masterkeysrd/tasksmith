# Customizations: Agents, Skills, Commands & Decisions

TaskSmith allows you to tailor your AI development environment by creating custom agent personas, reusable workflow skills, prompt slash commands, and calibrated decision classifiers. Customizations can be scoped locally to a single project in `.agents/` or stored globally in `~/.config/tasksmith/agents/`.

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

### Specifying Inheritance (`extends`)
- **Extending Built-in Agents (`extends: system:main`)**: Inherits base prompt instructions, core coding workflows, and tool access from TaskSmith's default developer agent while layering your specialized guidelines on top.
- **Standalone Agents**: Omit `extends` for specialized roles (like code reviewers or documentation writers) where you want a pure, unpolluted prompt focused entirely on that domain.

### Scoping Capabilities
- **`tools`**: Restrict the agent's actions using `policies.tools.include` or `exclude`. For example, a code reviewer can be restricted to read-only tools (`view`, `grep`, `glob`).
- **`skills`**: Explicitly list which domain skills this agent should load (`["owasp-top-10"]`), or use `["*"]` to allow all skills in the workspace.
- **`subagents`**: Define which agents this persona is allowed to invoke for subtasks. Set to `[]` to prevent recursive delegation.

---

## 2. Reusable Skills (`*.skill.md` / `<name>/SKILL.md`)

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

## 3. Custom Slash Commands (`*.command.md`)

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

## 4. System 1 Decisions (`*.decision.md`)

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

## 5. Using the Built-in Customizations Skill

Ask the active agent to create or update a TaskSmith resource. It can load `tasksmith-customizations` and consult its focused references for agents, skills, rules, tools, MCP servers, providers, and workspace context without switching personas. Built-in skill files are materialized under `$XDG_DATA_HOME/tasksmith/builtin/` (normally `~/.local/share/tasksmith/builtin/`) and can be read by the agent. If that directory cannot be written, TaskSmith falls back to embedded resources; supporting files may then be unavailable.

Rule manifests can be authored using the skill, but automatic rule activation is not yet integrated into TaskSmith.

---

For complete YAML frontmatter specifications and schema definitions, see the [Manifests Reference](../reference/manifests.md).
