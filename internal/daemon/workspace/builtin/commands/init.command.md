---
apiVersion: warp/v1alpha1
kind: Command
metadata:
  name: init
  displayName: Initialize AGENTS.md
  description: Inspect the codebase and interactively create or update AGENTS.md instructions
spec:
  context: inline
  skills:
    - tasksmith-customizations
  tools:
    - ls
    - view
    - glob
    - grep
    - ask_question
    - write
    - edit
---

Analyze this codebase and create or update `AGENTS.md` to help future agents work effectively in this repository, following `references/agents-md.md` from the `tasksmith-customizations` skill.

### 1. Pre-Flight Check
- Check directory contents with `ls` (and `glob` if needed).
- If the repository is empty or contains only bare configuration/boilerplate with no real source code yet, **stop immediately without writing a file** and tell the user: *"Directory appears empty or only contains configuration. Add source code first, then run `/init` to generate `AGENTS.md`."*
- If `WORKSPACE.md` maps multiple projects (`spec.projects`) or the repository is a monorepo with distinct subprojects, use `ask_question` to confirm whether to target root `AGENTS.md`, a subproject `<project>/AGENTS.md`, or both.

### 2. Discovery Process
1. **Existing agent & editor instructions**: Check for existing instruction/rule files (`AGENTS.md`, `CLAUDE.md`, `.cursor/rules/*.md`, `.cursor/rules/*.mdc`, `.cursorrules`, `.github/copilot-instructions.md`, `.windsurfrules`, `.agents/rules/*.rule.md`) — read only those that exist.
2. **Project & toolchain manifests**: Inspect build, package, task-runner, linter, and CI configs (`go.mod`, `package.json`, `Cargo.toml`, `pyproject.toml`, `Makefile`, `justfile`, `Taskfile.yml`, `.golangci.yml`, `.github/workflows/*.yml`, etc.) to extract exact build, test (full suite + targeted single-test/package), codegen, lint, and typecheck commands.
3. **Representative source & test files**: Read entry points, core domain packages, key interfaces, and 1–2 representative test files to understand actual architecture, component boundaries, control/data flow, error handling, naming/testing idioms, and non-obvious gotchas (such as `DO NOT EDIT` generated files, lock ordering, or sandbox/trust boundaries).
4. **Clarify true ambiguities**: If critical workflows remain ambiguous (e.g., competing test runners or unclear canonical build targets), ask the user with `ask_question` rather than guessing.

### 3. Progressive Disclosure & Signal Quality
- **Non-obvious knowledge over surface filler**: `AGENTS.md` is injected into context on every turn. Mentioning obvious details an agent immediately sees from `ls` or reading one file (exhaustive file trees, dependency lists copied from manifests, or generic language platitudes) wastes tokens and dilutes attention. Focus on what saves trial-and-error: how components fit together, control/data flow across packages, commands with surprising flags or required env vars, implicit conventions, and sharp gotchas.
- **Layer context progressively**: Keep `AGENTS.md` focused on repository-wide essentials. Link to existing in-repo architecture/reference docs with relative Markdown links instead of duplicating them, and if you encounter large path- or tool-specific rule sets (for example, glob-scoped `.cursor/rules/` files), keep `AGENTS.md` concise and offer via `ask_question` to factor narrow path/tool guardrails into conditional `.agents/rules/*.rule.md` files.
- **Strict source truth**: **Only document what you actually observe.** Never invent commands, flags, directory roles, or conventions. If a category does not exist in this repository, omit it.

### 4. Write or Update `AGENTS.md`
- **Format**: Clear Markdown sections adapted to what actually exists in the repository (commands, architecture & data/control flow, code & testing conventions, gotchas & critical invariants).
- **Updating an existing `AGENTS.md`**: Read it in full first; refine outdated or incomplete technical sections while **strictly preserving all human-authored rules, custom instructions, and invariants**.
- **Portability**: Use relative Markdown links and portable paths only — never write machine-local absolute paths or file-scheme URIs.
