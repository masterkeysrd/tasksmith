# TaskSmith Agent Instructions

TaskSmith is a Go-based autonomous agent orchestrator and TUI application. This document serves as the primary guidance for agents working on this codebase.

## 🏗 Architecture Overview

- **`cmd/tasksmith`**: Entry point. Orchestrates flag loading, application initialization, and lifecycle management.
- **`internal/app`**: Core application logic and lifecycle (Run, Close, InitializeLogs). Registers builtin commands.
    - `flags`: Command-line flag parsing and options loading.
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
    - `highlight`: Global registry for semantic groups, Kite context propagation, and style caching.
    - `styles`: Mapping of colorscheme values to Kite styles.
    - `mode`: Reactive store for managing TUI input modes (Normal, Insert, Command) using `kites`.
    - `command`: Global registry and execution mechanism for TUI commands.

## 📜 Engineering Rules

1. **Logging**: Never use `fmt.Print` or standard `log`. Always use `internal/core/log`. Use `log.DefaultLogFilename()` for file naming.
2. **Paths**: Never hardcode paths. Always use `internal/core/xdg` for configuration, data, and cache locations.
3. **Documentation**: Every package under `internal` MUST have a `doc.go` file with package-level documentation.
4. **Testing**: All logic changes MUST be accompanied by unit tests. Every internal package should have a corresponding `_test.go` file.
5. **Types**: Favor explicit interfaces and composition. The `internal/api` package defines the public types used by the TUI.
6. **Error Handling**: Use structured error wrapping with `%w`.
7. **Global State**: Use the `kites` library for global, thread-safe state management outside the VDOM.

## 🖥 TUI Development

- **Framework**: Use the `kite` framework and `kitex` for components.
- **Data Fetching**: Use the `wind` package for reactive queries. Ensure `UseClient` is used to access the API service.
- **Styling**:
    - Use `highlight.Provider` at the root to propagate themes.
    - Use `highlight.Use(group)` in components to reactively consume styles.
    - Define groups using `highlight.Set(name, opts...)`.
- **Commands**:
    - Register global actions using `command.Register(id, fn)`.
    - Execute reactively in components using `command.UseCommand(id)`.
- **Input Modes**: Use `mode.Use()` to react to the current input state and `mode.Set()` to transition.

## 🛠 Tooling & Environment

- **Go Version**: 1.26.4
- **Dependencies**:
    - `github.com/masterkeysrd/warp`: Workspace and resource management.
    - `github.com/masterkeysrd/kite`: TUI framework and reactive hooks.
- **Build**: Use `go build ./cmd/tasksmith/...`.
- **Test**: Use `go test ./...`.
