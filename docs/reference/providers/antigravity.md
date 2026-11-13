# Antigravity Provider

The **Antigravity** provider connects TaskSmith to the Antigravity developer platform and enterprise model gateway (`https://daily-cloudcode-pa.googleapis.com`).

---

## 1. Overview & Capabilities

Designed for enterprise environments and Google internal developer workflows, the Antigravity provider provides authenticated model access with managed thinking effort:
- **Completion (`completion` — System 2)**: Conversational reasoning, software engineering workflows, and tool execution using `gemini-4-hydrogen`, `gemini-4-carbon`, `gemini-4-argon`, and `gemini-3.8-flash`.
- **Decision (`decision` — System 1)**: Evaluates structured decisions via TaskSmith's completion fallback adapter.
- **Managed Reasoning & Effort**: Integrated support for low, medium, and high thinking effort tiers, plus `xhigh` on `gemini-4-hydrogen`.
- **Sticky Session Prompt Caching**: Automatically pins `request.session_id` and `user_prompt_id` on conversational and compaction turns so sequential requests route to the same backend replica and hit its implicit KV prefix cache.

---

## 2. Authentication & Quick Start

The Antigravity provider supports both interactive OAuth 2.0 PKCE authentication and static environment token fallback.

### Option A: Interactive Browser OAuth (Recommended)

Run the interactive setup wizard in TaskSmith:

```bash
tasksmith
# Then run:
/setup
```

Select **Antigravity** as your provider and choose **Log in with Google (Browser)**:
1. TaskSmith opens your browser to sign in with your Google account.
2. The authorization flow uses PKCE on `http://localhost:51121/callback`.
3. Tokens are securely persisted with `0600` permissions at `~/.local/share/tasksmith/credentials/antigravity.json`.
4. Token expiration is handled automatically via background refresh.
5. Your authenticated Google account email is detected and surfaced in the setup wizard and daemon status. You can switch accounts or re-authenticate at any time.

### Option B: Static Environment Variable

Alternatively, define `ANTIGRAVITY_TOKEN` in your environment or workspace `.env` file:

```bash
ANTIGRAVITY_TOKEN=ya29.a0AfH6...
```

When set, `ANTIGRAVITY_TOKEN` overrides any on-disk OAuth token for headless environments or automated CI pipelines.

---

## 3. Provider Manifest

Create `.agents/providers/antigravity.yaml`:

```yaml
apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: antigravity
spec:
  type: antigravity
  endpoint: https://daily-cloudcode-pa.googleapis.com
  defaultModels:
    completion: gemini-4-hydrogen
  auth:
    type: oauth2
    env: ANTIGRAVITY_TOKEN
  models:
    - id: gemini-4-hydrogen
      name: gemini-4-hydrogen
      label: Gemini 4 Hydrogen
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
            allowedEfforts: [low, medium, high, xhigh]
    - id: gemini-4-carbon
      name: gemini-4-carbon
      label: Gemini 4 Carbon
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
    - id: gemini-4-argon
      name: gemini-4-argon
      label: Gemini 4 Argon
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
```

---

## 4. Provider Configuration (`spec.config`)

All `spec.config` fields are optional. By default, TaskSmith sends a versioned Antigravity CLI `User-Agent` and automatically discovers your account's Cloud AI Companion project via `/v1internal:loadCodeAssist`.

| Key | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `companionProject` | string | *(auto-discovered)* | Explicit Cloud AI Companion project ID sent in the JSON request body (`project` field), bypassing `/v1internal:loadCodeAssist` discovery without setting `X-Goog-User-Project`. |
| `projectId` | string | `""` | Explicit Google Cloud billing project ID sent in the `X-Goog-User-Project` HTTP header. Leave unset for standard Antigravity accounts. |
| `userAgent` | string | `antigravity/cli/2.0.0 (<os>/<arch>)` | Custom `User-Agent` applied to both the HTTP header and `RequestEnvelope.userAgent` (takes precedence over `spec.headers["User-Agent"]`). |
| `aliases` | map of strings | `{}` | Provider-level model aliases mapping custom identifiers to backend model IDs (for example `fast: gemini-3.8-flash-low`). |

---

## 5. Reasoning & Effort Levels

Models may declare thinking capabilities. Use `/effort` or `/budget` when supported, or set thinking defaults in agent definitions.

For the complete schema of capabilities, costs, and token limits, see the [Providers Overview](../providers.md).

---

## Related Documentation

- [Providers Overview & Manifest Reference](../providers.md)
- [Gateway Architecture](../../architecture/gateway.md)
- [Google GenAI Provider Guide](./google-genai.md)
