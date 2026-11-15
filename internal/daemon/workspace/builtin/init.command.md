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
    - view
    - glob
    - grep
    - ask_question
    - write
    - edit
---

Inspect the current repository to discover codebase patterns, tech stack, testing workflows, and engineering standards, and generate or update `AGENTS.md` following the guidelines in the `tasksmith-customizations` skill (`references/agents-md.md`).

### Procedure:
1. **Inspection**:
   - Use `glob` and `view` to inspect repository structure and key configuration files (e.g. `go.mod`, `package.json`, `Cargo.toml`, `pyproject.toml`, `Makefile`, `README.md`, CI workflows in `.github/workflows/`).
   - Identify the primary languages, frameworks, entry points, test runners, build commands, and linters.
2. **Clarification**:
   - If commands or standards are ambiguous (e.g., multiple test scripts or complex build targets), ask the user using `ask_question`.
3. **Creation or Update**:
   - If `AGENTS.md` does not exist: create a comprehensive `AGENTS.md` file with Project Overview, Commands (build, test, lint), Architecture, Code Style, and Critical Rules.
   - If `AGENTS.md` already exists: read it thoroughly. Merge newly discovered facts while strictly preserving user-authored rules, custom instructions, and existing conventions.
   - If in a monorepo with multiple project directories: ask the user whether to write to root `AGENTS.md` and/or subproject `AGENTS.md`.
4. **Writing**:
   - Write or update `AGENTS.md` using `write` or `edit`. Ensure clean Markdown formatting and zero machine-local absolute paths.
