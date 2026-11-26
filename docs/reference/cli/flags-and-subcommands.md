# CLI Flags and Subcommands

Use these commands from the shell. Session flags select which conversation to open; daemon flags control the background process.

## Root Invocation: `tasksmith`

Invoking `tasksmith` without a subcommand automatically starts the interactive terminal REPL attached to the Brain daemon:

```bash
tasksmith [flags]
```

With `-p` / `--print`, the root command runs a single headless turn instead of the REPL — identical to `tasksmith exec`:

```bash
tasksmith -p "summarize this repo"
git diff | tasksmith -p "review this diff"
```

## Subcommands

| Command | Usage | Description |
| :--- | :--- | :--- |
| **`repl`** | `tasksmith repl [flags]` | Starts the [interactive terminal REPL](../../guides/cli.md) (identical to invoking `tasksmith` directly). |
| **`acp`** | `tasksmith acp [--socket <path>] [-w <workspace>]` | Serves [editor sessions](../../guides/acp.md) over JSON-RPC stdio. Each new, loaded, or resumed session uses the editor’s `cwd`; `-w` supplies the fallback workspace for listing and deleting detached sessions. Load replays history; resume does not. Close detaches without deleting history. |
| **`exec`** | `tasksmith exec [flags] [prompt...]` | Runs a single headless task against the Brain daemon and exits. Prompt text comes from arguments and/or piped stdin; `--input-format jsonl` drives a bidirectional NDJSON session. See the [Headless Execution Reference](../exec.md). |
| **`daemon serve`** | `tasksmith daemon serve [flags]` | Runs the Brain daemon server in the foreground. |
| **`daemon start`** | `tasksmith daemon start` | Spawns and detaches the Brain daemon process in the background. |
| **`daemon stop`** | `tasksmith daemon stop` | Gracefully terminates a running Brain daemon via SIGTERM / shutdown API. |
| **`daemon status`** | `tasksmith daemon status` | Checks daemon process health, build version, IPC socket connectivity, and active workspace runtimes. |
| **`version`** | `tasksmith version` | Prints the client build version and, when reachable without spawning, the running daemon's version. |
| **`auth login`** | `tasksmith auth login [provider] [--no-browser] [--paste]` | Starts the provider's browser sign-in and waits for it to settle. Antigravity prints the URL and supports `--no-browser` and `--paste`. Codex, Claude Code, and GitHub Copilot require browser sign-in on the daemon host and do not support those flags. Name the provider when multiple OAuth presets are available. |
| **`auth status`** | `tasksmith auth status [provider]` | With a provider name, prints `<provider>: <status> (<account>)`; with none, prints a `PROVIDER / AUTH / STATUS` table over all configured providers. |
| **`auth logout`** | `tasksmith auth logout [provider]` | Removes the daemon-stored OAuth credentials for one provider and reports `Signed out of <provider>` or `<provider> was not signed in`. |

All `auth` subcommands require a running daemon (`tasksmith daemon start`) and never spawn one.

## Global Flags

Workspace and socket flags are inherited. Help is available on each command; `--version` reports the root command’s version.

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--workspace <path>` | `-w` | `.` (current directory) | Path to the target workspace root directory. Discovers `WORKSPACE.md` and `.agents/`. |
| `--socket <path>` | | `$XDG_RUNTIME_DIR/tasksmith/brain.sock` | Explicit path to the Brain daemon Unix domain socket. When `XDG_RUNTIME_DIR` is unset, defaults to `~/.local/state/tasksmith/run/brain.sock`. |
| `--version` | | | Print the client build version and exit. |
| `--help` | `-h` | | Print help and usage information for any command. |

## REPL Session Flags

The following flags are accepted by `tasksmith` and `tasksmith repl` (also shared with `exec`) — `tasksmith daemon` subcommands reject them:

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--session <id>` | | *(none)* | UUIDv7 of an existing session to resume immediately. |
| `--continue` | `-c` | `false` | Resume the workspace's most recently updated top-level session (including forks). Ignored when `--session` is given; starts a fresh session when the workspace has none. |
| `--fork` | | `false` | Branch the resolved session (`--session` or `-c`) and attach to the fork instead of mutating the source. `--title` names the fork. A usage error when no session resolves. |
| `--agent <name>` | | `main` | Default agent persona to activate for newly initialized sessions. |
| `--model <id>` | | Provider default | Upstream model identifier (e.g., `openai/gpt-5.5` or `ollama/qwen3.8`). |
| `--title <title>` | | *(none)* | Optional human-readable title assigned to a newly created session. |
| `--sandbox <mode>` | | `native` when available | Sandbox isolation backend for sessions this client creates: `native` (OS kernel sandboxes) or `local` (in-process confinement). The mode is resolved per session at creation — one daemon can serve native and local sessions side by side — and cannot be changed afterwards (resuming an existing session with a differing request surfaces a warning instead). `native` requests degrade to `local` with a warning when a host probe shows kernel sandboxing is unusable (`sandbox-exec`/`bwrap` missing, restricted containers, nested sandboxes). |

