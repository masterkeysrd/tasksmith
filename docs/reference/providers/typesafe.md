# TypeSafe Provider

The **TypeSafe** provider integrates TaskSmith with TypeSafe AI's high-speed, calibrated decision models via `https://api.typesafe.ai`.

---

## 1. Overview & Capabilities

Unlike conversational models designed for text generation (System 2), TypeSafe's **Jev** engine is purpose-built for **System 1 probabilistic decision evaluation**:
- **Role: Decision (`decision` — System 1)**: Evaluates structured classifications, security gate checks, tool call approvals, and workflow routing in under 50 milliseconds.
- **Deterministic Calibration**: Returns mathematically calibrated probabilities rather than free-form conversational tokens at a fraction of the cost.
- **Dedicated Workload**: TypeSafe models do **not** support conversational chat (`completion: false`). They are designed to be paired alongside a completion provider (such as Anthropic, OpenAI, or Google GenAI).

---

## 2. Quick Start

### Step 1: Set Your API Key

Define `TYPESAFE_API_KEY` in your workspace `.env`:

```bash
TYPESAFE_API_KEY=ts_live_...
```

### Step 2: Configure Provider Manifest

Create `.agents/providers/typesafe.yaml`:

```yaml
apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: typesafe
spec:
  type: typesafe
  endpoint: https://api.typesafe.ai
  defaultModels:
    decision: jev-latest
  auth:
    type: bearer
    env: TYPESAFE_API_KEY
  models:
    - id: jev-latest
      name: jev-latest
      label: Jev Latest
      limits:
        context: 64000
      capabilities:
        completion: false
        decision: true
    - id: jev-preview
      name: jev-preview
      label: Jev Preview
      limits:
        context: 64000
      capabilities:
        completion: false
        decision: true
```

---

## 3. Workspace Pairing Example

Pair TypeSafe with a completion provider in your `WORKSPACE.md` to run System 1 and System 2 workloads on their optimal engines:

```markdown
---
name: production-workspace
models:
  completion:
    - anthropic/claude-sonnet-5
  decision:
    - typesafe/jev-latest
---
```

When an agent executes a declarative decision prompt (`*.decision.md`) or checks permission heuristics, the gateway routes directly to TypeSafe's native decision API without invoking large generative models.

For general manifest properties and `.env` secret resolution, see the [Providers Overview](../providers.md).

---

## Related Documentation

- [Providers Overview & Manifest Reference](../providers.md)
- [Gateway Architecture](../../architecture/gateway.md)
- [Anthropic Provider Guide](./anthropic.md)
- [OpenAI Provider Guide](./openai.md)
