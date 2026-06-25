---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
    name: conventional-commits
    description: "Conventional Commits specification, commit message formatting rules, and best practices for structured version control history."
---

# Conventional Commits Skill

This skill defines the **Conventional Commits** specification for structuring commit messages to enable automated changelogs, versioning, and clear project history.

## 1. The Commit Message Format

Every commit message follows this structured format:

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

- **`type`**: Required. Describes the category of change (see [Types](#2-supported-types)).
- **`scope`**: Optional. A noun referring to a section of the codebase (e.g., `api`, `tui`, `agent`).
- **`description`**: Required. A concise summary of the change in imperative mood.
- **`body`**: Optional. A detailed explanation, separated from the subject by a blank line.
- **`footer`**: Optional. Metadata such as breaking changes or issue references.

### Rules

1. **Separate subject from body with a blank line.**
2. **Do not capitalize** the subject line.
3. **Do not end** the subject line with a period.
4. **Use the imperative mood** in the subject line ("Fix bug" not "Fixed bug").
5. **Limit the subject line** to 72 characters.
6. **Wrap the body** at 72 characters.
7. **Reference issues and pull requests** in the footer.

## 2. Supported Types

The following types are recommended. Choose the one that best describes the primary change:

| Type         | Description                                                                                   |
|-------------|-----------------------------------------------------------------------------------------------|
| `feat`      | A new feature.                                                                                |
| `fix`       | A bug fix.                                                                                  |
| `chore`     | Maintenance tasks, dependency updates, tooling changes. No production code change.            |
| `docs`      | Documentation-only changes.                                                                   |
| `style`     | Code style changes (formatting, semicolons, whitespace). No logic change.                     |
| `refactor`  | A code change that neither fixes a bug nor adds a feature.                                   |
| `test`      | Adding missing tests or correcting existing tests.                                            |
| `ci`        | Changes to CI configuration files and scripts.                                                |
| `build`     | Changes that affect build system, dependencies, or release procedures.                        |
| `perf`      | A code change that improves performance.                                                      |
| `revert`    | Reverts a previous commit.                                                                    |
| `release`   | A version release tag.                                                                        |

## 3. Scope (Optional)

The scope should be a noun indicating the component affected. Use one of the following conventions:

- **Package names**: `api`, `agent`, `tui`, `session`, `workspace`, `tools`
- **File paths**: `internal/core/log`, `cmd/tasksmith`
- **Modules**: `kite`, `warp`, `loom`

Examples:
```
feat(tui): add mode-aware keybinding system
fix(api): resolve null pointer in session lookup
docs(warp): update specification for MCP resources
```

## 4. Breaking Changes

Indicate breaking changes with a `!` after the type/scope, or by including a `BREAKING CHANGE` footer:

```
feat(api)!: rename SessionStore to SessionManager

The old SessionStore name conflicts with the database layer.
All callers must migrate to the new interface.

BREAKING CHANGE: SessionStore has been renamed to SessionManager.
Update all imports and usages accordingly.
```

The `!` before the `:` signals a breaking change. The footer provides additional context.

## 5. Footer Conventions

The footer can contain:

- **Breaking change declarations**: `BREAKING CHANGE: <description>`
- **Issue references**: `Closes #123`, `Fixes #456`, `Refs #789`
- **Pull request references**: `Pull request: #100`
- **Signed-off-by**: `Signed-off-by: Your Name <email>`

Example:
```
refactor(tui): extract button rendering to dedicated component

Improve readability by separating layout from rendering logic.

Closes #42
```

## 6. Examples

### Simple feature commit
```
feat(kite): add reactive query hooks for TUI components

Introduce UseQuery and UseWatch to enable reactive data fetching
in kite-based components.
```

### Bug fix with body
```
fix(session): prevent SQLite deadlock on concurrent writes

The previous implementation held locks across goroutine boundaries.
Now uses a single goroutine to serialize all database operations.

Fixes #89
```

### Chore with scope
```
chore(deps): update github.com/go-git/go-git/v5 to v5.13.0

Align with the latest stable release for improved performance
and bug fixes in the packager.
```

### Breaking change
```
refactor(workspace)!: restructure builtin resource loading

Resources are now loaded from .agents/ instead of .tasksmith/.
All workspace manifests must be relocated accordingly.

BREAKING CHANGE: .tasksmith/ directory has been renamed to .agents/.
```

## 7. Automated Versioning

Conventional Commits enable automated semantic versioning:

- **`feat`** → minor version bump (`0.1.0` → `0.2.0`)
- **`fix`** → patch version bump (`0.1.0` → `0.1.1`)
- **`BREAKING CHANGE`** → major version bump (`0.1.0` → `1.0.0`)

## 8. Best Practices

1. **One change per commit**: Each commit should represent a single logical change.
2. **Write meaningful bodies**: The body should explain *why*, not *what*.
3. **Use scopes consistently**: Pick a convention and stick to it across the project.
4. **Reference related work**: Always link to issues, PRs, or design docs in the footer.
5. **Use `chore` sparingly**: Reserve it for truly trivial maintenance (e.g., whitespace, typos in comments).

## 9. Resources

- [Conventional Commits Specification](https://www.conventionalcommits.org/)
- [Angular Commit Guidelines](https://github.com/angular/angular/blob/main/CONTRIBUTING.md#commit)
- [Semantic Versioning](https://semver.org/)
