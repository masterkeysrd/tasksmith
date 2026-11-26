# Workspace Resource Tools

See [skills and rules](../../guides/skills-and-rules.md) for workspace instructions and [tools and MCP](../../guides/tools-and-mcp.md) for discovery. [Tool catalog and permission modes](../tools.md).

## `load_skill`

Loads and renders instructions, coding conventions, or runbooks for a specialized workspace skill into the agent's context on demand.

**Safety annotations:** `isReadOnly: true`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`.

**User hint:** `Load skill knowledge`. **Default permission:** Safe read; allowed.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `skill` | string | Yes | Identifier of the skill to load (e.g. `'postgres-migrations'`, `'docker-deploy'`). |
| `arguments` | map of strings | No | Optional key-value parameters passed to render the skill template. |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `skill` | string | Yes | Name of the skill that was loaded. |
| `path` | string | No | Base directory where the skill is located on disk, for resolving relative assets (scripts, references). |
| `instructions` | string | Yes | Rendered instructions and guidelines populated from the skill package. |
| `truncated` | boolean | No | `true` if the skill instructions exceeded size limits (500 lines / 32 KB) and were truncated. |
| `tools` | array of strings | No | Names of deferred skill tools activated into context when the skill is loaded. |

```json
{
  "skill": "docker-deploy",
  "arguments": {
    "image": "my-service",
    "tag": "v1.2.0"
  }
}
```

## `load_rule`

Loads and renders the full instructions of a conditional workspace rule into the agent's context on demand. Conditional rules appear in the system prompt only as a name-and-trigger summary; this tool fetches the body. Always-on rules (`triggers.always: true`) are already in the prompt and never need to be loaded.

The agent normally calls this after a keyword suggestion, but it may call it directly for any rule listed in `<available_rules>`. The harness records the rule as loaded so it is not injected twice by a later glob or tool trigger.

### Availability

Registered when the active agent resolves at least one conditional rule and the workspace or agent tool policy does not deny it, so rule-free workspaces do not pay the schema cost. It is never deferred and never discoverable through `tool_search`: it is either eager or absent. It is also listed in the built-in `main` agent's eager tools.

**Safety annotations:** `isReadOnly: true`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`.

**User hint:** `Load rule`. **Default permission:** Safe read; allowed.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `rule` | string | Yes | Rule name. Accepts the short name (`go-error-handling`), `namespace/name`, or the fully qualified `namespace/Rule/name` form; matching is case-insensitive. |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `rule` | string | Yes | Name of the rule that was loaded. |
| `instructions` | string | Yes | Rendered rule body. |
| `path` | string | No | Rule manifest path, relative to the workspace root. |
| `priority` | integer | No | Rule `spec.priority` used for injection ordering. |
| `truncated` | boolean | No | `true` if the instructions exceeded size limits (500 lines / 32 KB) and were truncated. |

Loading a rule the agent cannot access returns an error rather than an empty result.

```json
{
  "rule": "go-error-handling"
}
```

## `read_mcp_resource`

Reads one resource URI from a configured [MCP server](../../guides/tools-and-mcp.md) and returns its text content. Resources are content a server chooses to publish (documentation, templates, indexes), which is distinct from workspace files — use [`view`](filesystem.md#view) and [`grep`](filesystem.md#grep) for the repository itself.

### Availability

Registered only while the workspace has an MCP runtime attached **and** at least one connected server exposes a resource its own `spec.policies.resources` permits. It is activated automatically once that is true, even when an agent omits it from `spec.tools`, because no agent author can know at authoring time whether the servers it talks to publish resources. Like [`load_rule`](#load_rule), it is never deferred and never discoverable through `tool_search`: it is either eager or absent. A tool policy that excludes it is respected, so an agent can still deny it.

**Safety annotations:** `isReadOnly: true`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`.

**User hint:** `Read MCP resource: %s`. **Default permission:** Safe read; allowed.

The approval plan target is `<server>/<uri>`, so granting approval can be scoped to a single resource or, with a trailing glob, to one server's resources.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `server` | string | Yes | Name of the configured MCP server to read from. |
| `uri` | string | Yes | Resource URI exactly as the server advertises it. |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `server` | string | Yes | Name of the MCP server the resource was read from. |
| `uri` | string | Yes | Resource URI that was read. |
| `mime_type` | string | No | MIME type the server declared for the resource, when known. |
| `text` | string | Yes | Text content of the resource, or a head-plus-tail preview when it exceeded output limits. |
| `truncated` | boolean | No | `true` if the resource text exceeded output limits and was spilled to disk. |

```json
{
  "server": "notes",
  "uri": "notes://specs/spec-1"
}
```

### Prohibited resources

A resource the server manifest blocks under `spec.policies.resources` is rejected before the request leaves TaskSmith, with an error naming the section and the manifest path relative to the workspace root. Only a user acting interactively may authorize a single prohibited read — through the `/mcp` confirmation or via `allowProhibited` on the [MCP HTTP endpoints](../daemon-api.md#mcp-http-endpoints); this tool never sets that flag, so an agent cannot escalate its own access. See [Scoping MCP resources](../../guides/tools-and-mcp.md#use-mcp-prompts-and-resources).

## `tool_search`

Searches the workspace's deferred (not eagerly loaded) tools by name, description, or parameters using BM25 keyword matching and loads the best matches into the active toolset.

**Activation**: `tool_search` is a built-in system tool that is automatically activated (`DeferLoading: false`) whenever the session resolves at least one deferred tool — that is, a policy-allowed built-in, custom (`*.tool.md`), or MCP tool that is not listed in the agent's `spec.tools`. When the session has zero deferred tools, `tool_search` is not registered.

**Discovery**: When at least one tool is deferred, the agent's system prompt renders a `### tool_search tool` section containing a compact `<deferred_tools>` directory. The directory lists each deferred custom/built-in tool by name and description, and groups deferred MCP tools under their server (e.g. `github` with prefix `github__*`) showing up to 15 tool names per server plus a `+N more` overflow count. This advises the agent to call `tool_search` with matching keywords before concluding a capability is unavailable.

**Safety annotations:** `isReadOnly: true`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`.

**User hint:** `Search available tools`. **Default permission:** Safe read; allowed.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `query` | string | Yes | Search query terms to find relevant tools by name, description, or parameters. |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `tools` | array of strings | Yes | Names of matching tools loaded into context. |

```json
{
  "query": "http download"
}
```
