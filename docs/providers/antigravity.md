# Antigravity Provider

The **Antigravity** provider connects TaskSmith to the Antigravity developer platform and enterprise model gateway (`https://daily-cloudcode-pa.googleapis.com`).

---

## 1. Overview & Capabilities

Designed for enterprise environments and Google internal developer workflows, the Antigravity provider provides authenticated model access with managed thinking effort:
- **Completion (`completion` — System 2)**: Conversational reasoning, software engineering workflows, and tool execution using `gemini-3.8-flash`, `gemini-3.7-flash`, and `gemini-2.5-pro`.
- **Decision (`decision` — System 1)**: Evaluates structured decisions via TaskSmith's completion fallback adapter.
- **Managed Reasoning & Effort**: Integrated support for low, medium, and high thinking effort tiers.

---

## 2. Quick Start

### Step 1: Set Your Auth Token

Define `ANTIGRAVITY_TOKEN` in your environment:

```bash
ANTIGRAVITY_TOKEN=ya29.a0AfH6...
```

### Step 2: Configure Provider Manifest

Create `.agents/providers/antigravity.yaml`:

```yaml
apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: antigravity
spec:
  type: antigravity
  endpoint: https://daily-cloudcode-pa.googleapis.com
  defaultModel: gemini-3.8-flash
  auth:
    type: env
    env: ANTIGRAVITY_TOKEN
  models:
    - id: gemini-3.8-flash
      name: gemini-3.8-flash
      label: Gemini 3.8 Flash
      limits:
        context: 1048576
        output: 65536
      capabilities:
        completion: true
        tools: true
        temperature: true
        thinking:
          - type: effort
            default: high
            isDefault: true
            allowedEfforts: [low, medium, high]
    - id: gemini-3.7-flash
      name: gemini-3.7-flash
      label: Gemini 3.7 Flash
      limits:
        context: 1048576
        output: 65536
      capabilities:
        completion: true
        tools: true
        temperature: true
        thinking:
          - type: effort
            default: high
            isDefault: true
            allowedEfforts: [low, medium, high]
    - id: gemini-2.5-pro
      name: gemini-2.5-pro
      label: Gemini 2.5 Pro
      limits:
        context: 1048576
        output: 65536
      capabilities:
        completion: true
        tools: true
        temperature: true
        thinking:
          - type: effort
            default: high
            isDefault: true
            allowedEfforts: [low, medium, high]
```

---

## 3. Reasoning & Effort Levels

All models in the Antigravity catalog include thinking capability declarations. Adjust reasoning depth dynamically via `/effort <low|medium|high>` or set `effort` directly in agent definitions.

For the complete schema of capabilities, costs, and token limits, see the [Providers Overview](../providers.md).

---

## Related Documentation

- [Providers Overview & Manifest Reference](../providers.md)
- [Gateway Architecture](../gateway.md)
- [Google GenAI Provider Guide](./google-genai.md)
