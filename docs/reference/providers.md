# Providers Overview & Manifest Reference

In TaskSmith, **Model Providers** represent the upstream AI engines—such as OpenAI, Anthropic, Ollama, Google GenAI, and TypeSafe—that power agents, tools, and semantic embeddings.

TaskSmith decouples agents and skills from specific vendor APIs. For details on how workspaces resolve model roles, load hierarchical `.env` secrets, and prioritize configuration tiers, see [Workspaces](../guides/workspaces.md).

---

## 1. Supported Providers

TaskSmith includes built-in drivers and presets for the following providers:

| Provider | Type Identifier(s) | Supported Roles | Best Used For | Guide |
| :--- | :--- | :--- | :--- | :--- |
| **OpenAI** | `openai` | `completion`, `decision`, `embedding` | Frontier reasoning (`gpt-6.1-sol`, `gpt-6-astra`), Responses API tools, embeddings | [OpenAI Guide](./providers/openai.md) |
| **Anthropic** | `anthropic` | `completion`, `decision` (fallback) | Extended thinking, deep reasoning (`claude-sonnet-5-5`, `claude-opus-5-5`), prompt caching | [Anthropic Guide](./providers/anthropic.md) |
| **Ollama** | `ollama` | `completion`, `decision`, `embedding` | 100% offline local inference (`qwen3.8`, `nimble-v3`, `nomic-embed-text`), zero data egress | [Ollama Guide](./providers/ollama.md) |
| **Google GenAI** | `google-genai`, `genai`, `gemini` | `completion`, `embedding` | Massive 1M+ context window (`gemini-4-carbon`, `gemini-4-argon`), multimodal inputs | [Google GenAI Guide](./providers/google-genai.md) |
| **TypeSafe** | `typesafe` | `decision` | Ultra-fast native System 1 decisions (`jev-latest`), sub-50ms deterministic classifications | [TypeSafe Guide](./providers/typesafe.md) |
| **Antigravity** | `antigravity` | `completion` | Enterprise model gateway with managed thinking effort tiers (`gemini-4-carbon`, `gemini-3.8-flash`) | [Antigravity Guide](./providers/antigravity.md) |

---

## 2. Provider Manifest Schema (`*.provider.yaml`)

Provider configurations are declared as Warp resources with `kind: ModelProvider` under `.agents/providers/<name>.yaml` (or globally in `~/.config/tasksmith/providers/`):

```yaml
apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  # Provider identifier used in model references (e.g. "openai/gpt-5.5")
  name: openai
spec:
  # Driver type: openai | anthropic | ollama | google-genai (genai, gemini) | typesafe | antigravity
  type: openai

  # Upstream API endpoint URL
  endpoint: https://api.openai.com/v1

  # Role-based default models for this provider (completion, decision, embedding)
  defaultModels:
    completion: gpt-5.5
    decision: gpt-5.5
    embedding: text-embedding-3-small

  # Authentication configuration
  auth:
    # Scheme: env (default) | bearer | api-key | basic | oauth2 | none
    type: env
    # Environment variable holding the secret (resolved from .env or os.Getenv)
    env: OPENAI_API_KEY
    # Custom header name (defaults to "Authorization")
    header: Authorization
    # Or read secret from an explicit file path on disk
    # ("~/…" and paths relative to this manifest are expanded)
    # file: ./tokens/openai

  # Static HTTP headers attached to all requests from this provider
  headers:
    X-Client-App: TaskSmith

  # Vendor-specific engine settings (e.g. OpenAI Responses API mode and tools)
  # See the OpenAI Provider Guide for all available keys
  config:
    apiMode: responses
    store: false

  # Model catalog available from this provider
  models:
    - id: gpt-5.5
      name: gpt-5.5
      label: GPT-5.5
      aliases: [fast]            # Additional @-reference names for this model
      limits:
        context: 1050000     # Input context window in tokens
        output: 128000       # Maximum output tokens
        dimensions: 1536     # Output vector dimensions (for embedding models)
      capabilities:
        completion: true     # Supports chat completion (default: true)
        decision: true       # Supports native probabilistic decision (default: false)
        embedding: false     # Supports vector embedding (default: false)
        tools: true          # Supports function / tool calling
        attachment: true     # Supports document / image attachments
        structuredOutput: true # Supports strict JSON Schema enforcement
        temperature: true    # Supports temperature sampling
        thinking:            # Reasoning configuration strategies
          - type: effort     # "toggle" | "effort" | "budget" | "adaptive"
            default: medium
            isDefault: true
            allowedEfforts: [low, medium, high]
        modalities:
          input: [text, image]
          output: [text]
      cost:
        input: 2.5            # USD per million tokens
        output: 10.0
        cacheRead: 1.25
        cacheWrite: 2.5
```

Explicit model `capabilities.temperature` overrides the provider profile. An agent's configured temperature is sent when supported, omitted with a chat warning when unsupported, and sent with a warning when support is unknown. Anthropic extended thinking omits temperature. Workspace reload refreshes provider configuration and session warnings; `/clear` dismisses visible warnings.

Requests requiring tools, tool history, media, or a response schema fail when the model is known not to support them. Workspace capability declarations override provider profiles. Requests with an unknown structured-output capability are forwarded to Loom; media is rejected if the provider adapter cannot preserve it. Start a new session or choose a compatible model when existing history is incompatible. The send API accepts text blocks in user messages; other, unrecognized, or invalid blocks are rejected.

> [!NOTE]
> Thinking capabilities come from provider model profiles or workspace model declarations. Declared strategies replace profile strategies for that model. `/effort`, `/budget`, and `/thinking` report when the resolved model does not support the requested strategy; `/thinking` accepts `on`/`off` for a `toggle` strategy and `adaptive` for an `adaptive` one. `default` clears the session thinking override and restores the agent or model default.

