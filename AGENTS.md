# TaskSmith Repository Guidelines

## Project Overview

TaskSmith is an extensible, daemon-backed AI coding agent for the terminal: OS-level sandboxing,
multi-provider model routing, and declarative version-controlled workspaces. It is a single Go
module (`github.com/masterkeysrd/tasksmith`, Go 1.26+) built as one binary (`cmd/tasksmith`) that
serves two roles:

- **Client**: interactive TUI/REPL plus one-shot commands (`internal/cli`, `internal/cmd`).
- **Daemon**: headless service on a Unix domain socket (`$XDG_RUNTIME_DIR/tasksmith/brain.sock`),
  running the agent harness, tool execution, provider gateway, and SQLite turn journal
  (`internal/daemon`). Client and daemon talk over an HTTP API with a multiplexed SSE event bus.

Terminal UI is built on the `trail`/`kitex` stack (`masterkeysrd/kite`), agent graphs on
`masterkeysrd/loom`, and workspace manifests on the WARP schema (`masterkeysrd/warp`). Windows is
unsupported: the daemon relies on POSIX process groups and `flock`.

## Build, Test & Lint Commands

Run from the repository root after the last edit. `.github/workflows/ci.yml` enforces gofmt, vet,
and tests on Linux and macOS, so anything below that fails locally fails CI.

| Purpose | Command |
| :--- | :--- |
| Build all packages | `go build ./...` |
| Build the binary | `go build -o tasksmith ./cmd/tasksmith` |
| Full test suite | `go test -count=1 ./...` |
| Targeted tests | `go test -count=1 ./internal/daemon/harness/...` |
| Apply standard fixes | `go fix ./...` |
| Format check | `gofmt -l .` (must print nothing; fix with `gofmt -w`) |
| Vet | `go vet ./...` |
| Lint (new findings only) | `golangci-lint run --new-from-rev=HEAD~ ./...` |
| Regenerate code | `go generate ./...` |

- `-count=1` is required so results are never served from the test cache.
- There is **no** `.golangci.yml`: the repo carries a large pre-existing lint baseline, so gate on
  **new** findings. Never widen lint config or add `nolint` comments to silence a finding you caused.
- `go generate ./...` runs `tools/toolgen` (see the `//go:generate` directive in
  `internal/daemon/tools/builtin/builtin.go`), which rewrites `generated.go` and the public tool
  schemas in `api/tools.go`. Regenerate whenever built-in tool definitions change, and commit the
  output.
- Documentation site (Rspress 2 + React 19, TypeScript) lives in `site/` and is separate from the Go
  toolchain: `npm ci`, then `npm run dev`, `npm run build`, `npm run preview`, or
  `npm run typecheck`, all with the working directory set to `site/`.

## Architecture Map

| Path | Responsibility |
| :--- | :--- |
| `cmd/tasksmith/` | Binary entry point; delegates to `internal/cmd`. |
| `api/` | Public client/daemon protocol and tool/event schemas (partly codegen output). |
| `client/` | Go client library for the daemon HTTP + SSE API, plus daemon lifecycle (`ConnectOrStart`, spawn helpers). |
| `internal/cmd/` | Cobra root command, flags, daemon start/stop lifecycle. |
| `internal/cli/` | TUI app, REPL, slash commands (`command/`), components, state stores. |
| `internal/exec/` | Headless one-shot and bidirectional NDJSON runs (`tasksmith exec`, `-p`); UI-free slash dispatcher and exit-code contract. |
| `internal/ui/`, `internal/tui/` | Reusable `trail` components, theme, formatting, render test utils. |
| `internal/daemon/` | Daemon subsystems: `server`, `broker` (SSE), `gateway` (providers), `harness` (agent graph), `session`, `journal` (SQLite), `timeline` (snapshots/revert), `tools` + `mcp`, `permissions`, `sandbox`/`sandboxcfg`, `workspace` (WARP loading, embedded built-ins). |
| `internal/core/` | Shared, dependency-light utilities: `guard` (path confinement), `shellguard` (shell AST classifier), `xdg` (portable paths), `diff`, `sqlite`, `vcs`, `strutils`. |
| `tools/toolgen/` | Code generator for built-in tool schemas. |
| `.agents/` | Repository WARP resources: `rules/`, `skills/`, `commands/`, `providers/`. |
| `docs/` | Public docs: `guides/` (workflows), `architecture/` (subsystem specs), `reference/` (schemas). |
| `site/` | GitHub Pages documentation site that renders `docs/`. |

