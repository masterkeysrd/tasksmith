# TaskSmith Documentation Portal

Welcome to the TaskSmith Documentation Portal. TaskSmith is an agentic software development tool for the terminal, combining the keyboard-driven modal efficiency of Neovim with custom AI workflows and terminal UI integration.

## 🚀 Key Features

- **Keyboard-Driven Terminal UI (TUI)**: Fast modal interaction models (Normal, Insert) using our custom DOM-based TUI framework, **Kite**.
- **Model Presets & Providers**: Declarative integration with model providers like Anthropic (Claude), Google GenAI (Gemini), OpenAI, and offline local instances (Ollama).
- **Workspace Manifests (Warp)**: Standardized YAML configuration format for specifying project configurations, default model nodes, and tool access policies.
- **Cognitive Orchestration (Loom)**: LangGraph-style workflow orchestration tailored for terminal tasks and agent loop operations.
- **Granular Security Policies**: Workspace-level restrictions allowing developers to explicitly authorize or deny filesystem, network, system shell, LSP, and MCP tools.

## 📁 Project Structure

- **[docs/getting-started.md](getting-started.md)**: Steps to install, configure, and customize your first workspace.
- **[docs/architecture.md](architecture.md)**: Detailed breakdown of the internal Go packages and application lifecycle.
- **[docs/configuration.md](configuration.md)**: Comprehensive guide to Warp manifests, model provider files, and security policies.
- **[docs/tui-development.md](tui-development.md)**: Developer guide for building TUI views, using the Kite framework, registering commands, and configuring custom themes.
