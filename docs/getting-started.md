# Getting Started with TaskSmith

This guide details how to install TaskSmith, configure your first workspace using the interactive setup wizard, and get the environment ready for coding.

## 📥 Installation

Ensure you have **Go 1.26.4 or later** installed, then install the binary:

```bash
go install github.com/masterkeysrd/tasksmith@latest
```

Alternatively, build from source inside the repository:

```bash
go build -o tasksmith ./cmd/tasksmith
```

## ⚙️ Interactive Wizard Setup

When you start `tasksmith` in an unconfigured directory, it detects the absence of workspace manifests and launches the interactive **Console Setup Wizard**:

### Step 1: Welcome Screen
A brief introduction outlining the customization sequence. You can skip the wizard to run in **ad-hoc mode** (all configurations are kept in memory and no files are written to disk).

### Step 2: Configure Model Providers
Choose your primary reasoning provider node:
- **Anthropic** (e.g. Claude Sonnet/Opus)
- **Google GenAI** (e.g. Gemini Pro/Flash)
- **OpenAI** (e.g. GPT-4o, o1)
- **Ollama** (e.g. local llama3, mistral, gemma)

Customize the base endpoint URL, specify a custom default model identifier, or click preset buttons to auto-fill recommended models. Fill in your authentication API secret (if applicable).

### Step 3: Configure Sandbox Tools
Review and toggle the local capabilities you authorize the AI agent to execute. Tools are grouped into categories:
- **filesystem**: directory listing, reading, editing, writing files.
- **network**: web fetching, page scraping, search queries.
- **system**: terminal command execution (`bash`).
- **lsp**: Language Server Protocol diagnostic extraction and searches.
- **mcp**: Model Context Protocol resource reading.

You can toggle individual checkbox permissions or click ** ENABLE ALL** for complete automation scope.

### Step 4: Confirm Boundary Configurations
Review your configuration options:
- Selected workspace project name.
- Configured default model router.
- Number of active authorized tools.

Click **[ CONFIRM & TRUST WORKSPACE ]** to finalize the setup.

## 📂 Generated Files

Once setup is confirmed, TaskSmith generates the following configuration files:

1. **`WORKSPACE.md`** (Workspace Root)
   Declares the project root path, default model provider, and security policies (authorized tools inclusion list).
2. **`.agents/providers/<provider>.yaml`** (Workspace Root)
   Stores endpoint parameters, default model routers, and schemas.
3. **`.env`** (Workspace Root)
   Stores secret credentials (e.g., `OPENAI_API_KEY`) to keep them out of git-tracked files.
4. **`.gitignore`** (Workspace Root)
   Automatically ignores `.env` files to prevent credentials leakage.
5. **`setup.json`** (XDG Workspaces Directory)
   Stores setup timestamps and versions to enable system migrations.
6. **`tasksmith.config.json`** (XDG Global Config Directory)
   Persists global preferences such as active color themes.
