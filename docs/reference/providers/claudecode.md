# Claude Code

The `claudecode` provider is a completion backend for an eligible Claude account. TaskSmith uses Loom to run the installed Claude Code CLI (`claude`) on the daemon host. This is separate from the [Anthropic API provider](./anthropic.md) and does not use an Anthropic API key.

| Setting | Required value or behavior |
| :--- | :--- |
| Daemon host | Install `claude` and make it available to the daemon. First-time sign-in and reauthorization require a browser on that host. |
| `metadata.name` and `spec.type` | Both must be `claudecode`. |
| `spec.auth` | `type: oauth2` only. Do not supply credential fields, an auth header, or vendor tokens in `.env`. |
| `spec.endpoint`, `spec.headers`, `spec.config` | Managed by the integration; leave these fields unset. |
| `spec.models`, `spec.defaultModels.completion` | Configure completion models and a default your account can use. Preset aliases are [suggestions](../providers.md#tier-aliases), not account entitlements. |
| Billing | Subscription quota, not per-token dollars. TaskSmith shows remaining percentages and reset windows only when the vendor reports them. |

Choose **Claude Code** in `/setup` and complete sign-in in the browser on the daemon host. In an existing workspace, add the preset from `/providers` and use `/login claudecode` (or `tasksmith auth login claudecode` with a running daemon). This flow does not support callback paste, `--no-browser`, or headless device login. See [Provider Sign-In](../cli.md#provider-sign-in-login) for status and logout.

Use **Manage models** in `/providers` to browse the available catalog. Actual model access depends on the signed-in account; a preset entry is not proof of access. Use `/model` to select a configured completion model.

## Preset model limits

These are the Anthropic catalog limits Loom uses to enrich Claude Code model metadata; they are not account-specific measurements. In TaskSmith, nonzero manifest limits override live catalog values for the same fields. Removing a limit lets provider metadata supply it when available. Actual model access depends on the signed-in account.

| Model ID | Context (tokens) | Maximum output (tokens) |
| :--- | ---: | ---: |
| `claude-haiku-5-5` | 1,000,000 | 128,000 |
| `claude-sonnet-5-5` | 1,000,000 | 128,000 |
| `claude-opus-5-5` | 1,000,000 | 128,000 |
| `claude-fable-5-2` | 1,000,000 | 128,000 |

## Runtime and credentials

| Runtime and credentials | Behavior |
| :--- | :--- |
| Profile | TaskSmith uses a protected, isolated profile. It does not import vendor tokens from the host environment or sync auth files from the host user's `~/.claude` into this profile. |
| Runtime | Loom's `SpawnedStdio` starts `claude` directly with an explicit allowlist of child environment variables. It does not use a shell wrapper or temporary launcher. |
| Sign-out | Clears TaskSmith's active registration even if `claude` is missing or incompatible. Vendor credential cleanup is best effort; local sign-out does not guarantee vendor-side revocation. |
| Tools | Native Claude Code tools and configuration are disabled. TaskSmith tools still follow [TaskSmith permissions](../../guides/permissions.md) and its [sandbox](../../architecture/sandbox.md). |
