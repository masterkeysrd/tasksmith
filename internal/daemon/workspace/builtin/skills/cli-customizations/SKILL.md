---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: cli-customizations
  displayName: CLI Customizations
  description: Configure and operate TaskSmith's CLI and REPL workflows—statusline.json (/statusline), theme.json and custom themes (/theme), session modes and plan review (/mode, /plan, Shift+Tab), /sandbox, /permissions, --sandbox, ! shell commands, /model and /usage quota windows, multimodal @/paste input, -c/--continue and /fork, headless tasksmith exec/-p (NDJSON), and provider auth (/login).
spec:
  useWhen: "The user asks how to customize CLI appearance or keybindings, switch modes or review plans, drive /sandbox or /permissions, interpret /model pricing or subscription quotas, attach images/PDFs/audio, resume or fork sessions, automate TaskSmith headlessly in CI/scripts, or sign in to OAuth providers."
  keywords: [statusline, theme, mode, /mode, /plan, shift+tab, accept_edits, ask, auto, /sandbox, /permissions, --sandbox, quota, /usage, /model, multimodal, paste, exec, headless, --print, ndjson, jsonl, --continue, --fork, /fork, auth, /login, oauth]
---

# CLI Customizations

TaskSmith's CLI stores user-level appearance preferences in `$XDG_CONFIG_HOME/tasksmith/` (`statusline.json`, `theme.json`) and exposes session posture, permissions, billing, branching, and OAuth sign-in through dedicated REPL `/commands` and CLI subcommands (`exec`, `auth`, `daemon`).

Prefer driving the interactive REPL view when the user is inside the TUI; edit `statusline.json` or `theme.json` directly only when working non-interactively or preparing a requested snippet.

## Core guardrails & etiquette

- **Protected daemon stores**: never attempt to edit `sandbox.json`, `permissions.json`, or OAuth credential files (`~/.local/share/tasksmith/credentials/`) with file tools — they are protected by design. Guide the user through `/sandbox`, `/permissions`, `tasksmith auth`, `/login`, or provide an explicit JSON snippet when appropriate.
- **Safety badges & theme keys**: never remove the permission `mode` segment from `statusline.json` unless explicitly asked; `theme.json` is optional (built-in theme applies when absent) — never invent theme color keys.
- **User-scope files**: `statusline.json` and `theme.json` are personal XDG settings, not repository files; do not create or commit them inside a project workspace unless asked, and remind users that direct JSON edits take effect after restarting the REPL (whereas `/statusline` applies live). If `statusline.json` fails to parse, fix the JSON rather than deleting the file.
- **Billing & headless accuracy**: never report dollar costs for subscription (`quota`) models or invent provider reset countdowns; never run `tasksmith exec` / `-p` on an interactive TTY without a prompt (exit `2`), and recommend `--input-format jsonl` whenever headless runs may hit interactive prompts (which exit `4` / `needs_input` in `text` mode).

## Topic references

Read only the reference file matching the user's question (paths are relative to this skill directory):

| Topic | Reference | What it covers |
| --- | --- | --- |
| Status line & themes | `references/appearance.md` | `statusline.json` (`/statusline`), `theme.json` (`/theme`), built-in themes, and custom `$XDG_CONFIG_HOME/tasksmith/themes/<name>.json` token groups |
| Modes & planning | `references/modes-and-planning.md` | `default` / `accept_edits` / `plan` / `ask` / `auto` gates vs. postures, `Shift+Tab`, `/mode`, `/plan [prompt]`, `<session-dir>/plans/`, and `exit_plan_mode` review (`p` plan viewer) |
| Sandbox & permissions UI | `references/sandbox-and-permissions.md` | `/sandbox` tabs (`Policy`, `Exclusions`, `Effective`), live policy refresh vs. `mode` restart, `/permissions`, `--sandbox`, and `!command` shell execution |
| Models, quotas, & OAuth | `references/models-quotas-and-auth.md` | `/model` columns (`ctx`, `@alias`, `quota` vs. `$/M`), statusline quota badges, `/usage` `4 Quotas` tab thresholds, `tasksmith auth login\|status\|logout` (`--no-browser`, `--paste`), and `/login` |
| Resume, forking, & media | `references/sessions-and-media.md` | `-c` / `--continue`, `/fork`, `--fork`, `/sessions` `F3`, fork copy/reset rules, `@<path>` image/PDF/audio mentions, `Ctrl+V`/`Alt+V` paste, and `capability_mismatch` `--force` |
| Headless & SDK (`exec` / `-p`) | `references/headless-exec.md` | `tasksmith exec` / `-p` flags, `text`/`json`/`jsonl` framing, bidirectional `stdin` control frames (`permission`, `question`, `plan_review`, `config`), slash commands, exit codes, and `client.ConnectOrStart` |