### Authentication Schemes

`spec.auth.type` selects how the resolved credential is put on the wire. Matching is case-insensitive.

| `type` | Request header | Notes |
| :--- | :--- | :--- |
| `env` (default) | `Authorization: Bearer <credential>` | Default when `type` is omitted. |
| `bearer` | `Authorization: Bearer <credential>` | Same header as `env`. |
| `api-key` | `Authorization: <credential>` | Sends the credential un-prefixed. |
| `basic` | `Authorization: Basic base64(<credential>)` | Credential is usually `user:pass`. |
| `oauth2` | Dynamic / `Authorization: Bearer <token>` | Uses managed OAuth 2.0 PKCE tokens with automatic background refresh (e.g. Antigravity/Google Cloud Code), falling back to `auth.env` if set. |
| `none` | — | No credential is read or sent; use for unauthenticated or proxy-fronted upstreams. |

`auth.header` replaces `Authorization` for any scheme except `none`. When it does, the gateway removes the vendor credential header a provider SDK would otherwise add (`X-Api-Key`, `x-goog-api-key`, or its own `Authorization`), so exactly the configured header is sent.

Credential lookup order is `auth.env` then `auth.file`; declaring both uses `env`. `auth.file` accepts `~/…` and paths relative to the manifest declaring it, so a token can live next to the provider definition. Rotating a credential — a new environment value or new file contents — changes the gateway fingerprint and rebuilds provider connections.

### Thinking Capability Fields

Each entry under a model's `capabilities.thinking` declares one reasoning strategy and optionally the value to use when nothing overrides it.

| Field | Type | Description |
| :--- | :--- | :--- |
| `type` | string | Strategy: `toggle`, `effort`, `budget`, or `adaptive`. |
| `isDefault` | boolean | Marks this strategy as the model's primary default. |
| `default` | string, integer, or boolean | Value applied when nothing overrides it (for example `high`, `4096`, or `true`). |
| `allowedEfforts` | array of strings | Permitted effort levels for `type: effort`. |
| `maxBudgetTokens` | integer | Upper bound for `type: budget`. |

Effective thinking resolves in this order: the session override (`/effort`, `/budget`, `/thinking`), then the active agent's `spec.thinking`, then the model's own default. The model default is the strategy marked `isDefault`, or the first strategy declaring a usable `default`. A `default` that the model cannot honor is ignored, leaving thinking unset.

The model endpoints (`GET /v1/models` and `GET /v1/models/{id}`) return effective `capabilities`, limits, optional `pricing` in USD per million tokens, and the model's `aliases`. `GET /v1/workspace` includes the resolved default model as `default_model` when available, and session responses include their resolved `model`. Both report each strategy's `is_default` flag and a `thinking_default` setting holding the effective default. `POST /v1/sessions/preview` accepts proposed session configuration (`agent`, `model`, `thinking`, `mode`) and returns the effective configuration, resolved model, and diagnostics without creating or persisting a session. Agent preferences skip providers that are not configured; an explicit model override takes precedence. Manifest values override built-in profile values; pricing, including tiers, comes entirely from the manifest when `cost` is present. Missing context and output limits fall back to 128000 and 16384 tokens. `thinking[].values` contains objects with a required `value` and optional `description`. Common effort levels receive general hints; model-specific values may have no description. Descriptions are informational and do not change the supported values.

```json
{"type":"effort","values":[{"value":"low","description":"Less reasoning; potentially faster."},{"value":"custom"}]}
```

> [!NOTE]
> Detailed vendor-specific engine parameters (such as `apiMode`, `store: false`, and built-in server tools) are documented in the [OpenAI Provider Guide](./providers/openai.md).

---

## 3. Model Referencing Syntax

When configuring an agent, tool, or decision manifest, reference models using either qualified or unqualified syntax:

- **Qualified Reference (`<provider>/<model-id>`)**:
  ```yaml
  model: anthropic/claude-sonnet-5
  ```
  Unambiguously resolves the model from the specified provider manifest.
- **Short Name (`<provider>/<name>`)**:
  ```yaml
  model: anthropic/claude-3-7-sonnet
  ```
  Resolves to the model declaring that `name`, and reports the canonical wire `id`.
- **Bare Model ID (`<model-id>`)**:
  ```yaml
  model: gpt-5.5
  ```
  Searches all registered providers in precedence order for a model matching `id`.
- **Model Alias (`@<alias>` or `<provider>/@<alias>`)**:
  Resolves to a model's `aliases` entry or a workspace `modelAliases` target. Aliases are stored without their `@` prefix, so `fast` and `@fast` are equivalent.
- **Role Identifier (`completion`, `decision`, `embedding`)**:
  Resolves against the workspace or provider defaults configured in `WORKSPACE.md`.

References to a model declared in a provider manifest match case-insensitively, so `Anthropic/Claude-Sonnet-5` resolves to the declared `anthropic/claude-sonnet-5` with its limits, capabilities, and pricing intact. Identifiers for models not listed in any manifest are matched exactly and passed through to the provider.

A model's `name` is its WARP resource name used for referencing, and `label` is the human-readable display value. Provider manifests commonly repeat the `id` as `name`; because that carries no display information, it does not override the display name from the built-in provider profile. Set `label` (or a `name` distinct from `id`) to change what the CLI and `/model` picker show.

---

## Related Documentation

- [Gateway Architecture](../architecture/gateway.md)
- [Workspaces Guide](../guides/workspaces.md)
- [OpenAI Provider Guide](./providers/openai.md)
