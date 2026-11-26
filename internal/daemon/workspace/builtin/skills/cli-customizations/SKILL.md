---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: cli-customizations
  displayName: CLI Customizations
  description: Configure and operate TaskSmith's CLI and REPL workflows—statusline.json (/statusline), theme.json and custom themes (/theme), session modes and plan review (/mode, /plan, Shift+Tab), /sandbox, /permissions, --sandbox, ! shell commands, tool and MCP availability toggling in /tools and /mcp (spec.enabled & WORKSPACE.md policies), /model and /usage quota windows, the /providers browser and shared F2 model alias editor, multimodal @/paste input, -c/--continue and /fork, headless tasksmith exec/-p (NDJSON), tasksmith acp editor integration, and provider auth (/login).
spec:
  useWhen: "The user asks how to customize CLI appearance or keybindings, switch modes or review plans, drive /sandbox or /permissions, enable or disable tools or MCP servers from /tools or /mcp, interpret /model pricing or subscription quotas, manage providers or model aliases via /providers or the F2 alias editor, attach images/PDFs/audio, resume or fork sessions, automate TaskSmith headlessly in CI/scripts, connect an ACP editor, or sign in to OAuth providers."
  keywords: [statusline, theme, mode, /mode, /plan, shift+tab, accept_edits, ask, auto, /sandbox, /permissions, --sandbox, quota, /usage, /model, /providers, /provider, alias, aliases, f2, spec.enabled, /tools, /mcp, disabled, disable, enable, tools, mcp, workspace policies, multimodal, paste, acp, exec, headless, --print, ndjson, jsonl, --continue, --fork, /fork, auth, /login, oauth]
---

# CLI Customizations

TaskSmith's CLI stores user-level appearance preferences in `$XDG_CONFIG_HOME/tasksmith/` (`statusline.json`, `theme.json`) and exposes session posture, permissions, billing, branching, and OAuth sign-in through dedicated REPL `/commands` and CLI subcommands (`acp`, `exec`, `auth`, `daemon`).

Prefer driving the interactive REPL view when the user is inside the TUI; edit `statusline.json` or `theme.json` directly only when working non-interactively or preparing a requested snippet.

For ACP editors, configure `tasksmith` as the executable and `acp` as its argument. Do not use `exec --input-format jsonl`: that is a different protocol. The editor supplies each session’s working directory; `--socket` selects the daemon. Load replays history; resume does not. Close preserves history; delete removes it. Editors can select mode, model, thought level, and agent, and send image/audio attachments. Questions and plan reviews use `elicitation.form` when available; otherwise retry in the TUI. Compaction, notices, rich plans, and subagent streams depend on editor capabilities. File and terminal tools, MCP, and auth stay daemon-managed; do not enable editor MCP injection or ACP login.

## Core guardrails & etiquette

- **Protected daemon stores**: never attempt to edit `sandbox.json`, `permissions.json`, or OAuth credential files (`~/.local/share/tasksmith/credentials/`) with file tools — they are protected by design. Guide the user through `/sandbox`, `/permissions`, `tasksmith auth`, `/login`, or provide an explicit JSON snippet when appropriate.
- **Safety badges & theme keys**: never remove the permission `mode` segment from `statusline.json` unless explicitly asked; `theme.json` is optional (built-in theme applies when absent) — never invent theme color keys.
- **User-scope files**: `statusline.json` and `theme.json` are personal XDG settings, not repository files; do not create or commit them inside a project workspace unless asked, and remind users that direct JSON edits take effect after restarting the REPL (whereas `/statusline` applies live). If `statusline.json` fails to parse, fix the JSON rather than deleting the file.
- **Billing & headless accuracy**: never report dollar costs for subscription (`quota`) models or invent provider reset countdowns; never run `tasksmith exec` / `-p` on an interactive TTY without a prompt (exit `2`), and recommend `--input-format jsonl` whenever headless runs may hit interactive prompts (which exit `4` / `needs_input` in `text` mode).

For read-only code review, use `/review [target]` or `tasksmith exec "/review HEAD~1..HEAD"` in CI. The command opens a clean `code-reviewer` session; omit the target to inspect local changes.

## Topic references

Read only the reference file matching the user's question (paths are relative to this skill directory):

| Topic | Reference | What it covers |
| --- | --- | --- |
| Status line & themes | `references/appearance.md` | `statusline.json` (`/statusline`), `theme.json` (`/theme`), built-in themes, and custom `$XDG_CONFIG_HOME/tasksmith/themes/<name>.json` token groups |
| Modes & planning | `references/modes-and-planning.md` | `default` / `accept_edits` / `plan` / `ask` / `auto` gates vs. postures, `Shift+Tab`, `/mode`, `/grill-me` design interviews, `/plan [prompt]`, `<session-dir>/plans/`, and `exit_plan_mode` review (`p` plan viewer) |
| Sandbox & permissions UI | `references/sandbox-and-permissions.md` | `/sandbox` tabs (`Policy`, `Exclusions`, `Effective`), live policy refresh vs. `mode` restart, `/permissions`, `--sandbox`, `!command` shell execution, and `/tools` / `/mcp` availability toggling (`[Disabled]` badge, `Space` / `e` keys) |
| Models, quotas, & OAuth | `references/models-quotas-and-auth.md` | `/model` columns (`ctx`, `@alias`, `quota` vs. `$/M`), alias status line, `/providers` browser (`spec.enabled` toggle, model manager, catalog, add flow), shared `F2` alias editor (atomic 1-to-1 moves), statusline quota badges, `/usage` `4 Quotas` tab thresholds, `tasksmith auth login\|status\|logout` (`--no-browser`, `--paste`), and `/login` |
| Sessions, subagents, & media | `references/sessions-and-media.md` | `-c` / `--continue`, `/fork`, `--fork`, `/sessions` `F3`, `/subagents` inspection and model badges, `@<path>` image/PDF/audio mentions, `Ctrl+V`/`Alt+V` paste, and `capability_mismatch` `--force` |
| Headless & SDK (`exec` / `-p`) | `references/headless-exec.md` | `tasksmith exec` / `-p` flags, `text`/`json`/`jsonl` framing, bidirectional `stdin` control frames (`permission`, `question`, `plan_review`, `config`), slash commands, exit codes, and `client.ConnectOrStart` |
