# Model providers

A ModelProvider describes a gateway driver, its connection settings, available models, and defaults. Definitions are discovered through the system, user, workspace, and current-project hierarchy, commonly in `.agents/providers/<name>.provider.yaml`. The resource name identifies the configured provider; `spec.type` selects the driver.

TaskSmith ships provider presets as templates. A preset being present does not itself make it an active provider. Gateway types include `openai`, `codex`, `anthropic`, `claudecode`, `github-copilot`, `ollama`, `typesafe`, `antigravity`, and `google-genai` (also `genai` or `gemini`). Provider support for completion, decision, and embedding roles differs; a capability declaration cannot add a role the driver does not implement. Decision model support in the gateway does not imply TaskSmith can evaluate Decision resources.

For Claude Code and GitHub Copilot, set `metadata.name` and `spec.type` to the same canonical identifier (`claudecode` or `github-copilot`) and set `spec.auth.type: oauth2` with no additional auth fields. Leave `spec.endpoint`, `spec.headers`, and `spec.config` unset. These drivers implement completion through `claude` and `copilot`, respectively, installed on the daemon host. The Copilot runtime must speak protocol 4, as Copilot CLI `1.0.114` does. Model access is account-specific; bundled aliases are suggestions. Use subscription quota, not per-token dollars, and report quota windows only when the vendor exposes them.

For ChatGPT plan inference, use `metadata.name: codex`, `spec.type: codex`, and `spec.auth.type: oauth2`. The driver manages the endpoint and credentials. Setup copies the preset models and aliases into the manifest. To browse models available to the signed-in account, use **Manage models** in `/providers`.

OpenAI and Codex map `@fast` to `gpt-6-luna`, `@balanced` to `gpt-6.2-sol`, and `@flagship` to `gpt-6.2-astra`. OpenAI maps `@smart` to Sol, and Codex maps it to Astra. Google GenAI maps `@balanced` to `gemini-4-neon` and `@fast` to `gemini-4-hydrogen`.

The three presets declare model names, limits, and aliases. Loom supplies model capabilities and API pricing at runtime. Codex uses ChatGPT plan billing.

## Provider definition

| Field | Type | Meaning |
| --- | --- | --- |
| `spec.type` | string | Gateway driver; required by TaskSmith. |
| `spec.enabled` | *bool | Enables the provider; defaults to true when omitted. A disabled manifest is skipped by the gateway and model resolution while keeping its endpoint, auth, headers, aliases, and models in YAML. |
| `spec.endpoint` | string | API base endpoint; handling and defaults depend on the driver. |
| `spec.defaultModels` | object | Role-specific defaults: `completion`, `decision`, and `embedding`. |
| `spec.auth` | object | Credential source and authentication scheme. |
| `spec.headers` | map | Additional HTTP headers. |
| `spec.config` | map | Driver-specific options. |
| `spec.models` | object[] | Model catalog and descriptive metadata. |

Endpoint and configuration support varies by driver. For example, Ollama has a local default endpoint when omitted; the Google GenAI adapter does not currently pass the manifest endpoint into its client. TaskSmith reads selected OpenAI `config` options such as `apiMode` and `store`; arbitrary keys, such as generic retry or timeout settings, are not necessarily applied. Custom headers are added when the outgoing request does not already have that header.

### Enabling and disabling providers

`spec.enabled` (`*bool`, default `true`) is the declarative switch for provider availability: a disabled provider contributes no models, aliases, or role defaults, and comes back unchanged when re-enabled. When a patch targets a manifest the workspace may write (workspace or current-project tier), the owning `*.provider.yaml` is rewritten through WARP formatting; when the provider is only inherited from the user tier, disabling instead writes a shadow override at `.agents/providers/<name>.provider.yaml` with `enabled: false` (plus the inherited identity), so muting a global provider for one repository never edits `~/.config/tasksmith/`. Re-enabling removes the shadow when the inherited manifest is already enabled. The daemon hot-reloads the workspace after every patch so the gateway, model catalog, and manifests agree.

Preset installs through `/setup` and `/providers` copy every preset model, but treat aliases as suggestions. An alias already assigned to another model in the destination is skipped and reported; existing provider assignments and workspace overrides remain in place.

## Model resolution

The Ollama preset defaults to `qwen3.8` for completion and `nimble-v3` for decisions. Pull `nimble-v3` into Ollama before using it.

