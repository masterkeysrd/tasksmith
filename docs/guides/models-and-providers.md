# Connect providers and choose models

Connect a provider, choose the model for your next turn, and use aliases to
give everyday work, reviews and permission checks different defaults.

## Connect and authenticate a provider

Open the provider browser in the composer (for shell-based `tasksmith auth` sign-in, make sure the daemon is running with `tasksmith daemon start`):

```text
/providers
```

Press `a` to add a provider, choose a preset, select the scope and submit
**Create provider**. For API-key presets, enter the key in the form, not
in the manifest or chat. TaskSmith puts it in the scope's `.env` and stores
an environment reference in the YAML. Keep workspace `.env` files untracked.

| If you have… | Choose | How to connect |
| --- | --- | --- |
| An OpenAI or Anthropic API account | `openai` or `anthropic` | Supply the API key for that service. These are distinct from subscription sign-ins. |
| An eligible ChatGPT subscription | `codex` | Create the preset, then sign in with ChatGPT in the daemon host's browser. |
| A Claude subscription | `claudecode` | Install `claude` on the daemon host and complete the vendor browser sign-in. |
| GitHub Copilot | `github-copilot` | Install a `copilot` CLI supporting protocol 4 on the daemon host and sign in. |
| A Google GenAI API key | `google-genai` | Use the key-based Gemini preset. |
| Models served locally | `ollama` | Start Ollama and make the configured models available at its endpoint. |
| An Antigravity account | `antigravity` | Use browser OAuth; a pasted redirect is supported. |
| A native decision model | `typesafe` | Supply `TYPESAFE_API_KEY`. This is for decisions, not the chat model picker. |

For browser-only presets you configure scope, not a token or a custom
endpoint. Installing a preset doesn't verify live access. Highlight an
OAuth provider and press `l`, or name it explicitly:

```text
/login codex
```

From a shell, the equivalent sign-in and status check are:

```sh
tasksmith auth login codex
tasksmith auth status codex
```

Check the `Status:` and `Account:` rows to confirm which account signed in.
The summary also shows the auth type and, when available, the provider scope,
default model, model count, and error. Omit the provider from `auth status`
to compare accounts and scopes across presets. For scripts, use
`tasksmith auth status codex --json` for one object (including model IDs when
available), or `tasksmith auth status --json` for the full array.

Omit the provider on login only if exactly one OAuth provider is configured.
For Antigravity over SSH, print the URL and submit the final redirect:

```sh
tasksmith auth login antigravity --no-browser --paste
```

Don't use those flags for Claude Code or Copilot, or import credentials from
the vendor CLI's own profile. Use `tasksmith auth logout <provider>` to
remove that registration and cancel any pending sign-in.

## Switch models and thinking effort

1. Open `/model`. Type a provider, model ID, display name or alias to filter.
2. Check context capacity and billing before pressing `Enter`. This changes
   the active session, **not** the durable default for other sessions.
3. Open `/effort` for the levels supported by this model. For an effort
   model that offers `high`, you can also set it directly:

```text
/effort high
```

Return control to the configured default:

```text
/effort default
```

Some models expose a token budget (`/budget`) or a toggle or adaptive
strategy (`/thinking`) instead. Don't infer supported efforts from another
model's name. If the conversation includes images or audio, switching to a
model without that capability can require confirmation.

| Picker or status display | How to read it |
| --- | --- |
| `400.0k ctx` | Context window capacity, not tokens remaining in a subscription. |
| `$3/$15 /M` | Example input/output prices per million tokens. Blank pricing means unknown, not free. |
| `quota` | Subscription billing. The status line shows the limiting quota bucket when reported, not an estimated dollar charge. |

Open `/usage quotas` (the **4 Quotas** tab) to compare remaining capacity
and reset times across windows. A shared bucket is grouped by ID. Less than
20% remaining is red; below 50% is yellow; otherwise the gauge is green.
`∞` means unlimited. A missing countdown means the provider didn't report one.

