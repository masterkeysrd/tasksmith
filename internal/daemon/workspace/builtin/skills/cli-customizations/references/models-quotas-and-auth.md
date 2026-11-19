# Model Billing, Quota Windows, & Provider Sign-In

## `/model` picker columns

The `/model` picker groups models by provider and displays three columns:

1. **Label**: model display name (plus `• current` on the active model).
2. **Capacity & aliases**: context window and typed aliases (for example, `"400.0k ctx · @fast, @default"`).
3. **Billing**: `"quota"` for subscription-billed providers, `"$3/$15 /M"` for per-token input/output pricing, or blank when pricing is unknown.

A muted footer beneath the list describes the highlighted model. Live filtering matches labels, aliases, model IDs, provider names, and billing cells.

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
