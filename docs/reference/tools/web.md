# Web Tools

See [tools and MCP](../../guides/tools-and-mcp.md) and [permissions](../../guides/permissions.md) for network access and approvals. [Tool catalog and permission modes](../tools.md).

## `web_fetch`

Fetches web documents over HTTP or HTTPS and returns formatted text content (converting HTML to Markdown or plain text, and pretty-printing JSON and XML by default).

**Safety annotations:** `isReadOnly: true`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: true`.

**User hint:** `Fetch URL: %s`. **Default permission:** Network read; prompts.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `url` | string | Yes | Target HTTP or HTTPS URL to fetch. |
| `headers` | map of strings | No | Optional HTTP request headers (e.g. `{"Accept": "application/json"}`). |
| `format` | string | No | Output format: `markdown` (default — converts HTML to Markdown and pretty-prints JSON/XML), `text` (extracts plain text from HTML and pretty-prints JSON/XML), or `html` (returns the raw unmodified response body). |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `url` | string | Yes | Normalized request URL. |
| `status_code` | integer | Yes | HTTP response status code (e.g. 200). |
| `content_type` | string | No | Response `Content-Type` header. |
| `text` | string | No | Formatted or decoded text response body (subject to `format`). |

```json
{
  "url": "https://raw.githubusercontent.com/masterkeysrd/tasksmith/main/README.md"
}
```

## `web_search`

Runs a web search query and returns ranked result titles, URLs, and snippets. The query executes from the daemon host on its own HTTP client rather than the session's sandbox network client, so child-process network confinement (`NetworkPolicy`, allowlist proxy) is unaffected; the permission pipeline still gates every search. Follow up on a promising result with `web_fetch` to retrieve the full page.

**Safety annotations:** `isReadOnly: true`, `isIdempotent: false`, `isDangerous: false`, `isOpenWorld: true`.

**User hint:** `Search the web: %s`. **Default permission:** Network read; prompts.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `query` | string | Yes | Search query. |
| `allowed_domains` | array of strings | No | Optional host filter; only results whose host matches one of these domains are returned. A bare domain matches its subdomains; `*` wildcards are supported. |
| `blocked_domains` | array of strings | No | Optional host filter; results whose host matches one of these domains are dropped. Takes precedence over `allowed_domains`. |
| `limit` | integer | No | Maximum number of results to return. Defaults to 10, capped at 20. |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `query` | string | Yes | Normalized search query that was executed. |
| `results` | array of objects | Yes | Search results in ranking order. |
| `results[].title` | string | Yes | Title of the search result. |
| `results[].url` | string | Yes | Destination URL of the search result. |
| `results[].snippet` | string | No | Short description or snippet of the search result. |
| `truncated` | boolean | No | `true` when the results page carried more results than the limit. |

```json
{
  "query": "go language effective go",
  "allowed_domains": ["go.dev"]
}
```

## `download`

Downloads a remote file over HTTP or HTTPS directly to a designated workspace path.

**Safety annotations:** `isReadOnly: false`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: true`.

**User hint:** `Download file: %s`. **Default permission:** Network-backed write; prompts.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `url` | string | Yes | Source HTTP or HTTPS URL to download. |
| `path` | string | Yes | Destination file path within the workspace. |
| `headers` | map of strings | No | Optional HTTP request headers. |
| `force` | boolean | No | Bypass read-before-overwrite and compare-and-swap checks. Default: `false`. |
| `timeout_ms` | integer | No | Download timeout in milliseconds. Omitted or nonpositive: no additional per-call deadline. |
| `max_bytes` | integer (int64) | No | Maximum allowed download size in bytes. Default 100 MiB. |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `url` | string | Yes | Source URL downloaded. |
| `path` | string | Yes | Normalized destination file path in the workspace. |
| `status_code` | integer | Yes | HTTP status code returned by upstream server. |
| `content_type` | string | No | Upstream `Content-Type` header. |
| `bytes_written` | integer | Yes | Number of bytes written to disk. |
| `created` | boolean | Yes | `true` if a new file was created. |
| `before_hash` | string | No | Previous SHA-256 hex digest when overwriting. |
| `after_hash` | string | Yes | New SHA-256 hex digest of the downloaded file. |
| `is_binary` | boolean | No | `true` if content was detected as binary data. |

```json
{
  "url": "https://example.com/data.json",
  "path": "assets/data.json"
}
```
