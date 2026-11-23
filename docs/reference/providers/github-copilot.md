# GitHub Copilot

The `github-copilot` provider is a completion backend for an eligible GitHub Copilot account. TaskSmith uses Loom to run the installed GitHub Copilot CLI (`copilot`) on the daemon host.

| Setting | Required value or behavior |
| :--- | :--- |
| Daemon host | Put a `copilot` executable that speaks protocol 4 on the daemon's `PATH`. Copilot CLI `1.0.114` is compatible with the pinned Copilot SDK v2.0.1, which accepts protocol 4 only. Sign-in requires a browser on the daemon host. |
| `metadata.name` and `spec.type` | Both must be `github-copilot`. |
| `spec.auth` | `type: oauth2` only. Do not supply credential fields, an auth header, or vendor tokens in `.env`. |
| `spec.endpoint`, `spec.headers`, `spec.config` | Managed by the integration; leave these fields unset. |
| `spec.models`, `spec.defaultModels.completion` | Configure completion models and a default your account can use. Preset aliases are [suggestions](../providers.md#tier-aliases), not account entitlements. |
| Billing | Subscription quota, not per-token dollars. TaskSmith shows remaining percentages and reset windows only when the vendor reports them. |

Choose **GitHub Copilot** in `/setup` and complete sign-in in the browser on the daemon host. In an existing workspace, add the preset from `/providers` and use `/login github-copilot` (or `tasksmith auth login github-copilot` with a running daemon). This flow does not support callback paste, `--no-browser`, or headless device login. See [Provider Sign-In](../cli.md#provider-sign-in-login) for status and logout.

Use **Manage models** in `/providers` to browse the available catalog. Actual model access depends on the signed-in account; a preset entry is not proof of access. Use `/model` to select a configured completion model.

## Preset model limits

These values come from Loom's GitHub Copilot catalog. In TaskSmith, nonzero manifest limits override live CLI catalog values for the same fields. Remove a limit to use the CLI-reported value when available. Actual model access depends on the signed-in account.

| Model ID | Context (tokens) | Maximum output (tokens) |
| :--- | ---: | ---: |
| `gpt-6-luna` | 1,050,000 | 128,000 |
| `gpt-6.2-sol` | 1,050,000 | 128,000 |
| `gpt-6.1-terra` | 1,050,000 | 128,000 |
| `gpt-6.2-astra` | 1,050,000 | 128,000 |

## Runtime and credentials

| Runtime and credentials | Behavior |
| :--- | :--- |
| Profile | TaskSmith uses a protected, isolated profile and does not reuse your normal Copilot profile or inherit vendor tokens. Copilot's OS keyring access is explicitly disabled for login, the compatibility check, account verification, inference, and logout. |
| Launcher | For authentication and inference, TaskSmith uses the Copilot SDK to start `copilot` directly with an explicit allowlist of child environment variables. There is no shell wrapper, and the credential directory does not need to allow executable files. |
| Sign-out | Clears TaskSmith's active registration even if `copilot` is missing or uses an incompatible protocol. Vendor credential cleanup is best effort; local sign-out does not guarantee vendor-side revocation. |
| Tools | Native Copilot tools and configuration are disabled. TaskSmith tools still follow [TaskSmith permissions](../../guides/permissions.md) and its [sandbox](../../architecture/sandbox.md). |
