# Codex with Sign in with ChatGPT

The `codex` provider streams Responses API completions through an eligible ChatGPT plan. TaskSmith runs the browser sign-in and tool loop directly through Loom. [OpenAI direct inference guide](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference)

## Set up a workspace

1. Run `/setup`, choose **Codex**, and sign in with ChatGPT in the browser on the daemon host.
2. Finish setup to install the Codex preset. Its default completion model is GPT-6.2 Sol.
3. Use `/model` to select a configured model that your plan includes. To browse or add models available to the signed-in account, open `/providers`, select **codex**, and use **Manage models**.

In an existing workspace, run `/login codex` and add the **codex** preset from `/providers`. From a shell connected to a running daemon, use:

```sh
tasksmith auth login codex
tasksmith auth status codex
tasksmith auth logout codex
```

The daemon opens the browser and completes the loopback callback on the same host. Use the browser there for both first-time sign-in and reauthorization. To sign in to a different account, sign out first.

## Manifest and capabilities

The provider requires `metadata.name: codex`, `spec.type: codex`, and `spec.auth.type: oauth2`. Use `spec.models` to declare model IDs shown in the account catalog and `spec.defaultModels.completion` to choose the default. The service endpoint, headers, and provider options are managed by the driver.

| Preset model | Aliases | Context | Output |
| :--- | :--- | ---: | ---: |
| `gpt-6.2-sol` | `@default`, `@balanced` | 1,050,000 | 128,000 |
| `gpt-6.2-astra` | `@smart`, `@flagship` | 1,050,000 | 128,000 |
| `gpt-6-luna` | `@fast` | 1,050,000 | 128,000 |
| `gpt-6.1-terra` | | 1,050,000 | 128,000 |

Loom resolves the account catalog’s model capabilities from its OpenAI profiles. Requests use ChatGPT plan billing.

The driver sends TaskSmith function tools with each request and uses `store: false`. The direct inference preview omits sampling and max-token controls and excludes hosted file search, Code Interpreter, native tool search, audio, and video. [OpenAI preview limitations](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations)