`internal/` is off-limits to external importers; `api/` and `client/` are the stable surface other
Go programs consume.

## Code Style & Engineering Conventions

- Standard Go idioms: `gofmt` formatting, small packages with a `doc.go` stating package purpose,
  table-driven `_test.go` tests colocated with the code they cover.
- Errors are wrapped with context and propagated; never swallow an error to keep a path running.
- Reuse before authoring: `internal/ui/format` for numbers/durations/tokens and
  `internal/core/strutils` for truncation instead of hand-rolled helpers.
- Terminal UI work follows `.agents/rules/tasksmith-ui-conventions.rule.md` and the `tasksmith-tui`
  skill: `trail.FC`-style components, theme colors only via `theme.UseTheme()`, no `kitex.Render`
  outside tests, and a render test for every new or changed component.
- Security boundaries are invariants, not suggestions: filesystem access stays confined to the
  workspace root, and shell commands pass `shellguard` AST inspection before execution. Do not
  loosen path guards, sandbox capabilities, or permission defaults to make a test pass.

## Documentation & Workspace Resources

Documentation and embedded skills must be updated in the same change as the behavior they describe —
see [.agents/rules/docs-maintenance.rule.md](./.agents/rules/docs-maintenance.rule.md) and
[.agents/rules/builtin-skill-sync.rule.md](./.agents/rules/builtin-skill-sync.rule.md). Embedded
built-in skills live in `internal/daemon/workspace/builtin/skills/`; any file added or renamed there
must also be asserted in `internal/daemon/workspace/builtin_sync_test.go`.

Conditional guardrails belong in `.agents/rules/*.rule.md` (path-, keyword-, or tool-triggered), and
multi-step playbooks in `.agents/skills/<name>/SKILL.md` plus `references/*.md`. Keep this file
short and high-signal: it should state non-obvious repository-wide facts, not duplicate a rule.

## Critical Invariants

- **Zero Machine-Local Paths**: Never commit machine-local absolute home/system paths or file-scheme URIs in documentation, comments, examples, or tests. Always use relative Markdown links and portable paths (`~`, `$XDG_CONFIG_HOME/tasksmith/`, `$XDG_DATA_HOME/tasksmith/`, `$XDG_RUNTIME_DIR/tasksmith/`, `<workspace-root>/`).
- **Strict Source Truth**: Never invent CLI flags, tool parameters, or manifest fields; verify against actual Go structs, Cobra commands, and `.tool.md` / WARP definitions.
- **Progressive Disclosure & Sync**: Keep always-on instructions concise and non-obvious. When altering user-facing behavior, CLI flags, built-in tools, WARP schemas, or subsystem architecture, update `docs/` and embedded skills in the same change per [.agents/rules/docs-maintenance.rule.md](./.agents/rules/docs-maintenance.rule.md) and [.agents/rules/builtin-skill-sync.rule.md](./.agents/rules/builtin-skill-sync.rule.md) (do not churn `docs/` for internal refactors, bug fixes, or test-only changes).
- **No Secrets In Repo**: API keys live in the gitignored `env.sh` or the host environment; never paste credentials into tracked code, manifests, tests, fixtures, or commit messages, and never stage a secrets file.
- **Go Verification Gate**: Changes to `.go` files, `go.mod`, or `go.sum` must pass the [Go verification gate](./.agents/rules/go-verification-gate.rule.md). Documentation-only and other non-Go changes do not require it.
