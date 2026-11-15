// Package gateway manages model provider lifecycles and instantiates client
// interfaces for LLM completions, probabilistic decision models, and dense
// vector embeddings.
//
// The gateway is completely decoupled from configuration manifest formats (such as
// Warp). It operates purely on strongly-typed in-memory domain configurations
// ([Config]), leaving manifest parsing, inheritance, and disk watching to the
// workspace layer.
//
// # Content-Addressable Gateway Pool
//
// To maximize connection reuse and eliminate cache invalidation bugs, gateways
// are pooled by a deterministic configuration hash ([Config.Fingerprint]).
// Workspaces, worktrees, or subagents with identical provider configurations and
// credentials automatically share the same [Gateway] instance and HTTP connection
// pools. When configurations change, the pool instantiates a new gateway entry
// while gracefully draining previous instances.
//
// # Workload Systems
//
// The gateway supports three distinct AI workload systems:
//
//   - System One (Decision): Fast, calibrated probabilistic evaluation models
//     represented by [github.com/masterkeysrd/loom/decision.Model]. When a provider
//     natively supports decision evaluation (such as Ollama with nimble-v3 or TypeSafe
//     with jev-latest), the gateway constructs a native decision backend. For providers
//     or models supporting only text completion, the gateway transparently wraps the
//     completion model using Loom's structured decision adapter ([decision.NewFromLLM]).
//
//   - System Two (Completion): Conversational, reasoning, and tool-augmented models
//     represented by [github.com/masterkeysrd/loom/llm.Model]. These models power
//     agent planning, interactive dialogues, and complex execution loops.
//
//   - System Three (Embedding): Dense vector representation models represented by
//     [github.com/masterkeysrd/loom/embedding.Model], supporting similarity search,
//     retrieval-augmented generation (RAG), and semantic indexing.
//
// # Authentication Guarantees
//
// In TaskSmith v2, security and deterministic behavior are paramount. The gateway
// enforces strict authentication invariants:
//
//   - Zero Implicit Fallbacks: The gateway never inspects process-level environment
//     variables (via os.Getenv) or searches the local filesystem for dot-env files.
//   - Explicit Credential Injection: All environment credentials must be passed via
//     the injected in-memory map on [Config.Env]. If a required credential key is missing
//     or empty, initialization fails immediately with a descriptive error.
//   - File Authentication: If a credential file path is specified, it must point to
//     an explicit, readable file.
package gateway
