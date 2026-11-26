# Daemon HTTP API

These endpoints serve CLI views over the daemon’s Unix socket. Workspace-scoped requests identify the workspace with `X-TaskSmith-CWD`.

## Usage HTTP Endpoints

All usage endpoints are read-only.

| Method | Path | Description |
| :--- | :--- | :--- |
| `GET` | `/v1/sessions/{id}/usage` | `SessionUsage` for one session (including child sessions): totals, breakdowns by model, pricing tier, purpose and tool (calls, errors, denied, canceled, total and slowest duration, lines changed), code changes, prompt cache statistics, and first and last activity time. |
| `GET` | `/v1/usage` | Usage summary (totals, groups, sessions count, top sessions, tools, code changes, previous-period totals, and per-workspace groups for global scope). Query params: `scope=workspace\|global` (default `workspace`, resolved from `X-TaskSmith-CWD`), `since` and `until` (RFC 3339 or `YYYY-MM-DD`; `since` defaults to 30 days ago), `group_by=model\|provider\|agent\|purpose\|tier\|day` (default `model`). |
| `POST` | `/v1/metrics/query` | Runs a generic metrics query (`kind`, `aggregates`, `group_by`, `bucket`, `filter`, `since`, `until`) and returns grouped rows. Aggregate functions are `sum`, `min`, `max` and `count`; `count` counts only the records that measured that metric, so `sum/count` is a mean over measured records. Rows report one value per metric name, so a query listing one metric twice is rejected with `400`; request one function per metric. |

## Model Quota HTTP Endpoint

| Method | Path | Description |
| :--- | :--- | :--- |
| `GET` | `/v1/quotas` | `QuotaSnapshotResponse` (`providers[]` of `ProviderQuotaSnapshot` with `provider`, optional `account`, `collected_at`, `buckets` map keyed by bucket ID, and `models` map of qualified model IDs to ordered bucket IDs). |
| `GET` | `/v1/quotas?model=<id>` | `ModelQuota` (`model`, `provider`, optional `account`, and `buckets[]` of `QuotaBucket` with `id`, `scope`, `window`, `unit`, `limit`, `used`, `remaining_fraction` 0–1, `unlimited`, `overage`, and optional `reset_at` RFC 3339) for quota-billed providers. Returns `204 No Content` when the model's provider does not report quotas; `400` when `model=` is empty. |

Model listings carry the billing facts behind this: each `Model` reports `description` and
`quota_billed` (`true` when the model has no pricing and its provider bills against a
subscription window). Sessions on quota-billed models push a `quota_updated` event on the
session stream carrying the latest `ModelQuota` after every completed turn and whenever a call
is rate limited, so clients keep the window fresh without polling. A rate-limited call's error
is enriched with the fresh facts it can prove (e.g. `94% quota remaining · resets in 3h12m at
18:40`) and never with an invented reset time.

## Provider Auth HTTP Endpoints

These existing endpoints drive provider sign-in behind `tasksmith auth` and `/login`. Providers are addressed by name. Claude Code and GitHub Copilot use vendor-owned browser sign-in on the daemon host without adding API endpoints.

| Method | Path | Description |
| :--- | :--- | :--- |
| `GET` | `/v1/providers` | Lists provider presets as `ProviderSummary` with `name`, `display_name`, `status`, `has_key`, `auth_type`, `auth_env`, `account`, `default_model`, `models`, and `scope`. The optional `browser_auth_label` is the sign-in label for providers that open the browser on the daemon host. |
| `POST` | `/v1/providers/{name}/auth` | Starts an OAuth flow and returns `ProviderAuthResponse`. The optional `auth_url` is omitted for browser-owned flows on the daemon host. |
| `GET` | `/v1/providers/{name}/auth` | Returns the current `ProviderAuthResponse` for one provider; `status` is `idle`, `authorizing`, `authenticated`, `expired_auth`, `error`, or `signed-out`. |
| `POST` | `/v1/providers/{name}/auth/callback` | Completes a flow that accepts pasted redirects. The body field `callbackUrl` accepts the full URL, a bare query string, or an authorization code. Returns the settled `ProviderAuthResponse`. Returns `400` `bad_request` when the paste is malformed, the flow is absent, or the provider requires a browser callback. |
| `DELETE` | `/v1/providers/{name}/auth` | Removes stored OAuth credentials for one provider and cancels any in-flight flow. Returns `ProviderAuthResponse` with `status` `signed-out` when credentials were removed or `idle` when the provider was not signed in. |

