# Ollama Provider

The **Ollama** provider enables completely local, private, and offline AI workloads in TaskSmith by connecting directly to an [Ollama](https://ollama.com) daemon running on your workstation or internal network (`http://localhost:11434`).

---

## 1. Overview & Capabilities

Ollama requires zero external network egress, charges zero API token fees, and runs entirely on local hardware across all three architectural roles:
- **Completion (`completion` — System 2)**: Local conversational reasoning and tool execution using models like `qwen3.8`, `nemotron-3.5-lightning`, `gemma4`, and `llama3.3`.
- **Decision (`decision` — System 1)**: Native probabilistic decision evaluation using specialized weights (`nimble-v3`) or completion fallback.
- **Embedding (`embedding` — System 3)**: Fast local dense vector generation using dedicated embedding weights (`nomic-embed-text`, `bge-m3`) for codebase indexing.

---

## 2. Quick Start

### Step 1: Pull Local Models

Verify Ollama is running and pull the desired models:

```bash
ollama serve
ollama pull qwen3.8
ollama pull nimble-v3
ollama pull nomic-embed-text
```

### Step 2: Configure Provider Manifest

Create `.agents/providers/ollama.yaml`:

```yaml
apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: ollama
spec:
  type: ollama
  endpoint: http://localhost:11434
  defaultModel: qwen3.8
  defaultModels:
    completion: qwen3.8
    decision: nimble-v3
    embedding: nomic-embed-text
  models:
    - id: qwen3.8
      name: qwen3.8
      label: Qwen 3.8
      limits:
        context: 262144
        output: 32768
      capabilities:
        completion: true
        tools: true
    - id: nimble-v3
      name: nimble-v3
      label: Nimble v3 Decision
      limits:
        context: 32768
      capabilities:
        completion: false
        decision: true
    - id: nomic-embed-text
      name: nomic-embed-text
      label: Nomic Embed Text
      limits:
        context: 8192
        dimensions: 768
      capabilities:
        completion: false
        embedding: true
    - id: llama3.3
      name: llama3.3
      label: Llama 3.3
      limits:
        context: 128000
        output: 4096
      capabilities:
        completion: true
        tools: true
```

> [!NOTE]
> Standard local Ollama instances require no authentication. The `auth` block can be safely omitted for `http://localhost:11434`.

---

## 3. Thinking Controls

TaskSmith uses the resolved Ollama model profile to determine which thinking controls are available. Explicit `capabilities.thinking` declarations in the provider manifest replace the profile's thinking strategies.

| Command | Availability | Behavior |
| :--- | :--- | :--- |
| `/effort [level\|default]` | Models with effort levels | Select a model-supported level, or open the picker without an argument. |
| `/thinking [on\|off\|default]` | Models with a thinking toggle | Show or change whether thinking is enabled. |
| `/budget [tokens\|default]` | Models with a thinking budget | Show or set a model-supported thinking token budget. |

All three commands remain visible, even when the active model does not support them. `default` clears the session's thinking override and restores the agent or provider default. Setting one control replaces the previous thinking override.

See the [CLI Reference](../cli.md) for command details and the [Providers Overview](../providers.md) for manifest properties.

---

## 4. Remote Ollama Instances & Authentication

For remote GPU workstations or cloud-hosted Ollama servers requiring authentication, specify `spec.endpoint` and configure Bearer or Basic auth:

```yaml
spec:
  endpoint: https://ollama.internal.corp:11434
  auth:
    type: bearer
    env: OLLAMA_AUTH_TOKEN
```

For general manifest properties and `.env` secret resolution, see the [Providers Overview](../providers.md).

---

## Related Documentation

- [Providers Overview & Manifest Reference](../providers.md)
- [Gateway Architecture](../../architecture/gateway.md)
- [OpenAI Provider Guide](./openai.md)
