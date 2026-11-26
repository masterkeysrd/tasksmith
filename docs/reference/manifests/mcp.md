# MCP Server Manifest (`*.mcp.yaml`)

Configures an external Model Context Protocol (MCP) server integration. Placed under `.agents/mcps/<name>.mcp.yaml`.

## Example

This stdio server exposes selected read queries and schema resources. Marking `write_query` as dangerous in `overrides` does not undo its exclusion from `policies.tools`.

```yaml
apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: sqlite-explorer
  displayName: SQLite Database Explorer
  description: Model Context Protocol server exposing database inspection and queries
spec:
  type: stdio
  command:
    - npx
    - -y
    - "@modelcontextprotocol/server-sqlite"
    - "--db-path"
    - "./data/app.db"
  env:
    DEBUG: "mcp:*"
  annotations:
    isReadOnly: true
    isDangerous: false
    isOpenWorld: false
  policies:
    tools:
      include:
        - "read_query"
        - "list_tables"
        - "describe_table"
      exclude:
        - "write_query"
    prompts:
      include:
        - "*"
    resources:
      include:
        - "schema://*"
  overrides:
    write_query:
      isReadOnly: false
      isDangerous: true
```

## Spec Reference

| Field | Type | Description |
| :--- | :--- | :--- |
| `enabled` | boolean | Set `false` to keep the server from ever connecting or exposing tools; it lists `[Disabled]` in `/mcp` and can be re-enabled from there (which removes the flag). Omitted means enabled. |
| `type` | string | Transport protocol: `stdio`, `sse`, `http`, or `streamable-http`. |
| `command` | array of strings | Command line and arguments for `stdio` process execution. |
| `endpoint` | string | HTTP/SSE server URL for network-based MCP servers. |
| `env` | map[string]string | Environment variables passed to the child process. |
| `auth` | object | Authentication config (`type`, `header`, `token`, `username`, `password`, `oauth`) for HTTP/SSE endpoints. |
| `headers` | map[string]string | Custom HTTP headers sent with requests. |
| `annotations` | object | Default safety annotations applied to all tools exported by this MCP. |
| `policies.tools` | object | Inclusion/exclusion filter globs for exported tools (`include`, `exclude`). |
| `policies.prompts` | object | Inclusion/exclusion filter globs for exported prompts. |
| `policies.resources` | object | Inclusion/exclusion filter globs for exported resources. |
| `overrides` | map of objects | Per-tool safety annotation overrides keyed by tool name. |
| `sandbox` | string | WARP sandbox reference, or `none`. This schema field does not override TaskSmith daemon isolation. |

A `policies` filter is enforced as a hard block against the agent: a prohibited tool is never registered and reports status `denied`, while a prohibited prompt or resource read is rejected with `403` `mcp_policy_prohibited` naming the item, the policy section, and the manifest path. Tightening a filter takes effect immediately, including for sessions that already discovered the tool. Only a user may authorize a single prohibited prompt or resource read with `allowProhibited` — through the [`/mcp`](../cli/slash-commands.md#mcp-item-confirmation) confirmation or the [MCP HTTP endpoints](../daemon-api.md#mcp-http-endpoints) — and only a user editing the manifest can permit a prohibited tool. See [MCP Tool Execution Model](../tools.md#6-mcp-tool-execution-model).

## Authentication and annotations

| `auth` field | Type | Use |
| --- | --- | --- |
| `type` | string | Auth scheme, such as `bearer`, `api-key`, `basic`, `oauth2`, `client-credentials`, `enterprise`, or `none`. |
| `header`, `token` | strings | API-key header name and secret token expression; bearer auth uses `token`. |
| `username`, `password` | strings | Basic auth username and password expression. |
| `oauth.clientId`, `oauth.clientSecret` | strings | Target authorization server client ID and secret expression. |
| `oauth.idpIssuer`, `oauth.idpClientId`, `oauth.idpClientSecret` | strings | Enterprise identity provider issuer, client ID and secret expression. |
| `oauth.audienceIssuer`, `oauth.resourceURI` | strings | Token-exchange audience issuer and requested resource identifier. |
| `oauth.scopes` | string array | Requested scopes; empty uses discovered defaults. |

For secret expressions, use `${env:VARIABLE}` or `${file:path}` rather than plaintext. `annotations` and each `overrides` entry share the [tool annotation fields](./tool.md#spec-reference): `isReadOnly`, `isIdempotent`, `isDangerous`, `isOpenWorld`, and `userHint`. A tool-specific override takes precedence over server defaults, which take precedence over the remote tool's annotations.

## See Also

- [Tools and MCP](../../guides/tools-and-mcp.md)
- [Manifest hierarchy and metadata](../manifests.md)