## Tools HTTP Endpoints

Tool endpoints act on the workspace resolved from the `X-TaskSmith-CWD` header.

| Method | Path | Description |
| :--- | :--- | :--- |
| `GET` | `/v1/workspace/tools` | Lists workspace tools (disabled ones included with `enabled: false` and `disabled_by` — `manifest` or `workspace_policy`) as `api.Tool`, optionally scoped to an agent via the `agent` query for per-agent `status`. |
| `PATCH` | `/v1/workspace/tools/{name}/enabled` | Enables or disables one tool for the workspace. Body: `enabled` (bool). Disabling patches `spec.enabled: false` into a workspace-writable manifest or adds the name to `WORKSPACE.md` `spec.policies.tools.exclude`; enabling removes whichever opt-out exists. Reloads the workspace and returns the updated `Tool` list. `404` `not_found` for an unknown tool or an MCP-discovered `<server>__<tool>` name (toggle the parent server instead); `409` `manifest_read_only` when only a non-writable manifest declares the opt-out; `409` `policy_pattern_conflict` when a WORKSPACE.md pattern would keep a re-enabled tool excluded. |

## MCP HTTP Endpoints

MCP endpoints act on the workspace resolved from the `X-TaskSmith-CWD` header. Servers are addressed by manifest name, or by qualified name (`<slug>/MCP/<name>`) when a global and a project server share a name. Every endpoint returns `404` `not_found` for an unknown server, and `503` `mcp_unavailable` for operations needing a live connection while the workspace has no MCP runtime attached.

| Method | Path | Description |
| :--- | :--- | :--- |
| `GET` | `/v1/workspace/mcps` | Lists configured MCP servers as `MCPServer`: manifest `name`, `displayName`, `description`, `namespace`, `scope`, portable manifest `path`, `transport` (as declared: `stdio`, `sse`, `http`, or `streamable-http`; defaults to `stdio`), resolved `command` or `endpoint`, `authType`, live `status` (`disconnected`, `connecting`, `connected`, `auth_required`, or `error`), `authUrl` and `error` when set, plus discovered `tools`, `prompts`, and `resources`. Items excluded by manifest policies are still listed with `allowed: false`. Servers with no live connection report `status: disconnected`. |
| `POST` | `/v1/workspace/mcps/{name}/restart` | Reconnects one server and returns its refreshed `MCPServer`. |
| `POST` | `/v1/workspace/mcps/{name}/auth` | Starts or resumes authorization for one server and returns `MCPAuthResponse` (`authUrl`, `status`). |
| `POST` | `/v1/workspace/mcps/{name}/auth/callback` | Completes the authorization in progress with a redirect URL the user copied from their browser. Body: `callbackUrl` — the full address-bar URL, a bare query string, or an authorization code alone. Answers `204` with no body, since the flow continues on its own; `400` `bad_request` when the paste is empty or no authorization is open for the server. |
| `POST` | `/v1/workspace/mcps/{name}/prompts/{prompt}` | Renders one discovered prompt with optional `args` (string key to string value) and returns `MCPPromptResponse` (`description`, `messages` with `role` and `text`). |
| `POST` | `/v1/workspace/mcps/{name}/resources` | Reads one resource by `uri` and returns `MCPResourceResponse` (`uri`, `mimeType`, `text`). |
| `POST` | `/v1/workspace/mcps/{name}/policies/allow` | Permits one item in the server manifest. Body: `kind` (`tool`, `prompt`, or `resource`) and `value` (raw tool name, prompt name, or resource URI). Returns the updated `MCPServer`. |
| `PATCH` | `/v1/workspace/mcps/{name}/enabled` | Enables or disables one server for the workspace. Body: `enabled` (bool). Disabling patches `spec.enabled: false` into a workspace-writable manifest or adds the name to `WORKSPACE.md` `spec.policies.mcps.exclude`; enabling removes whichever opt-out exists. Reloads the workspace and returns the updated `MCPServer` list. `404` `not_found` for an unknown server; `409` `manifest_read_only` for an opt-out in a non-writable manifest; `409` `policy_pattern_conflict` when a WORKSPACE.md pattern would keep a re-enabled server excluded. |

