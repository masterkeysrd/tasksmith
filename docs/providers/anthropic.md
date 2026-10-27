# Anthropic Provider

The **Anthropic** provider connects TaskSmith to Anthropic's Claude family of models via the Anthropic Messages API (`https://api.anthropic.com`).

---

## 1. Overview & Capabilities

Anthropic models serve as primary System 2 reasoning and conversational agents in TaskSmith:
- **Completion (`completion` — System 2)**: Multi-turn reasoning, coding, and tool execution using models such as `claude-sonnet-5`, `claude-opus-5`, and `claude-haiku-4-5`.
- **Decision (`decision` — System 1)**: Evaluates classification and routing via TaskSmith's completion fallback adapter, wrapping Claude with structured output schemas.
- **Extended Thinking & Prompt Caching**: Natively supports thinking effort levels (`thinking.type: effort`) and automatic prompt prefix caching for reduced latency and token cost.

---

## 2. Quick Start

### Step 1: Set Your API Key

Define `ANTHROPIC_API_KEY` in your workspace `.env` (or globally in `~/.config/tasksmith/.env`):

```bash
ANTHROPIC_API_KEY=sk-ant-api03-...
```

### Step 2: Configure Provider Manifest

Create `.agents/providers/anthropic.yaml`:

```yaml
apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: anthropic
spec:
  type: anthropic
  endpoint: https://api.anthropic.com
  defaultModel: claude-sonnet-5
  auth:
    type: env
    env: ANTHROPIC_API_KEY
  models:
    - id: claude-sonnet-5
      name: claude-sonnet-5
      label: Claude 5 Sonnet
      limits:
        context: 1000000
        output: 128000
      capabilities:
        completion: true
        tools: true
        attachment: true
        thinking:
          - type: effort
            default: medium
            isDefault: true
            allowedEfforts: [low, medium, high]
      cost:
        input: 0.000003
        output: 0.000015
        cacheRead: 0.0000003
        cacheWrite: 0.00000375
    - id: claude-opus-5
      name: claude-opus-5
      label: Claude 5 Opus
      limits:
        context: 1000000
        output: 128000
      capabilities:
        completion: true
        tools: true
        thinking:
          - type: effort
            default: high
            isDefault: true
            allowedEfforts: [low, medium, high]
    - id: claude-haiku-4-5
      name: claude-haiku-4-5
      label: Claude 4.5 Haiku
      limits:
        context: 200000
        output: 64000
      capabilities:
        completion: true
        tools: true
```

---

## 3. Extended Thinking & Prompt Caching

- **Thinking Effort**: Models configured with `thinking.type: effort` support dynamic session-level reasoning intensity via `/effort <low|medium|high>`.
- **Prompt Caching**: Claude models automatically cache system prompts and conversation history. Declare `cost.cacheRead` and `cost.cacheWrite` to track savings in session metrics.

For the complete manifest schema and capability properties, see the [Providers Overview](../providers.md).

---

## 4. Custom Endpoints & Reverse Proxies

For enterprise setups using AWS Bedrock, Google Vertex AI, or an internal API gateway, set `spec.endpoint`, `spec.headers`, and `spec.auth.type: bearer`:

```yaml
spec:
  endpoint: https://anthropic-proxy.corp.internal.net
  headers:
    X-Corporate-ID: engineering-ai
  auth:
    type: bearer
    env: CORP_ANTHROPIC_TOKEN
```

---

## Related Documentation

- [Providers Overview & Manifest Reference](../providers.md)
- [Gateway Architecture](../gateway.md)
- [OpenAI Provider Guide](./openai.md)
