# TaskSmith Agent Instructions

TaskSmith is a Go-based autonomous agent orchestrator and TUI application. This document serves as the primary guidance for agents working on this codebase.

## 🏗 Architecture Overview

- **`cmd/tasksmith/`**: Entry point. Orchestrates flag loading, application initialization, and lifecycle management.
- **`cmd/stage/`**: Staging binary entry point (separate build target).
- **`internal/app`**: Core application logic and lifecycle (Run, Close, InitializeLogs). Registers builtin commands.
      - `flags/`: Command-line flag parsing and options loading.
      - `keymap.go`: Root-level keybinding configuration.
- **`internal/api/`**: Public service interface and data mapping. Connects workspace resources to application-level types.
- **`internal/session/`**: Session management with SQLite-backed persistence.
      - `storage.go`: Session storage interfaces.
      - `sqlite.go`: SQLite persistence implementation.
      - `store.go`: Session store coordination.
- **`internal/agent/`**: Builtin agent resources, tools, and graph-based orchestration.
      - `tools/`: Definitions and discovery for builtin tool presets. Contains `types.go` generated from markdown tool specs.
      - `graph/`: Agent execution graph with hooks and rehydration support.
      - `prompt/`: Prompt formatting and management utilities.
- **`internal/workspace/`**: Management of agents, projects, and providers using the `warp` library.
      - `builtin/`: Builtin workspace resources.
      - `preset/`: Provider presets (OpenAI, Anthropic, Google GenAI, Ollama).
- **`internal/core`**: Essential utilities and domain packages:
      - `log/`: Structured `slog` wrapper with level support and custom writers.
      - `xdg/`: XDG Base Directory Specification compliance.
      - `fsutil/`: File system helpers.
      - `db/`: Database utilities.
      - `diff/`: Diff computation and display helpers.
      - `fs/`: File system operations with glob support and `.gitignore`-aware ignore patterns.
      - `process/`: Cross-platform process management (unix, windows implementations).
      - `ripgrep/`: Ripgrep integration for fast text search.
      - `scheduler/`: Task scheduling utilities.
      - `vcs/`: Version control system helpers (Git).
- **`internal/tui/`**: Terminal User Interface built with the `kite` framework.
      - `api/`: TUI-specific API client context.
      - `plugin/`: Plugin system for extending TUI functionality.
      - `queries/`: Reactive data hooks using `wind`.
      - `theme/`: Dynamic theme styling, loading, and resolution.
      - `mode/`: Reactive store for managing TUI input modes (Normal, Insert, Command) using `kite`'s reactive primitives.
      - `command/`: Global registry and execution mechanism for TUI commands.
      - `keymap/`: Mode-aware keybinding system with sequence resolution.
      - `shell/`: Shell-level TUI components.
          - `commandbar/`: Command input bar.
          - `sidebar/`: Sidebar navigation and listing.
          - `statusline/`: Status line display.
          - `titlebar/`: Window title bar.
      - `views/`: Top-level TUI views.
          - `chat/`: Chat view for agent interactions.
          - `setup/`: Setup/onboarding view.
          - `welcome/`: Welcome/landing view.
      - `components/`: Reusable UI components (accordion, alert, badge, button, card, checkbox, codeblock, confirm_dialog, diffblock, input, markdown, modal, paper, tabs, etc.).
          - `icon/`: Icon definitions.
- **`tools/warp-gen/`**: Code generator that parses tool specs to produce `types.go` in `internal/agent/tools/`.

## 📜 Engineering Rules