Prompt rendering and resource reads reject anything the server manifest's `policies` prohibit with `403` `mcp_policy_prohibited`; the rejection message names the prohibited item, the policy section it is excluded from (`policies.tools`, `policies.prompts`, or `policies.resources`), and the manifest path. Pass `allowProhibited: true` in the prompt or resource body to permit a single prohibited use without editing the manifest. See [Tools & MCP Servers](../guides/tools-and-mcp.md) for manifest authoring and [Manifests](./manifests.md) for the `MCP` schema.

## Workspace Response

`GET /v1/workspace` returns the resolved `Workspace` for the `X-TaskSmith-CWD` header. Each warning field is omitted when empty:

| JSON field | Type | Description |
| :--- | :--- | :--- |
| `warnings` | array of strings | Full workspace warning list, including provider, model alias, and sandbox warnings. MCP health is tracked separately via `/v1/workspace/mcps`. |
| `sandbox_warnings` | array of strings | Sandbox-specific subset (`Workspace.SandboxWarnings`) used in the REPL header. Every entry is also included in `warnings`. |

## Workspace Onboarding & Trust HTTP Endpoints

All onboarding and trust endpoints operate on the workspace directory resolved from the `X-TaskSmith-CWD` header.

| Method | Path | Description |
| :--- | :--- | :--- |
| `GET` | `/v1/workspace/status` | Returns `WorkspaceStatus`: `path`, trust state (`is_trusted`, `fingerprint_changed`), executable scan (`executables`), configuration flags (`is_configured`, `has_agents_md`), `global_providers`, `global_mcps`, `missing_credentials`, and the first-run flag `needs_onboarding`. |
| `GET` | `/v1/workspace/trust` | Returns `TrustStatus` for the current workspace, including detected MCP configs, custom tools, and local `.env` existence. |
| `POST` | `/v1/workspace/trust` | Records trust for a workspace directory. Body: `path` (absolute path) and `scope` (`folder` or `parent`). |
| `DELETE` | `/v1/workspace/trust` | Revokes trust for the current workspace directory, returning it to restricted mode. |
| `POST` | `/v1/providers/verify` | Validates API credentials or endpoint reachability against upstream model provider. Body: `provider`, optional `api_key`, `endpoint`, `auth_type`, and `auth_env`. Returns `valid` and, when set, `models_count`, `error_message`, and `warning`. |
| `POST` | `/v1/workspace/init` | Initializes workspace configuration and applies user choices. Body: `InitWorkspaceRequest` (`cwd`, `scope`, `project_name`, `provider`, `api_key`, `endpoint`, `auth_type`, `auth_env`, `default_models`, `default_mode`, `disabled_tools`, `disabled_mcps`). Tool and MCP exclusions are ignored for `user` scope. Returns `status: "ok"` and optional `skipped_preset_aliases`. |
| `POST` | `/v1/workspace/providers` | Creates a provider from `CreateProviderRequest`; the returned `WorkspaceProvider` can include `skipped_preset_aliases`. |

Each `skipped_preset_aliases` entry has `alias`, `model_id` (full ID of the new model), optional `owner`, and `workspace_override` when the alias is set in `WORKSPACE.md`.

## See Also

- [CLI reference index](./cli.md)
- [Models and providers guide](../guides/models-and-providers.md)
- [Tools and mcp guide](../guides/tools-and-mcp.md)
- [Trust guide](../guides/trust.md)
- [Acp guide](../guides/acp.md)