A model identifier commonly has the form `provider/model`. Workspace resolution considers explicit overrides, workspace role defaults, the configured default provider's role defaults, then other effective providers' defaults and capability matches. `defaultModels` can separately select `completion`, `decision`, and `embedding`. The gateway also considers provider availability and authentication when resolving models. See `workspace-and-context.md` for workspace precedence.

## Authentication and environment

| `spec.auth` field | Type | Meaning |
| --- | --- | --- |
| `type` | string | HTTP scheme: `bearer`, `api-key`, `basic`, `oauth2`, or `none`; matching is case-insensitive. |
| `header` | string | Optional authentication header name; defaults to `Authorization` in that transport. |
| `token` | string | Secret expression (`${env:<VAR>}` or `${file:<PATH>}`) for `bearer`, `api-key`, or pre-encoded `basic`; `~/…` and paths relative to the manifest are expanded for `file`. |
| `username` | string | Username for `basic` auth (literal or `${<scheme>:<ref>}` expression). |
| `password` | string | Secret expression (`${env:<VAR>}` or `${file:<PATH>}`) for `basic` auth password. |

`bearer` sends `Authorization: Bearer <credential>`, `api-key` sends the credential un-prefixed, `basic` sends `Basic base64(<credential>)`, `oauth2` uses managed OAuth tokens, and `none` reads and sends no credential. When `header` replaces `Authorization`, the gateway also removes the credential header a provider SDK would add on its own, so only the configured header is sent.

When `${env:<VAR>}` is used, the gateway reads the workspace environment and falls back to the host environment; a missing or empty value produces a credential error. File credentials (`${file:<PATH>}`) must be readable and nonempty. Rotating a credential value or file changes the gateway fingerprint and rebuilds provider connections. Shipped token presets use `type: bearer` (e.g. `openai`, `typesafe`) or `type: api-key` with a vendor header (`X-Api-Key` for `anthropic`, `x-goog-api-key` for `google-genai`) and `token: "${env:<VAR>}"`, while `antigravity` uses `type: oauth2`.

### Managed OAuth sign-in

`oauth2` providers authenticate through the existing daemon HTTP API; clients do not handle tokens themselves. Antigravity supports a daemon-managed PKCE flow with pasted redirects. Claude Code and GitHub Copilot use vendor-owned sign-in, completed in the browser on the daemon host. TaskSmith uses a protected data directory for each vendor CLI's credentials and runtime state, not host environment credentials or shared user profiles. The vendor CLIs run without native tools and configuration; TaskSmith tools remain subject to its permissions and sandbox.

| Method | Path | Daemon behavior |
| --- | --- | --- |
| `GET` | `/v1/providers` | Lists providers as `ProviderSummary` (`name`, `status`, `has_key`, `auth_type`, `account`, …). |
| `POST` | `/v1/providers/{name}/auth` | Starts or resumes the provider's sign-in and returns `ProviderAuthResponse` (`provider`, optional `auth_url`, `status`). |
| `GET` | `/v1/providers/{name}/auth` | Current flow status: `idle`, `authorizing`, `authenticated`, `error`, or `signed-out`. |
| `POST` | `/v1/providers/{name}/auth/callback` | Completes a flow that accepts pasted redirects (Antigravity), with `callbackUrl` as the full URL, bare query string, or code. This is not supported for vendor CLI sign-in. |
| `DELETE` | `/v1/providers/{name}/auth` | Cancels any in-flight flow and removes stored credentials (`status: signed-out`, or `idle` when not signed in). |

For daemon-managed token flows, tokens persist under `$XDG_DATA_HOME/tasksmith/credentials/<provider>.json` (normally `~/.local/share/tasksmith/credentials/<provider>.json`) with owner-only permissions. The daemon refreshes these tokens in the background and reloads workspaces when credentials change. Claude Code and GitHub Copilot instead use isolated CLI profiles under `$XDG_DATA_HOME/tasksmith/credentials/<provider>/`. Agents never read, write, or print credential files or edit provider profiles — sign-in, status, and logout belong to the user-facing auth surfaces.

TaskSmith builds its workspace environment from the process environment and `.env` files: user configuration first, applicable ancestor files from outer to inner, workspace root, then current directory. Later layers override earlier values. Reference secrets through environment variables or files instead of embedding them in manifests or headers.

## Model entries

