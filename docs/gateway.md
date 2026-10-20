# Provider Gateway Architecture & Specification

## 1. Executive Summary

The **Provider Gateway** (`internal/gateway`) is the centralized subsystem in the TaskSmith v2 Brain daemon responsible for managing, authenticating, and instantiating artificial intelligence models across multiple upstream backends.

Under **Option 1 (Workspace Boundary Pattern)**, the Gateway is **completely decoupled from Warp manifests**. It operates strictly on strongly-typed domain configuration structures (`gateway.Config`), while `internal/workspace` acts as the translation layer converting Warp manifests and workspace settings into `gateway.Config`.

Gateways are pooled across the daemon using **content-addressable configuration hashing (`Config.Fingerprint()`)**, ensuring automatic connection reuse across worktrees and workspaces with identical configurations.

---

## 2. Architectural Principles & Invariants

### A. Total Manifest Decoupling (Anti-Corruption Layer)
- `internal/gateway` **does not import `github.com/masterkeysrd/warp`**.
- All manifest parsing, inheritance, YAML serialization, and directory watching are quarantined within `internal/workspace`.
- The Gateway accepts pure Go structs (`gateway.Config`) and instantiates concrete execution runtimes from **[Loom](https://github.com/masterkeysrd/loom)** (`llm.Model`, `decision.Model`, `embedding.Model`).

### B. Content-Addressable Pooling (`Config.Fingerprint`)
- Rather than keying gateways by mutable strings like `workspace_id`, the Gateway Pool keys instances by the canonical SHA-256 fingerprint of their effective configuration (providers, endpoints, credentials, and default models).
- **Worktree Efficiency**: Multiple `git worktree` instances sharing the same repository configuration automatically share the same Gateway and HTTP connection pools.
- **Zero Cache Invalidation Races**: Modifying a provider config produces a new hash, spawning a new Gateway entry while allowing existing in-flight turns on the prior gateway to drain cleanly.

### C. Zero Magic Fallbacks for Authentication
- **No Global Environment Probing**: The Gateway **never** reads `os.Getenv` or inspects process-level environment variables.
- **No Unmanaged File Traversal**: The Gateway does not search the filesystem for unreferenced `.env` files.
- **Explicit Injected Credentials**: Credentials are read strictly from an explicit in-memory map (`Config.Env map[string]string`). If a provider specifies `Auth.Env: "ANTHROPIC_API_KEY"` and the key is missing or blank in `Config.Env`, initialization immediately returns a descriptive error.

### D. Triple-System Workload Architecture
AI workloads in TaskSmith v2 are divided into three distinct operational domains:

1. **System One (Decisions)**: Fast, calibrated probabilistic evaluation models implemented via `loom/decision.Model`. Used for classification, routing, and dynamic flow branching.
2. **System Two (Completions & Reasoning)**: Multi-turn, tool-calling conversational and reasoning agents implemented via `loom/llm.Model`.
3. **System Three (Embeddings)**: High-throughput dense vector embeddings implemented via `loom/embedding.Model`. Used for semantic indexing, memory, and RAG.

---

## 3. Subsystem Architecture

```mermaid
flowchart TD
    subgraph WorkspaceLayer ["internal/workspace"]
        WarpLoader["Warp Loader\n(.agents/providers/*.yaml)"]
        EnvLoader["Workspace Secrets\n(in-memory map[string]string)"]
        Converter["Warp -> gateway.Config Converter"]
        WarpLoader --> Converter
        EnvLoader --> Converter
    end

    subgraph GatewaySubsystem ["internal/gateway"]
        Pool["gateway.Pool\n(Content-Addressable by Fingerprint)"]
        GW["gateway.Gateway"]
        AuthResolver["Strict Auth Validator\n(matches Config.Env)"]
        AdaptEngine["Decision Adapter Engine\n(decision.NewFromLLM)"]
        
        Pool -->|Acquire(cfg)| GW
        GW --> AuthResolver
    end

    subgraph UpstreamProviders ["Loom Providers (github.com/masterkeysrd/loom)"]
        Anthropic["anthropic (Claude 3.7)"]
        OpenAI["openai (GPT / o-series)"]
        Ollama["ollama (nimble-v3 / llama3)"]
        TypeSafe["typesafe (jev-latest)"]
        GenAI["genai (Gemini 2.5)"]
        Antigravity["antigravity (Enterprise)"]
    end

    Converter -->|gateway.Config| Pool
    GW -->|Native Decision / LLM / Embedding| UpstreamProviders
    GW -->|Fallback Adapter| AdaptEngine
    AdaptEngine --> UpstreamProviders
```

---

## 4. Supported Providers & Capabilities

| Provider Type | Loom Package | Native Capabilities | Notes |
| :--- | :--- | :--- | :--- |
| `anthropic` | `providers/anthropic` | Completion, Tools, Reasoning | Primary System Two reasoning model |
| `openai` | `providers/openai` | Completion, Decision, Embedding, Tools | System One, Two, and Three |
| `ollama` | `providers/ollama` | Completion, Decision (`nimble-v3`), Embedding | Local offline inference |
| `typesafe` | `providers/typesafe` | Decision (`jev-latest`) | High-speed deterministic decision backend |
| `genai` | `providers/genai` | Completion, Embedding, Tools | Multimodal & large context processing |
| `antigravity`| `providers/antigravity`| Completion, Tools | Internal enterprise gateway |

---

## 5. Workload Roles & Model Resolution

Model resolution uses role-based precedence:

```text
Explicit Override  ──>  Role Default (Config.DefaultModels)  ──>  Provider Default
```

### System One: Decision Resolution (`RoleDecision`)
1. Checks if the requested (or defaulted) model is provided by a backend supporting native decision evaluation (`Capabilities.Decision`).
2. If native: instantiates `*decision.Model` via `decision.NewModel`.
3. If the backend only supports completion: instantiates the underlying `*llm.Model` and adapts it via `decision.NewFromLLM(llmModel, cfg)`.

### System Two: Completion Resolution (`RoleCompletion`)
1. Resolves candidate model matching `RoleCompletion`.
2. Confirms model supports text/chat completions (`Capabilities.SupportsCompletion()`).
3. Instantiates `*llm.Model` with telemetry middleware.

### System Three: Embedding Resolution (`RoleEmbedding`)
1. Resolves candidate model matching `RoleEmbedding`.
2. Confirms model supports vector embeddings (`Capabilities.SupportsEmbedding()`).
3. Instantiates `*embedding.Model`.

---

## 6. Public Go API (`internal/gateway`)

### Core Types & Configuration
```go
type ModelRole string

const (
    RoleCompletion ModelRole = "completion"
    RoleDecision   ModelRole = "decision"
    RoleEmbedding  ModelRole = "embedding"
)

type Config struct {
    Providers     []ProviderConfig           `json:"providers"`
    DefaultModels map[ModelRole]string       `json:"default_models,omitempty"`
    Env           map[string]string          `json:"env,omitempty"`
}

func (c Config) Fingerprint() string
```

### Gateway & Pool Interfaces
```go
type Gateway interface {
    ResolveLLM(ctx context.Context, explicit string, opts ...Option) (*llm.Model, error)
    ResolveDecision(ctx context.Context, explicit string, opts ...Option) (*decision.Model, error)
    ResolveEmbedding(ctx context.Context, explicit string, opts ...Option) (*embedding.Model, error)
    ListProviders() []ProviderInfo
    Close() error
}

type Pool interface {
    Acquire(cfg Config) (Gateway, error)
    Close() error
}
```
