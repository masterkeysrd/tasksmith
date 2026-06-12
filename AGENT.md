# TaskSmith Agent Instructions

TaskSmith is a Go-based autonomous agent orchestrator and TUI application. This document serves as the primary guidance for agents working on this codebase.

## 🏗 Architecture Overview

- **`cmd/tasksmith`**: Entry point. Orchestrates flag loading, application initialization, and lifecycle management.
- **`internal/app`**: Core application logic and lifecycle (Run, Close, InitializeLogs).
- **`internal/api`**: Public service interface and data mapping. Connects workspace resources to application-level types.
- **`internal/core`**: Essential utilities:
    - `log`: Structured `slog` wrapper with level support and custom writers.
    - `xdg`: XDG Base Directory Specification compliance.
    - `fsutil`: File system helpers.
- **`internal/workspace`**: Management of agents, projects, and providers using the `warp` library.
- **`internal/tui`**: Terminal User Interface built with the `kite` framework.
    - `api`: TUI-specific API client context.
    - `queries`: Reactive data hooks using `wind`.
    - `colorscheme`: Management of color themes and palettes.
    - `highlight`: Global registry for highlight groups and style caching.
    - `styles`: Mapping of colorscheme values to Kite styles.
    - `mode`: Reactive store for managing TUI input modes.

## 📜 Engineering Rules

1. **Logging**: Never use `fmt.Print` or standard `log`. Always use `internal/core/log`.
2. **Paths**: Never hardcode paths. Always use `internal/core/xdg` for configuration, data, and cache locations.
3. **Documentation**: Every package under `internal` MUST have a `doc.go` file with package-level documentation.
4. **Testing**: All logic changes MUST be accompanied by unit tests. Every internal package should have a corresponding `_test.go` file.
5. **Types**: Favor explicit interfaces and composition. The `api` package defines the public types used by the TUI and other external consumers.
6. **Error Handling**: Use structured error wrapping with `%w`.

## 🖥 TUI Development

- **Framework**: Use the `kite` framework.
- **Data Fetching**: Use the `wind` package for reactive queries. Ensure `UseClient` is used to access the API service.
- **State Management**: Prefer the reactive patterns provided by `kite` and `wind`.

## 🛠 Tooling & Environment

- **Go Version**: 1.26.4
- **Dependencies**:
    - `github.com/masterkeysrd/warp`: Workspace and resource management.
    - `github.com/masterkeysrd/kite`: TUI framework.
- **Build**: Use `go build ./cmd/tasksmith/...`.
- **Test**: Use `go test ./...`.
