# TaskSmith

**An agentic software TUI development tool inspired by Neovim.**

TaskSmith is a terminal-based IDE that combines the keyboard-driven efficiency of Neovim with the power of AI agents. It allows developers to manage tasks, edit code, and automate workflows — all from within the terminal.

## Features

- **Neovim-inspired TUI** — Full keyboard navigation and modal editing for fast, hands-on-keyboard workflows.
- **Multi-provider AI support** — Seamlessly switch between Anthropic (Claude), Google (Gemini), OpenAI, and local models via Ollama.
- **Agentic task management** — Define, track, and automate development tasks with AI-driven assistance.
- **Configurable workspace system** — YAML-based workspace configuration with support for multiple model providers and custom model definitions.
- **Fully local mode** — Run entirely offline using local Ollama models for privacy-sensitive workflows.

## Tech Stack

| Component | Technology |
|---|---|
| Language | Go |
| TUI Framework | [Kite](https://github.com/masterkeysrd/kite) — DOM-based terminal UI (custom-built) |
| AI Orchestration | [Loom](https://github.com/masterkeysrd/loom) — LangGraph-style workflow framework for Go (custom-built) |
| Config Format | Warp v1alpha1 workspace manifests |

## Installation

```bash
go install github.com/masterkeysrd/tasksmith@latest
```

Or build from source:

```bash
git clone https://github.com/masterkeysrd/tasksmith.git
cd tasksmith
go build -o tasksmith
./tasksmith
```

## Usage

Start TaskSmith from your terminal:

```bash
tasksmith
```

Once inside, use Neovim-style keybindings to navigate, edit, and interact with AI agents. See the keybindings documentation for the full reference.

## Configuration

TaskSmith uses Warp v1alpha1 workspace manifests for configuration. The workspace file defines project scope, default AI provider, and available models.

### Workspace Configuration

Create a `WORKSPACE.md` (or `.warp`) file in your project root:

```yaml
---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: my-project
  description: My project workspace
spec:
  projects: ["."]
  defaultProvider: ollama
---
```

### Model Providers

Providers are configured as YAML files under `.agents/providers/`. Each provider defines available models, endpoints, and authentication:

| Provider | Models | Auth |
|---|---|---|
| **Anthropic** | Claude Sonnet 4.6, Claude Opus 4.7 | `ANTHROPIC_API_KEY` env var |
| **Google GenAI** | Gemini 3 Flash Preview, Gemini 3.1 Pro Preview | `GOOGLE_API_KEY` env var |
| **OpenAI** | GPT-4, GPT-4o, GPT-4.1 | `OPENAI_API_KEY` env var |
| **Ollama** | Qwen3.6, Llama4 Scout, Qwen3-Coder-Next | Local (no key needed) |

Example provider config (`.agents/providers/ollama.yaml`):

```yaml
apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: ollama
  description: Local Ollama provider
spec:
  type: ollama
  endpoint: http://localhost:11434
  defaultModel: ollama/qwen3.6:35b-a3b-coding-mxfp8
  models:
    - id: qwen3.6:35b-a3b-coding-mxfp8
      name: qwen3.6
      label: Qwen3.6
      limits:
        context: 32768
        output: 8192
```

### Environment Variables

Set provider API keys in an `env.sh` file or your shell profile:

```bash
export ANTHROPIC_API_KEY="your-key-here"
export GOOGLE_API_KEY="your-key-here"
export OPENAI_API_KEY="your-key-here"
```

## Project Structure

```
.
├── .agents/
│   └── providers/          # AI provider configurations
│       ├── anthropic.yaml
│       ├── genai.yaml
│       └── ollama.yaml
├── WORKSPACE.md            # Workspace manifest
├── env.sh                  # Environment variables
├── .gitignore
└── README.md
```

## Roadmap

- [ ] Core TUI with Neovim-style keybindings
- [ ] AI agent integration via Loom framework
- [ ] Task creation, tracking, and automation
- [ ] File and directory operations
- [ ] Plugin system for custom workflows
- [ ] Cross-platform support

## Contributing

Contributions are welcome! Please read the contributing guidelines before submitting pull requests.

## License

[Add your license here]

---

*TaskSmith — Code faster. Think less. Let the agent handle the rest.*
