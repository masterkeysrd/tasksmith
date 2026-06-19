# Configuration Guide

TaskSmith configurations are written using the **Workspace Agent Resource Protocol (WARP)** — a provider-agnostic, declarative format for defining workspace scopes, reasoning model providers, custom tools, and security constraints.

---

## 1. Workspace Manifest (`WORKSPACE.md`)

The `WORKSPACE.md` file defines the root configuration of the workspace directory. It declares active projects, default cognitive model configurations, and boundary policies.

### Example Manifest

```yaml
---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: untrusted-local-repo
spec:
  projects:
    - .
  defaultProvider: openai
  policies:
    tools:
      include:
        - bash
        - edit
        - grep
        - ls
        - view
        - write
---
```

### Manifest Specifications

- **`metadata.name`**: Unique label identifying this workspace.
- **`spec.projects`**: Path scopes to treat as workspace project directories.
- **`spec.defaultProvider`**: The default reasoning provider nodes referenced by agents.
- **`spec.policies.tools.include`**: Allowed tools inclusion whitelist. The cognitive agents will only be permitted to execute tools that appear on this list.

---

## 2. Model Provider (`.agents/providers/`)

Providers describe available LLM instances. Each provider is configured as a YAML file named `<provider-name>.yaml` under the `.agents/providers/` directory.

### Example Provider Config (`openai.yaml`)

```yaml
apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: openai
spec:
  type: openai
  endpoint: https://api.openai.com/v1
  defaultModel: gpt-4o
  auth:
    type: env
    env: OPENAI_API_KEY
  models:
    - id: gpt-4o
      name: gpt-4o
      label: GPT-4o
      limits:
        context: 128000
        output: 4096
```

### Provider Specifications

- **`spec.type`**: Reasoning engine type (e.g. `openai`, `anthropic`, `google-genai`, `ollama`).
- **`spec.endpoint`**: Base URL of the API.
- **`spec.auth.env`**: Name of the environment variable containing the secret API key.
- **`spec.models`**: Array of available model profiles with their token and context limits.

---

## 3. Local Environment Secrets (`.env`)

Secrets are written to a `.env` file at the workspace root to ensure credentials stay secure. When launching TaskSmith, keys are automatically resolved and injected:

```env
OPENAI_API_KEY=sk-proj-...
```

TaskSmith setup appends `.env` to `.gitignore` to ensure these keys are never committed to version control.

---

## 4. Global Configuration (`~/.config/tasksmith/`)

Global user configurations are stored under the user's home configuration directory:

### Theme Configuration (`theme.json`)
Saves the currently active color scheme name:
```json
{
  "theme": "tokyo-night"
}
```

### Status Line Configuration (`statusline.json`)
Defines the layout of the status line at the bottom of the shell. It supports built-in indicators and custom shell command executors controlled by a background scheduler:
```json
{
  "statusline": {
    "left": [
      { "type": "builtin", "name": "mode" },
      { "type": "builtin", "name": "git_branch" }
    ],
    "right": [
      {
        "type": "command",
        "exec": "date +%H:%M",
        "interval": "1m"
      },
      { "type": "builtin", "name": "stats" },
      { "type": "builtin", "name": "status" }
    ]
  }
}
```

#### Config Specifications:
* **`type: "builtin"`**: Predefined UI indicators.
  * `mode`: Shows current mode (Normal, Insert, Command).
  * `git_branch`: Shows active Git branch name.
  * `provider`: Shows default model provider.
  * `model`: Shows active model and thinking effort.
  * `agent`: Shows active orchestrating agent.
  * `stats`: Shows token metrics and cost metrics.
  * `status`: Shows system execution loop status.
* **`type: "command"`**: Runs custom script commands on a background interval timer.
  * `exec`: Shell command to run (spawned under `/bin/sh -c`).
  * `interval`: Go duration string specifying how often the command executes (e.g. `10s`, `1m`, `5m`).