## Headless Execution Flags (`exec` / `--print`)

Headless execution has its own reference page: [Headless Execution Reference](../exec.md) — flags, input/output framing, the bidirectional NDJSON control-frame protocol, daemon events, slash commands without the TUI, exit codes, and SDK embedding. The `exec` subcommand row appears in the Subcommands table above, and the REPL session flags apply to `exec` with the same meaning.

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--print` | `-p` | `off` | Root flag only: run the positional prompt headlessly instead of starting the REPL. |
| `--input-format <format>` | | `text` | stdin handling: `text` or `jsonl` (bidirectional control frames). See [§3](../exec.md#3-input-rules). |
| `--output-format <format>` | | `text` | stdout framing: `text`, `json`, or `jsonl`. See [§4](../exec.md#4-output-framing). |
| `--effort <level>` | | *(model default)* | Reasoning effort level (e.g. `low`, `medium`, `high`, or `default`). |
| `--mode <mode>` | | `default` | Permission mode: `default`, `accept_edits`, `plan`, `ask`, `auto`. |
| `--max-turns <n>` | | `0` | Stop after this many completed turns (`0` = unlimited). Enforced client-side; most useful in bidirectional runs. |
| `--ephemeral` | | `off` | Delete the session once the run settles. Ideal for throwaway CI runs. |
| `--force` | | `off` | `exec` only. Force a resumed session's `--model` switch even if it drops input modalities active in live context. |

## Daemon Flags

| Flag | Subcommand | Default | Description |
| :--- | :--- | :--- | :--- |
| `--lock <path>` | `daemon serve` | `$XDG_RUNTIME_DIR/tasksmith/brain.lock` | Override path for the daemon process lockfile. With a custom `--socket` and no `--lock`, the lock defaults to the socket path with a `.lock` extension. |

## Authentication flags

| Flag | Command | Default | Meaning |
| :--- | :--- | :--- | :--- |
| `--no-browser` | `auth login` | `false` | Print the Antigravity authorization URL instead of opening it. |
| `--paste` | `auth login` | `false` | Finish Antigravity sign-in by pasting the redirect URL. |

## Environment variables

| Variable | Default when unset | Use |
| :--- | :--- | :--- |
| `XDG_CONFIG_HOME` | `~/.config` | User configuration and appearance. |
| `XDG_DATA_HOME` | `~/.local/share` | Workspace data, trust, and credentials. |
| `XDG_STATE_HOME` | `~/.local/state` | Logs and fallback runtime state. |
| `XDG_RUNTIME_DIR` | Runtime files under `$XDG_STATE_HOME/tasksmith/run/` | Socket and process lock. |
| `XDG_CACHE_HOME` | `~/.cache` | Cache base directory. |
| `TASKSMITH_APPNAME` | `tasksmith` | Application directory name under the XDG bases. |
| `NO_COLOR` | Unset | A nonempty value disables ANSI color in the REPL exit summary. |

## See Also

- [CLI reference index](../cli.md)
- [Cli guide](../../guides/cli.md)
- [Sessions guide](../../guides/sessions.md)
- [Headless exec guide](../../guides/headless-exec.md)
- [Acp guide](../../guides/acp.md)
