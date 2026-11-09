# Authoring AGENTS.md Guidelines

`AGENTS.md` is the primary contextual specification file for coding agents working in a repository or subproject. It informs agents about project structure, build/test toolchains, coding conventions, architectural invariants, and project-specific rules.

---

## 1. Structure of `AGENTS.md`

A well-structured `AGENTS.md` file contains the following core sections:

### 1.1 Project Overview & Architecture
- Brief statement of the project purpose, architecture, and design patterns.
- High-level component interactions, core subsystems, and key directories.

### 1.2 Build, Test, & Lint Commands
Crisp tables or lists of commands agents must run to verify their changes:
- Build command (e.g. `go build ./...`, `npm run build`, `cargo build`).
- Unit test command (e.g. `go test ./...`, `pytest`, `npm test`).
- Targeted / fast test runner options.
- Linter / typecheck commands (e.g. `golangci-lint run`, `npm run lint`, `tsc --noEmit`).

### 1.3 Code Style & Engineering Conventions
- Language-specific formatting and idioms.
- Error handling patterns, naming rules, and logging conventions.
- Package layout or module organization standards.

### 1.4 Critical Rules & Invariants
- Zero machine-local paths: never commit `/Users/...`, `/home/...`, or system-specific paths.
- Security constraints: never write credentials, tokens, or plaintext secrets into code or manifests.
- Documentation synchronization: rules for updating docs when modifying CLI flags, schemas, or public APIs.

---

## 2. `/init` Execution Behavior

When invoked via `/init`, the agent must adhere to the following rules:

### 2.1 Inspection
1. Scan project roots using `glob` and `view`:
   - Inspect build files (`go.mod`, `package.json`, `Cargo.toml`, `pyproject.toml`, `Makefile`, `CMakeLists.txt`).
   - Inspect CI workflows (`.github/workflows/*.yaml`).
   - Inspect existing `README.md` or design documents for high-level architecture.
2. If ambiguous options exist (for example, whether to use `make test` vs `go test ./...`), ask the user using the `ask_question` tool instead of guessing.

### 2.2 Create vs. Update
- **If `AGENTS.md` does not exist**:
  - Author a clean, organized `AGENTS.md` file from scratch based on observed files and facts.
- **If `AGENTS.md` already exists**:
  - Read existing `AGENTS.md` before making any edits.
  - Merge newly detected commands or facts into existing sections.
  - **Preserve all existing human-authored rules, custom instructions, and notes.** Never overwrite or discard custom user rules.

### 2.3 Monorepos
- If the workspace defines multiple subprojects in `WORKSPACE.md` (`spec.projects`), ask the user using `ask_question` whether they want to generate/update:
  1. The root repository `AGENTS.md`.
  2. A subproject-specific `<project>/AGENTS.md`.
  3. Both.

### 2.4 Restricted Mode
- If running in a restricted workspace, `/init` is fully allowed: the agent uses read-only tools (`view`, `glob`, `grep`), and proposes a confirmed write to `AGENTS.md` via `write` or `edit`.
