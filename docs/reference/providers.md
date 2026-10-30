# Providers Overview & Manifest Reference

In TaskSmith, **Model Providers** represent the upstream AI engines—such as OpenAI, Anthropic, Ollama, Google GenAI, and TypeSafe—that power agents, tools, and semantic embeddings.

TaskSmith decouples agents and skills from specific vendor APIs. For details on how workspaces resolve model roles, load hierarchical `.env` secrets, and prioritize configuration tiers, see [Workspaces](../guides/workspaces.md).

---

## 1. Supported Providers

TaskSmith includes built-in drivers and presets for the following providers:

| Provider | Type Identifier(s) | Supported Roles | Best Used For | Guide |
| :--- | :--- | :--- | :--- | :--- |
| **OpenAI** | `openai` | `completion`, `decision`, `embedding` | Frontier reasoning (`gpt-5.5`, `o3`), coding (`codex`), Responses API tools, embeddings | [OpenAI Guide](./providers/openai.md) |
| **Anthropic** | `anthropic` | `completion`, `decision` (fallback) | Extended thinking, deep reasoning (`claude-sonnet-5`, `claude-opus-5`), prompt caching | [Anthropic Guide](./providers/anthropic.md) |
| **Ollama** | `ollama` | `completion`, `decision`, `embedding` | 100% offline local inference (`qwen3.8`, `nimble-v3`, `nomic-embed-text`), zero data egress | [Ollama Guide](./providers/ollama.md) |
| **Google GenAI** | `google-genai`, `genai`, `gemini` | `completion`, `embedding` | Massive 1M+ context window (`gemini-3.1-pro`, `gemini-3.8-flash`), multimodal inputs | [Google GenAI Guide](./providers/google-genai.md) |
| **TypeSafe** | `typesafe` | `decision` | Ultra-fast native System 1 decisions (`jev-latest`), sub-50ms deterministic classifications | [TypeSafe Guide](./providers/typesafe.md) |
| **Antigravity** | `antigravity` | `completion` | Enterprise model gateway with managed thinking effort tiers (`gemini-3.8-flash`, `gemini-2.5-pro`) | [Antigravity Guide](./providers/antigravity.md) |

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

  # Default completion model if none specified
  defaultModel: gpt-5.5

  # Role-based default models for this provider (completion, decision, embedding)
  defaultModels:
    completion: gpt-5.5
    decision: gpt-5.5
    embedding: text-embedding-3-small

  # Authentication configuration
  auth:
    # Scheme: env (default) | bearer | api-key | basic
    type: env
    # Environment variable holding the secret (resolved from .env or os.Getenv)
    env: OPENAI_API_KEY
    # Custom header name (defaults to "Authorization")
    header: Authorization
    # Or read secret from an explicit file path on disk
    # file: /path/to/token

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
          - type: effort     # "effort" | "budget"
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
> Thinking capabilities come from provider model profiles or workspace model declarations. Declared strategies replace profile strategies for that model. `/effort`, `/budget`, and `/thinking` report when the resolved model does not support the requested strategy. `default` clears the session thinking override and restores the agent or provider default.

The model endpoints (`GET /v1/models` and `GET /v1/models/{id}`) return effective `capabilities`, limits, and optional `pricing` in USD per million tokens. `GET /v1/workspace` includes the resolved default model as `default_model` when available, and session responses include their resolved `model`. `POST /v1/sessions/preview` accepts proposed session configuration (`agent`, `model`, `thinking`, `mode`) and returns the effective configuration, resolved model, and diagnostics without creating or persisting a session. Agent preferences skip providers that are not configured; an explicit model override takes precedence. Manifest values override built-in profile values; pricing, including tiers, comes entirely from the manifest when `cost` is present. Missing context and output limits fall back to 128000 and 16384 tokens. `thinking[].values` contains objects with a required `value` and optional `description`. Common effort levels receive general hints; model-specific values may have no description. Descriptions are informational and do not change the supported values.

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
- **Bare Model ID (`<model-id>`)**:
  ```yaml
  model: gpt-5.5
  ```
  Searches all registered providers in precedence order for a model matching `id`.
- **Role Identifier (`completion`, `decision`, `embedding`)**:
  Resolves against the workspace or provider defaults configured in `WORKSPACE.md`.

---

## Related Documentation

- [Gateway Architecture](../architecture/gateway.md)
- [Workspaces Guide](../guides/workspaces.md)
- [OpenAI Provider Guide](./providers/openai.md)
