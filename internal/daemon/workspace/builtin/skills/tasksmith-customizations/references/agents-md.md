# Authoring AGENTS.md Guidelines

`AGENTS.md` is the always-on project context file for coding agents working in a repository or mapped subproject. Because its rendered body enters prompt context on **every turn**, high signal density and progressive disclosure are essential.

---

## 1. Core Principles

### 1.1 High Signal, Zero Obvious Filler
LLM agents explore codebases dynamically with `ls`, `glob`, `grep`, and `view`. Mentioning obvious facts they pick up from reading a file or two (exhaustive directory trees, restating dependency manifests, or generic advice like "write clean, modular code") is actively detrimental: it burns context budget and dilutes attention away from real constraints.

Focus primarily on **non-obvious knowledge that prevents trial-and-error discovery**:
- Exact build, test, codegen, and lint invocations — especially required flags, environment variables, build tags, or how to run a single targeted test quickly.
- How subsystems fit together: entry points, package boundaries, ownership, and end-to-end control/data flow across files.
- Implicit conventions, error-handling idioms, concurrency/locking invariants, and testing patterns not self-evident from a single file.
- Sharp gotchas: generated files (`DO NOT EDIT` + the generator command), migrations, fixture quirks, sandbox/security boundaries, or doc-sync rules.

### 1.2 Progressive Disclosure Across Layers
Do not treat `AGENTS.md` as a dump for every specialized runbook in the repository:
- **Keep in `AGENTS.md`**: Universal commands, architecture map, cross-cutting conventions, and repository-wide invariants needed on most tasks.
- **Link instead of duplicating**: Reference deeper existing repository docs (`docs/architecture/...`, `CONTRIBUTING.md`) via relative Markdown links so agents read them only when working in that area.
- **Split narrow guardrails into `.agents/rules/*.rule.md`**: Path- or tool-specific rules (e.g., `**/*.sql`, `docs/**/*.md`, `internal/api/**`) belong in conditional `Rule` manifests (`triggers.globs` / `triggers.tools`) so they auto-inject only when matching files or tools are touched; see `rules.md`.
- **Split multi-step playbooks into `.agents/skills/<name>/SKILL.md`**: Release checklists, migration playbooks, or domain workflows with supporting scripts/references belong in on-demand `Skill` resources; see `skills.md`.

### 1.3 Strict Observation-Only Fidelity
- **Never invent commands, flags, patterns, or conventions.** Verify every command against actual `Makefile`, `package.json`, `go.mod`, `Cargo.toml`, `pyproject.toml`, taskfiles, or CI workflows, and verify architectural claims against real source files.
- If a category (e.g., deployment or codegen) does not exist in the repository, omit it completely rather than writing placeholders.
- **Portability**: Never write machine-local absolute home paths or file-scheme URIs; use repository-relative paths/links and portable variables (`~`, `$XDG_CONFIG_HOME`).

---

## 2. Recommended Content & Structure

Adapt section headings to what actually exists in the codebase rather than forcing empty boilerplate sections:

- **Architecture & Data/Control Flow**: Concise map of core subsystems, entry points, state/storage boundaries, and how requests, events, or data move across packages.
- **Essential Commands**: Verified commands for build, full tests, single-package/single-test execution, code generation, linting, and typechecking, noting non-obvious flags or prerequisites.
- **Conventions & Patterns**: Project-specific naming, error wrapping, logging, configuration, API/schema evolution, and testing idioms observed in representative source and test files.
- **Gotchas & Critical Invariants**: Generated files, ordering/concurrency hazards, security/credential rules, portability rules, and documentation-sync requirements.

---

## 3. Discovery & Initialization Workflow

When creating or updating `AGENTS.md` (including via `init`):

1. **Empty / Config-Only Guard**:
   - Check directory contents with `ls` / `glob` first. If the directory is empty or contains only bare config files with no source code yet, stop without creating `AGENTS.md` and ask the user to add source code first.
2. **Harvest Existing Agent & Editor Context**:
   - Check for existing instruction files (`AGENTS.md`, `CLAUDE.md`, `.cursor/rules/*.md`, `.cursor/rules/*.mdc`, `.cursorrules`, `.github/copilot-instructions.md`, `.windsurfrules`, `.agents/rules/*.rule.md`), reading only those that exist.
   - Incorporate relevant repo-wide rules; if existing editor rules carry narrow file globs, consider suggesting conditional `.agents/rules/*.rule.md` equivalents via `ask_question`.
3. **Inspect Configs, CI, & Representative Source Code**:
   - Read build/package manifests, task runners, linter configs, and CI workflows (`.github/workflows/`) to confirm what CI actually enforces.
   - Read representative entry points, core implementation files, and test files — never rely on config files and `README.md` alone, since READMEs may be aspirational or stale.
4. **Create vs. Update**:
   - **New `AGENTS.md`**: Write clear, grounded Markdown sections covering everything non-obvious an agent needs to work effectively.
   - **Existing `AGENTS.md`**: Read it thoroughly first, update stale/missing technical facts, and **strictly preserve all human-authored rules, constraints, and custom notes**.
5. **Monorepos & Restricted Workspaces**:
   - When `WORKSPACE.md` defines multiple `spec.projects` (or distinct subproject roots exist), use `ask_question` to confirm whether to author root `AGENTS.md`, subproject `<project>/AGENTS.md`, or both.
   - In restricted workspaces, inspect with read-only tools (`ls`, `glob`, `grep`, `view`) and propose the `AGENTS.md` change via `write` or `edit` for user confirmation.