## Assign model aliases with F2

Use aliases when the *job* is stable but the model may change:

| Alias | Useful assignment |
| --- | --- |
| `@fast` | Quick exploration or a low-latency fallback. |
| `@balanced` | Everyday implementation. |
| `@smart` | Deeper debugging, planning or review. |
| `@flagship` | The most capable model you want available. |
| `@default` | The default completion model for a new main session. |
| `@code-reviewer` | Pin reviews independently of `@smart`. |
| `@permission-reviewer` | Prefer a particular model for auto-mode permission review. |

In `/model`, highlight the desired model and press `F2`. The same editor
is available in `/providers` → provider → model manager.

1. Press `Space` to stage a standard alias, or `n` to add a custom alias.
2. Read **moves from** before saving: the assignment is removed from its
   other owners in writable manifests, not copied to a second model.
3. Press `Enter` to save or `Esc` to cancel. The editor persists changes in
   provider manifests; `@default` sets `spec.defaultModels.completion`.

For a repository-specific choice, merge this fragment into the frontmatter
of `WORKSPACE.md`. Substitute IDs actually configured in `/providers`:

```yaml
spec:
  defaultModels:
    completion: ollama/qwen3.8
  modelAliases:
    fast: ollama/qwen3.8
    code-reviewer: ollama/qwen3.8
    permission-reviewer: typesafe/jev-latest
```

Use bare names as keys; use `@name` when selecting one. Workspace settings
shadow provider-level assignments and the editor doesn't rewrite them.
`@inherit` is reserved for a child's parent model, not an assignable alias.
Installing another preset skips aliases already assigned elsewhere; use
`F2` if you actually intend to move them.

## Add a custom or OpenAI-compatible provider

In `/providers`, press `a`, choose **Custom provider**, and enter the service
name, adapter type, endpoint and credential reference. A custom name doesn't
inherit the preset's models, defaults or auth configuration. In its model
manager, `n` adds the ID served by the endpoint; `d` stages a default and
`s` saves. With catalog support, use `c`, `Enter` to select, then `Ctrl+S`.

For an endpoint implementing Chat Completions only, use the OpenAI adapter
and explicitly select the wire protocol. For example, adapt the following
in `.agents/providers/team-inference.provider.yaml`; supply the endpoint
and model ID your service actually exposes:

```yaml
apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: team-inference
  description: Team inference endpoint
spec:
  type: openai
  endpoint: https://openrouter.ai/api/v1
  auth:
    type: bearer
    token: "${env:OPENROUTER_API_KEY}"
  config:
    apiMode: chat_completions
  models:
    - id: meta-llama/llama-3.3-70b-instruct
      name: meta-llama/llama-3.3-70b-instruct
      capabilities:
        completion: true
        tools: true
  defaultModels:
    completion: meta-llama/llama-3.3-70b-instruct
```

See [provider configuration](../reference/providers.md) for capabilities
and [OpenAI-compatible endpoints](../reference/providers/openai.md) for
protocol choices. Don't advertise tool calling or input modalities the
upstream model can't handle.

## Handle auth expiry and provider warnings

For token-based OAuth, a live `401` can trigger one credential refresh and
retry before stream output begins. This is not an indefinite replay: if
recovery fails, use the interactive re-authentication prompt or `/login`.
For a rejected static API key, update the credential and reload instead.

Open `/warnings` for the active diagnostic. An invalid provider manifest
is isolated: repair the named provider rather than removing healthy ones.
Use `/providers` to distinguish **Invalid config**, **Needs sign-in** and
**Disabled**. A disabled inherited provider may have a workspace shadow;
re-enable it there rather than editing your user-wide configuration.

## Next steps

- [Pin models for delegated work](./agents-and-subagents.md).
- [Arrange quota and usage counters](./appearance.md).
- [Check provider roles, authentication and capabilities](../reference/providers.md).
- [Configure the OpenAI adapter](../reference/providers/openai.md).