| Field | Type | Meaning |
| --- | --- | --- |
| `id` | string | Model identifier used in `provider/model` references. |
| `name`, `label` | string | Model name and human-readable label. |
| `aliases` | string[] | Additional `@`-reference names for the model. |
| `limits` | object | Token and embedding-vector limits. |
| `capabilities` | object | Supported roles, features, and configuration boundaries. |
| `cost` | object | Pricing metadata and conditional tiers. |

`name` is commonly repeated from `id`; because that adds no display information, it does not change the displayed name — use `label` for that. Workspace `spec.modelAliases` targets and model `aliases` are equivalent `@`-reference names, stored without the `@`.

TaskSmith standardizes four completion-model tiers that the built-in provider presets declare on their models: `fast` (cheapest, responsive), `balanced` (everyday workhorse, usually also the preset's default completion model), `smart` (strong reasoning step above balanced), and `flagship` (most capable). Reference them as `@fast`, `@balanced`, `@smart`, and `@flagship` from agent `spec.models`, session models, or `WORKSPACE.md` `spec.modelAliases` (which can retarget a tier to another model without editing provider files). Agent `spec.models` may also use `@default` (the workspace/provider role-default hierarchy) or the TaskSmith-reserved `@inherit` (the delegating parent session's model during subagent delegation; never a provider target). A provider that declares no tier alias simply leaves that alias unresolvable; add your own `aliases` or `spec.modelAliases` entries to enable it.

`limits.context` is the context token limit, `limits.output` the output token limit, and `limits.dimensions` the default or maximum embedding vector size. The Claude Code and GitHub Copilot presets declare context and output limits. Nonzero manifest limits override the corresponding provider metadata; omit a limit to let provider metadata fill it when available. Match limits to the actual provider and account; a manifest does not make an unsupported model available.

### Capabilities

| Field | Type | Meaning |
| --- | --- | --- |
| `completion`, `decision`, `embedding` | bool | Model roles. For role matching, omitted `completion` defaults to true; `decision` and `embedding` default to false. |
| `dimensions` | int[] | Supported configurable embedding dimensions. |
| `attachment`, `tools` | bool | Attachment and tool-calling support. |
| `structuredOutput`, `temperature` | bool | Structured-output and sampling-temperature support. |
| `modalities` | object | `input` and `output` arrays of modality names such as `text` and `image`. |
| `thinking` | object[] | Supported reasoning strategies. |

A thinking entry has `type` (`toggle`, `effort`, `budget`, or `adaptive`), optional `isDefault`, `allowedEfforts`, `maxBudgetTokens`, and a strategy-specific `default` (a string, an integer token budget, or a boolean). Omitted optional feature flags are unspecified metadata, not proof of support.

The workspace preserves all these schema fields. Gateway conversion forwards completion, decision, embedding, attachment, tools, temperature, dimensions, and selected thinking details. Explicit temperature capability values override the provider profile; if neither source specifies support, it is unknown. The `isDefault` flag is not forwarded as a field; instead it selects the thinking setting applied when neither the session nor the active agent overrides thinking. The gateway does not forward `structuredOutput` or `modalities` to the gateway model configuration. The actual provider and model still determine supported features.

### Cost metadata

`cost` contains `input`, `output`, `cacheRead`, and `cacheWrite` prices, typically expressed per million tokens. `cost.tiers` entries contain a `label`, a `tier` condition with `type` and `size`, and optional price overrides using the same fields. These values are descriptive provider metadata; the current workspace-to-gateway conversion does not forward them for runtime pricing calculations.

## Example

```yaml
apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: example
  description: Example OpenAI-compatible provider
spec:
  type: openai
  defaultModels:
    completion: example-model
  auth:
    type: bearer
    token: "${env:OPENAI_API_KEY}"
  config:
    apiMode: responses
    store: false
  models:
    - id: example-model
      name: example-model
      label: Example Model
      limits:
        context: 128000
        output: 16384
      capabilities:
        tools: true
        modalities:
          input: [text, image]
          output: [text]
        thinking:
          - type: effort
            allowedEfforts: [low, medium, high]
            default: medium
      cost:
        input: 1.0
        output: 4.0
        tiers:
          - label: Large context
            tier:
              type: context
              size: 100000
            input: 2.0
            output: 8.0
```

The example's model ID, limits, and prices are illustrative; use a configured provider and model that actually exist.
