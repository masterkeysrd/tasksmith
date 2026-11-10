# Google GenAI Provider

The **Google GenAI** provider connects TaskSmith to Google's Gemini family of models via the official Google GenAI Go SDK and Gemini API (`https://generativelanguage.googleapis.com/v1beta`).

---

## 1. Overview & Capabilities

Google GenAI models offer 1M+ token context windows, native multimodal inputs, and high-speed vector embeddings:
- **Completion (`completion` — System 2)**: Massive context ingestion, conversational reasoning, and tool execution using `gemini-4-carbon`, `gemini-4-argon`, and `gemini-3.8-flash`.
- **Decision (`decision` — System 1)**: Evaluates structured decision prompts via TaskSmith's completion fallback adapter.
- **Embedding (`embedding` — System 3)**: Fast semantic vector generation using `text-embedding-004` (768 dimensions).
- **Type Aliases**: TaskSmith accepts `type: google-genai`, `type: genai`, and `type: gemini` as equivalent aliases in manifests.

---

## 2. Quick Start

### Step 1: Set Your API Key

Define `GOOGLE_API_KEY` (or `GEMINI_API_KEY`) in your workspace `.env`:

```bash
GOOGLE_API_KEY=AIzaSy...
```

### Step 2: Configure Provider Manifest

Create `.agents/providers/google-genai.yaml`:

```yaml
apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: google-genai
spec:
  type: google-genai
  endpoint: https://generativelanguage.googleapis.com/v1beta
  defaultModels:
    completion: gemini-4-carbon
    embedding: text-embedding-004
  auth:
    type: env
    env: GOOGLE_API_KEY
  models:
    - id: gemini-4-carbon
      name: gemini-4-carbon
      label: Gemini 4 Carbon
      limits:
        context: 1048576
        output: 65536
      capabilities:
        completion: true
        tools: true
        attachment: true
        thinking:
          - type: effort
            default: high
            isDefault: true
            allowedEfforts: [low, medium, high]
        modalities:
          input: [text, image, audio, video]
          output: [text]
    - id: gemini-4-argon
      name: gemini-4-argon
      label: Gemini 4 Argon
      limits:
        context: 1048576
        output: 65536
      capabilities:
        completion: true
        tools: true
    - id: gemini-3.8-flash
      name: gemini-3.8-flash
      label: Gemini 3.8 Flash
      limits:
        context: 1048576
        output: 65536
      capabilities:
        completion: true
        tools: true
    - id: text-embedding-004
      name: text-embedding-004
      label: Text Embedding 004
      limits:
        context: 2048
        dimensions: 768
      capabilities:
        completion: false
        embedding: true
```

---

## 3. Large Context Windows & Thinking Effort

Gemini models feature large context windows, ideal for repository-wide refactoring passes and multimodal asset analysis. Use `/effort` or `/budget` when supported by the active model.

For the complete schema of capability flags and token limits, see the [Providers Overview](../providers.md).

### Custom Endpoints and Proxies

`spec.endpoint` overrides the Google endpoint, which lets an authenticating proxy or self-hosted gateway front Gemini traffic. A trailing API version segment is read as the API version rather than repeated, so both forms below request `…/v1beta/models/…`:

```yaml
spec:
  type: google-genai
  endpoint: https://proxy.example.com/v1beta   # version read from the path
  # endpoint: https://proxy.example.com        # or keep the SDK default v1beta
```

When the proxy authenticates requests itself, set `auth.type: none` to send no credential, or point `auth.header` at the header the proxy expects; the SDK's own `x-goog-api-key` header is dropped in both cases.

---

## Related Documentation

- [Providers Overview & Manifest Reference](../providers.md)
- [Gateway Architecture](../../architecture/gateway.md)
- [OpenAI Provider Guide](./openai.md)
