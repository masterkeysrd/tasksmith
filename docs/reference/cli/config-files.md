# CLI Configuration and State Files

User settings follow the XDG base directories. Workspace policies and session records are separate from terminal appearance settings.

## State, Log, and Socket Locations

TaskSmith adheres to the XDG Base Directory Specification for state persistence, logs, and configuration:

| Category | Path | Purpose | Persistence |
| :--- | :--- | :--- | :--- |
| **Global Config** | `~/.config/tasksmith/` | Root directory for machine-wide user configuration (`$XDG_CONFIG_HOME`). | Permanent |
| **Global Secrets** | `~/.config/tasksmith/.env` | Global fallback environment variables and upstream API credentials. | Permanent |
| **Global Providers** | `~/.config/tasksmith/agents/providers/*.yaml` | User-tier model provider manifests available across all workspaces. | Permanent |
| **Global Agents** | `~/.config/tasksmith/agents/agents/*.agent.md` | User-tier custom agent personas and instructions. | Permanent |
| **Global Rules** | `~/.config/tasksmith/permissions.json` | Persistent machine-wide permission allow and deny rules. | Permanent |
| **Provider Credentials** | `$XDG_DATA_HOME/tasksmith/credentials/` | OAuth 2.0 PKCE credentials with restricted permissions (`0600`). | Permanent |
| **Trust Store** | `~/.local/share/tasksmith/trust.json` | Machine-wide trusted directories registry, trust scopes (`folder` / `parent`), and executable fingerprints (`$XDG_DATA_HOME`). | Permanent |
| **Workspace Metadata** | `~/.local/share/tasksmith/workspaces/<hash>/workspace.json` | Workspace schema version and binary version for automated upgrade migrations. | Permanent |
| **Workspace DB** | `~/.local/share/tasksmith/workspaces/<hash>/tasksmith.db` | SQLite database storing turn journals, messages, and session state (`$XDG_DATA_HOME`). | Permanent |
| **Project Rules** | `~/.local/share/tasksmith/workspaces/<hash>/permissions.json` | Project-scoped persistent permission allow and deny rules. | Permanent |
| **Runtime Socket** | `$XDG_RUNTIME_DIR/tasksmith/brain.sock` | Active Unix domain IPC socket used by CLI and daemon. | Ephemeral |
| **Fallback Socket** | `~/.local/state/tasksmith/run/brain.sock` | Socket location when `XDG_RUNTIME_DIR` is unset (runtime home resolves under `$XDG_STATE_HOME`). | Ephemeral |
| **Process Lock** | `$XDG_RUNTIME_DIR/tasksmith/brain.lock` | File lock ensuring a single background daemon runs per user. | Ephemeral |
| **Daemon Logs** | `~/.local/state/tasksmith/logs/daemon.jsonl` | Structured JSON Lines log file with daily rolling rotation (`$XDG_STATE_HOME`). | Permanent |
| **Workspace Root** | `<workspace-root>/WORKSPACE.md` | Workspace declaration, project boundaries, defaults, and security policies. | Permanent |
| **Workspace Env** | `<workspace-root>/.env` | Workspace-scoped secrets and environment variables. | Permanent |
| **Workspace Agents** | `<workspace-root>/.agents/` | Workspace-tier agents, skills, commands, decisions, and tool manifests. | Permanent |

## Status Line Fields

Edit `~/.config/tasksmith/statusline.json` to configure the status line on the next REPL start, or use `/statusline` to preview changes live. The default file is:

```json
{
  "rows": [
    ["hint", "mode", "model", "effort", "warnings"],
    ["context", "cost", "tokens"]
  ],
  "compact": false
}
```

| Field | Type | Behavior |
| :--- | :--- | :--- |
| `rows` | Array of up to two ordered arrays of segment IDs | The first configured segment in each row is left-aligned; the rest are grouped on the right. The second row separates its right-hand segments with `·`. Rows with no visible segments are omitted. |
| `compact` | Boolean | Defaults to `false`. Uses shorter segment text where available without changing row assignments. |

| Segment ID | Default text | Compact text |
| :--- | :--- | :--- |
| `hint` | `Shift+Tab mode · /help`, or the Ctrl+C cancel prompt. | Unchanged. |
| `mode` | Permission mode badge, e.g. `[accept edits]`. | `[edits]` for accept edits; other modes are unchanged. |
| `model` | Active model with its provider when known, e.g. `Gemini 4 Neon (antigravity)`. | Model only; also used when space is tight. |
| `effort` | Thinking setting, or `default effort` when the provider chooses. | Unchanged. |
| `warnings` | Active workspace and MCP warning count (`⚠ N warnings`); hidden at zero. | `⚠ N` |
| `context` | Context window usage, e.g. `Context 109.5k/400.0k (27%)`. | `27%` |
| `cost` | Session cost, e.g. `Cost $0.42`. A leading `≥` means some calls had no pricing data. For subscription-billed models, shows remaining quota with a reset countdown when available, e.g. `Quota 94% (↻ 3h40m)`. | Amount or remaining quota, without the label or countdown. |
| `tokens` | Session input and output tokens, e.g. `Tokens ↑128.4k ↓8.3k`. Input includes cached tokens. | `↑128.4k ↓8.3k` |

IDs are trimmed and matched case-insensitively. Unknown IDs, duplicates after the first occurrence, and rows beyond the second are dropped and reported. If no valid segments remain, the default rows are used. The former `segments` field and `usage` ID are not supported; use `rows` with separate `cost` and `tokens` IDs instead.

## Theme preference (`theme.json`)

`$XDG_CONFIG_HOME/tasksmith/theme.json` is optional and read at startup. Use `/theme [name]` to select a built-in or custom theme for the running REPL. Custom theme definitions live in `$XDG_CONFIG_HOME/tasksmith/themes/<name>.json`, not in the preference file.

| Field | Type | Default | Effect |
| :--- | :--- | :--- | :--- |
| `theme` | string | `dark` | Startup built-in theme name; unknown names fall back to `dark`. |
| `transparent` | boolean | `false` | Let the terminal background show through canvas, panel, hover, and active surfaces. |

## Sandbox policy files

| Scope | Path | Editor |
| :--- | :--- | :--- |
| Global | `$XDG_CONFIG_HOME/tasksmith/sandbox.json` | `/sandbox` → Global |
| Project | `$XDG_DATA_HOME/tasksmith/workspaces/<hash>/sandbox.json` | `/sandbox` → Project |

## See Also

- [CLI reference index](../cli.md)
- [Appearance guide](../../guides/appearance.md)
- [Permissions guide](../../guides/permissions.md)
- [Sandbox guide](../../guides/sandbox.md)
- [Trust guide](../../guides/trust.md)
