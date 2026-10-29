# Model providers

A ModelProvider describes a gateway driver, its connection settings, available models, and defaults. Definitions are discovered through the system, user, workspace, and current-project hierarchy, commonly in `.agents/providers/<name>.provider.yaml`. The resource name identifies the configured provider; `spec.type` selects the driver.

TaskSmith ships provider presets as templates. A preset being present does not itself make it an active provider. Gateway types include `openai`, `anthropic`, `ollama`, `typesafe`, `antigravity`, and `google-genai` (also `genai` or `gemini`). Provider support for completion, decision, and embedding roles differs; a capability declaration cannot add a role the driver does not implement. Decision model support in the gateway does not imply TaskSmith can evaluate Decision resources.

## Provider definition

| Field | Type | Meaning |
| --- | --- | --- |
| `spec.type` | string | Gateway driver; required by TaskSmith. |
| `spec.endpoint` | string | API base endpoint; handling and defaults depend on the driver. |
| `spec.defaultModel` | string | Default completion model. |
| `spec.defaultModels` | object | Role-specific defaults: `completion`, `decision`, and `embedding`. |
| `spec.auth` | object | Credential source and authentication scheme. |
| `spec.headers` | map | Additional HTTP headers. |
| `spec.config` | map | Driver-specific options. |
| `spec.models` | object[] | Model catalog and descriptive metadata. |

Endpoint and configuration support varies by driver. For example, Ollama has a local default endpoint when omitted; the Google GenAI adapter does not currently pass the manifest endpoint into its client. TaskSmith reads selected OpenAI `config` options such as `apiMode` and `store`; arbitrary keys, such as generic retry or timeout settings, are not necessarily applied. Custom headers are added when the outgoing request does not already have that header.

## Model resolution

A model identifier commonly has the form `provider/model`. Workspace resolution considers explicit overrides, workspace role defaults, the configured default provider's role defaults, then other effective providers' defaults and capability matches. `defaultModel` is the completion fallback; `defaultModels` can separately select `completion`, `decision`, and `embedding`. The gateway also considers provider availability and authentication when resolving models. See `workspace-and-context.md` for workspace precedence.

## Authentication and environment

| `spec.auth` field | Type | Meaning |
| --- | --- | --- |
| `type` | string | HTTP scheme such as `bearer`, `api-key`, or `basic`; an empty type uses bearer for the gateway's HTTP transport. |
| `header` | string | Optional authentication header name; defaults to `Authorization` in that transport. |
| `env` | string | Environment variable containing the credential. |
| `file` | string | Path to a file containing the credential. |

When `env` is set, the gateway reads the workspace environment and falls back to the host environment; a missing or empty value produces a credential error. If both `env` and `file` are set, `env` is used and a missing value does not fall back to the file. File credentials must be readable and nonempty. Provider SDKs may also handle the resolved token using their own authentication conventions; shipped presets use `type: env` with an environment variable.

TaskSmith builds its workspace environment from the process environment and `.env` files: user configuration first, applicable ancestor files from outer to inner, workspace root, then current directory. Later layers override earlier values. Reference secrets through environment variables or files instead of embedding them in manifests or headers.

## Model entries

| Field | Type | Meaning |
| --- | --- | --- |
| `id` | string | Model identifier used in `provider/model` references. |
| `name`, `label` | string | Model name and human-readable label. |
| `limits` | object | Token and embedding-vector limits. |
| `capabilities` | object | Supported roles, features, and configuration boundaries. |
| `cost` | object | Pricing metadata and conditional tiers. |

`limits.context` is the context token limit, `limits.output` the output token limit, and `limits.dimensions` the default or maximum embedding vector size. Supply values matching the actual provider; a manifest does not make an unsupported model available.

### Capabilities

| Field | Type | Meaning |
| --- | --- | --- |
| `completion`, `decision`, `embedding` | bool | Model roles. For role matching, omitted `completion` defaults to true; `decision` and `embedding` default to false. |
| `dimensions` | int[] | Supported configurable embedding dimensions. |
| `attachment`, `tools` | bool | Attachment and tool-calling support. |
| `structuredOutput`, `temperature` | bool | Structured-output and sampling-temperature support. |
| `modalities` | object | `input` and `output` arrays of modality names such as `text` and `image`. |
| `thinking` | object[] | Supported reasoning strategies. |

A thinking entry has `type` (`toggle`, `effort`, `budget`, or `adaptive`), optional `isDefault`, `allowedEfforts`, `maxBudgetTokens`, and a strategy-specific `default`. Omitted optional feature flags are unspecified metadata, not proof of support.

The workspace preserves all these schema fields. Gateway conversion forwards completion, decision, embedding, attachment, tools, temperature, dimensions, and selected thinking details. Explicit temperature capability values override the provider profile; if neither source specifies support, it is unknown. The gateway does not forward `structuredOutput`, `modalities`, or the thinking `isDefault` flag to the gateway model configuration. The actual provider and model still determine supported features.

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
  defaultModel: example-model
  auth:
    type: env
    env: OPENAI_API_KEY
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
