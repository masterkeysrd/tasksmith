# Model Billing, Quota Windows, & Provider Sign-In

## `/model` picker columns

The `/model` picker groups models by provider and displays three columns:

1. **Label**: model display name (plus `• current` on the active model).
2. **Capacity & aliases**: context window and typed aliases (for example, `"400.0k ctx · @fast, @default"`).
3. **Billing**: `"quota"` for subscription-billed providers, `"$3/$15 /M"` for per-token input/output pricing, or blank when pricing is unknown.

A muted footer beneath the list describes the highlighted model. Live filtering matches labels, aliases, model IDs, provider names, and billing cells.

A muted alias status line under the title shows where each standard tier resolves (`@default → anthropic/claude-x · @fast → unassigned`). `Enter` stays a strictly ephemeral session model switch; `F2` opens the shared alias editor (below) on the highlighted model, and printable keys keep feeding the filter.

## `/providers` browser & provider management

`/providers` (alias `/provider`) manages the workspace's `ModelProvider` manifests (`.agents/providers/*.provider.yaml` and `~/.config/tasksmith/agents/providers/*.provider.yaml`) through the daemon — there is no sidecar state, and every action patches YAML and hot-reloads:

- **Browser** (unfiltered, so single-letter keys act on the highlighted provider): `space`/`e` toggle `spec.enabled`; `enter`/`m` open the model manager; `a`/`n` add a provider (the empty no-providers screen accepts `a`/`n` too); `esc` exits (`Exited /providers command`). Rows show status glyphs: `✔` Active, `●` Local, `⚠` Missing Auth, `○` Disabled (`Disabled (shadow)` when a workspace override mutes an inherited user provider).
- **Disabling semantics**: patching writes the owning manifest when it is workspace/project tier; muting an inherited user-tier provider instead writes a workspace shadow `.agents/providers/<name>.provider.yaml` carrying only `enabled: false`, so `~/.config/tasksmith/` stays untouched and the mute is repository-scoped. Re-enabling removes a mute-only shadow when the inherited manifest is enabled, clears the flag in place when a full hand-authored workspace override shares the name (never deleting it), and copies a disabled inherited manifest into a full workspace override otherwise.
- **Model manager** (`Models · <name>`, `★` marks the default completion model): `space` marks a model for removal, `d` stages a new default, `s` saves, `n` adds a custom model id (for OpenAI-compatible/local endpoints), `c` opens the type catalog (loom `models.dev` metadata; ollama answers live), `f2` opens the alias editor, `esc` goes back. Catalog/model editing is only offered for writable manifests.
- **Model catalog** (`Catalog · <name>`): filter-first — every printable key types into the filter, `enter` toggles an entry, `ctrl+s` adds the selected entries in one write, `esc` goes back.
- **Add provider**: preset list (or `Custom provider…`) then a field form; `enter` toggles `Scope` and submits `Create provider`, `e` edits the highlighted text field (non-text rows show why they have no free-text value), `esc` steps back. Supplied API keys are written **only** to the scope's `.env` (`~/.config/tasksmith/.env` or `<workspace-root>/.env`, with `.env` gitignored in workspace scope) and referenced from the manifest as `${env:<NAME>_API_KEY}` — never paste secrets into YAML or ask the user to. A preset-backed create keeps the preset's auth scheme and env var; a custom provider (name different from its type) never inherits the preset's auth, config, defaults, or models.

## Model alias editor (`F2` in `/model` and `/providers`)

One checklist card shared by both commands, persisting directly to the owning `*.provider.yaml`:

- **Standard aliases** always render: `@default` (primary session & main agent), `@fast`, `@balanced`, `@smart`, `@flagship`, each with live state (`assigned to this model`, `unassigned`, `currently <p>/<m>`, staged `moves from <p>/<m>`, or `⚠ duplicate on …` when manual YAML edits collided).
- **Custom aliases**: existing per-model aliases plus `n` to add a new lowercase `@name` (letters, digits, `-`, `_`). Typing an alias owned by another model stages the move (`moves from <p>/<m>`) instead of rejecting it.
- **Keys**: `space` toggle, `n` add, `enter` save, `esc` cancel.
- **Atomic move**: saving sends the exact set the model should own; each reassigned alias is stripped from every other model across writable manifests before being added, keeping bare aliases 1-to-1. Checking `@default` writes the owning manifest's `spec.defaultModels.completion` instead of a model alias.
- **Boundaries**: `WORKSPACE.md` `spec.modelAliases` (and a `spec.defaultModels.completion` for `@default`) shadow per-model aliases and are never rewritten — the editor wins over duplicate-manifest warnings with `⚠ WORKSPACE.md overrides @default`. `@inherit` is reserved and rejected.

## Subscription quota windows & `/usage`

For subscription-billed (`QuotaBilled`) models:

- **Status line `usage` segment**: switches from dollar cost to a quota badge for the bottleneck bucket (`94% (↻ 3h12m) · ↑2.0M ↓4.9k`, or `∞` when unlimited — see `references/appearance.md`).
- **Turn & exit summaries**: omit dollar cost automatically.
- **`/usage` `4 Quotas` tab** (also reachable via `/usage quotas`): lists each provider's multi-bucket quota snapshots with one bar per bucket showing percent remaining, optional `used/limit unit`, reset countdown, or `unlimited`. Gauges color-code remaining capacity:
  - `< 20%`: red
  - `< 50%`: yellow
  - `>= 50%`: green
  Shared quota pools are grouped by bucket ID, and readings update live from daemon SSE events.
- **Accuracy rules**: never claim a dollar cost for a quota-billed call, and never invent a reset countdown that the provider did not report.

## Provider OAuth sign-in (`tasksmith auth` & `/login`)

OAuth model providers (such as Antigravity) authenticate through the daemon; the CLI drives the flow:

- **`tasksmith auth login [provider]`**: starts the daemon's PKCE flow, prints the authorization URL, opens a browser, and waits up to 5 minutes for completion.
  - `--no-browser`: prints the URL without launching a browser.
  - `--paste`: reads the final redirect URL from `stdin` — use this on SSH or headless hosts where the browser cannot reach the daemon's loopback callback.
- **`tasksmith auth status [provider]`**: prints `<provider>: <status> (<account>)` for one provider or a `PROVIDER / AUTH / STATUS` table across all configured providers.
- **`tasksmith auth logout [provider]`**: deletes stored credentials for that provider and cancels any in-flight sign-in flow.
- **REPL `/login [provider]`** (aliases `/auth`, `/signin`): opens the interactive sign-in screen:
  - `c`: copy the authorization URL to clipboard.
  - `Enter`: submit a pasted redirect URL (full address-bar URL, query string, or bare code).
  - `Esc`: dismiss the screen while keeping the daemon's flow open for up to 5 minutes; the settled result posts to the conversation timeline.
- **Provider resolution & daemon requirement**:
  - Omitting `[provider]` auto-selects only when **exactly one** OAuth-capable provider is configured; otherwise the command lists the candidates and asks you to specify one.
  - All `tasksmith auth` subcommands require a running daemon and never auto-spawn one — instruct users to run `tasksmith daemon start` first if the daemon is stopped.
- **Credential boundary**: never read, write, or print OAuth token files under `~/.local/share/tasksmith/credentials/`; manage credentials strictly via `tasksmith auth` or `/login`.