1. **Logging**: Never use `fmt.Print` or standard `log`. Always use `internal/core/log`. Use `log.DefaultLogFilename()` for file naming.
2. **Paths**: Never hardcode paths. Always use `internal/core/xdg` for configuration, data, and cache locations.
3. **Documentation**: Every package under `internal` MUST have a `doc.go` file with package-level documentation.
4. **Testing**: All logic changes MUST be accompanied by unit tests. Every internal package should have a corresponding `_test.go` file.
5. **Types**: Favor explicit interfaces and composition. The `internal/api` package defines the public types used by the TUI.
6. **Error Handling**: Use structured error wrapping with `%w`.
7. **Global State**: Use the `kite` library for global, thread-safe state management outside the VDOM.
8. **Tool Specs**: Builtin agent tools are defined as markdown files in `internal/agent/tools/`. The front-matter defines the schemas (`parameters` and `outputSchema`), while the description must be in the markdown body. Run `go run ./tools/warp-gen` to update `types.go` after changes.
9. **No Reflection**: Avoid using `reflect` to handle `warp` spec objects. Bind directly to the structs defined by the local `warp` library.
10. **File Tracking & Reverting**: All filesystem modifications (`write`, `edit`, `multi_edit`, `remove`) must be recorded using the session-scoped `FileTracker.Record()`. Reverting a file must use a three-way merge via `FileTracker.RevertToBaseline()` to preserve non-conflicting manual user modifications, only blocking (returning `"conflict"`) on overlapping line-level changes.
11. **Knowledge Validation**: To prevent blind or stale edits/deletions, the agent tools (`edit`, `multi_edit`, `remove`, and `write` on existing files) must check `FileTracker.IsKnown()` to ensure the file has not been modified externally since it was last viewed or written. The `view` tool must call `FileTracker.RecordRead()` to register the file's current hash in the session resources.

## 🖥 TUI Development

- **Framework**: Use the `kite` framework and `kitex` for components.
- **Data Fetching**: Use the `wind` package for reactive queries. Ensure `UseClient` is used to access the API service.
- **Styling**:
      - Use `theme.Provider` at the root to propagate themes.
      - Components should consume colors reactively using `theme.UseTheme()` to access the active color scheme (`t.Color.*` and `t.Palette`).
      - For custom default and hover styling, components (such as buttons) should use semantic theme-aware styles or the component's `HoverStyle` prop to handle states dynamically rather than embedding static local wrappers.
- **Commands**:
      - Register global actions using `command.Register(id, fn)`.
      - Execute reactively in components using `command.UseCommand(id)` or dynamically via `command.Execute(ctx, id, args...)`.
      - Note: Registered TUI commands do not include the leading `:` prefix; strip it (e.g. using `strings.TrimPrefix`) before dynamic execution.
- **Input Modes**: Use `mode.Use()` to react to the current input state and `mode.Set()` to transition.
- **Component Conventions**:
      - Use `kitex.FC` for standard components and `kitex.FCC` for components that accept children.
      - Define a `Props` struct for every component (e.g., `PaperProps`).
      - Naming: Use `Style` suffix for style variables (e.g., `NormalStyle`).
      - Declare `style.Style` variables at the package level instead of hardcoding them within component render functions to improve readability.
      - Components should accept a `style.Style` prop for layout and visual overrides, merged at the end of the render function.
      - Favor pure style overrides over specific layout props (like padding/margin) to maintain API simplicity.

## 🛠 Tooling & Environment

- **Go Version**: 1.26.4
- **Dependencies**:
      - `github.com/masterkeysrd/warp`: Workspace and resource management.
      - `github.com/masterkeysrd/kite`: TUI framework and reactive hooks.
      - `github.com/masterkeysrd/loom`: Local library (replace directive).
      - `github.com/go-git/go-git/v5`: Git operations.
      - `github.com/jmoiron/sqlx`: Database access.
      - `modernc.org/sqlite`: SQLite database.
      - `github.com/yuin/goldmark`: Markdown processing.
      - `github.com/alecthomas/chroma/v2`: Syntax highlighting.
      - `github.com/PuerkitoBio/goquery`: HTML parsing.
      - `github.com/JohannesKaufmann/html-to-markdown`: HTML to Markdown conversion.
      - `github.com/anthropics/anthropic-sdk-go`: Anthropic API client.
      - `github.com/openai/openai-go/v3`: OpenAI API client.
      - `github.com/ollama/ollama`: Ollama API client.
      - `github.com/google/uuid`: UUID generation.
- **Build**: Use `go build ./cmd/tasksmith/...`.
- **Test**: Use `go test ./...`.
- **Warp Code Generator**: Run `go run ./tools/warp-gen` to compile tool markdown specs into Go structures.
