# Filesystem Tools

See [permissions](../../guides/permissions.md) for path-scoped approvals. Read an existing file before overwriting, editing, or removing it; `force` bypasses the file tracker, not permissions. [Tool catalog and permission modes](../tools.md).

## `view`

Reads text or supported multimodal content from a file inside the workspace. Supports line-range pagination (`offset`, `limit`) for text files (including `.svg`) and optional `pages` ranges for PDFs. Supported images (`image/png`, `image/jpeg`, `image/webp`, `image/gif`), PDFs (`application/pdf`), and audio (`audio/wav`, `audio/mpeg`, `audio/mp4`, `audio/ogg`, `audio/flac`) are snapshotted into `<session-dir>/media/<sha256>` and recorded in the file tracker for Read-Before-Overwrite CAS. If the active model lacks support for the file's modality, `view` returns a non-fatal notice (`omitted: true`) without injecting an unsupported media block.

**Safety annotations:** `isReadOnly: true`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`.

**User hint:** `Read file: %s`. **Default permission:** Safe read; allowed.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `path` | string | Yes | File path relative to workspace root or absolute path inside the workspace. |
| `offset` | integer | No | 1-based line number from which to start reading (default: 1). Ignored for images, PDFs, and audio. |
| `limit` | integer | No | Maximum number of lines to return. Ignored for images, PDFs, and audio. Default 500; maximum 2,000 lines (32 KiB total; 2,048 bytes per line). |
| `pages` | string | No | Optional page range for PDF files (e.g., `"1-10"` or `"3"`). Ignored for text, image, and audio files. |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `path` | string | Yes | Normalized workspace-relative file path. |
| `content` | string | Yes | Line-numbered text content of the selected line range, or compact metadata for media files. |
| `start_line` | integer | Yes | 1-based first line number returned. |
| `end_line` | integer | Yes | 1-based last line number returned. |
| `total_lines` | integer | Yes | Total line count of the file on disk. |
| `truncated` | boolean | No | `true` if additional lines remain after `end_line`. |
| `hash` | string | Yes | SHA-256 hex digest of the full file on disk. |
| `mime_type` | string | No | MIME type when the viewed file is supported multimodal media. |
| `byte_size` | integer (int64) | No | Byte size of the viewed media payload. |
| `pages` | string | No | Normalized page range for viewed PDF files. |
| `modality` | string | No | Media modality (`"image"`, `"pdf"`, or `"audio"`) when viewing multimodal files. |
| `omitted` | boolean | No | `true` if media content was omitted because the active model lacks support for its modality. |

```json
{
  "path": "src/index.ts",
  "offset": 1,
  "limit": 50
}
```

## `ls`

Lists files and directories under a specified directory path with file metadata.

**Safety annotations:** `isReadOnly: true`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`.

**User hint:** `List directory: %s`. **Default permission:** Safe read; allowed.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `path` | string | No | Directory path relative to workspace root (defaults to `"."`). |
| `recursive` | boolean | No | Traverse subdirectories recursively when `true`. Default: `false`. |
| `max_depth` | integer | No | Maximum recursion depth when `recursive` is `true`. No depth cap when omitted. |
| `include_hidden` | boolean | No | Include dot-prefixed hidden entries when `true`. Default: `false`. |
| `ignore` | array of strings | No | Glob patterns to exclude from listing (e.g. `["node_modules/**"]`). |
| `limit` | integer | No | Maximum number of directory entries to return. Default 200; maximum 500 (32 KiB total). |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `path` | string | Yes | Normalized directory path listed. |
| `entries` | array of objects | Yes | Directory entries found. |
| `entries[].path` | string | Yes | Entry path relative to the workspace root. |
| `entries[].name` | string | Yes | Base name of the entry. |
| `entries[].is_dir` | boolean | Yes | True when the entry is a directory. |
| `entries[].size` | integer (int64) | Yes | Size in bytes. |
| `truncated` | boolean | No | `true` if entries exceeded `limit`. |

```json
{
  "path": "internal/daemon",
  "recursive": false
}
```

## `glob`

Matches workspace file paths using standard wildcard glob expressions.

**Safety annotations:** `isReadOnly: true`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`.

**User hint:** `Glob pattern: %s`. **Default permission:** Safe read; allowed.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `pattern` | string | Yes | Wildcard glob pattern (e.g. `**/*.go`, `src/**/*.tsx`). |
| `path` | string | No | Base directory from which to search (defaults to `"."`). |
| `include_hidden` | boolean | No | Include dot-prefixed hidden files and directories. Default: `false`. |
| `ignore` | array of strings | No | Additional glob patterns to skip. |
| `limit` | integer | No | Maximum number of matched paths to return. Default 200; maximum 500 (32 KiB total). |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `pattern` | string | Yes | Glob pattern evaluated. |
| `path` | string | Yes | Base directory searched. |
| `matches` | array of strings | Yes | Sorted list of matching workspace file paths. |
| `truncated` | boolean | No | `true` if matches were truncated at `limit`. |

```json
{
  "pattern": "**/*.tool.md",
  "path": "."
}
```

## `grep`

Searches file contents across the workspace using regular expressions.

**Safety annotations:** `isReadOnly: true`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`.

**User hint:** `Search pattern: %s`. **Default permission:** Safe read; allowed.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `pattern` | string | Yes | Regular expression pattern to search for. |
| `path` | string | No | File or directory path under which to search. Defaults to `.`. |
| `include` | string | No | Optional glob pattern filtering target file names (e.g. `*.go`). |
| `case_insensitive` | boolean | No | Enable case-insensitive regular expression matching. Default: `false`. |
| `context_lines` | integer | No | Number of surrounding lines to capture before and after each match. Default 0. |
| `include_hidden` | boolean | No | Search dot-prefixed hidden files and directories. Default: `false`. |
| `limit` | integer | No | Maximum number of matches to return. Default 100; maximum 500 (32 KiB total; 500 bytes per line). |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `pattern` | string | Yes | Regular expression pattern evaluated. |
| `matches` | array of objects | Yes | Regular expression matches across searched files. |
| `matches[].path` | string | Yes | File path containing the match. |
| `matches[].line` | integer | Yes | 1-based line number of the match. |
| `matches[].column` | integer | Yes | 1-based column offset of the match. |
| `matches[].text` | string | Yes | Full text of the matching line. |
| `matches[].before` | array of strings | No | Context lines preceding the match. |
| `matches[].after` | array of strings | No | Context lines following the match. |
| `truncated` | boolean | No | `true` if matches were truncated at `limit`. |

```json
{
  "pattern": "func NewServer",
  "include": "*.go",
  "context_lines": 2
}
```

## `write`

Creates a new file or completely replaces the contents of an existing file. Enforces compare-and-swap checking against file modifications.

**Safety annotations:** `isReadOnly: false`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`.

**User hint:** `Write file: %s`. **Default permission:** Workspace write; prompts.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `path` | string | Yes | Target file path within the workspace. |
| `content` | string | Yes | Full text content to write to the file. |
| `force` | boolean | No | Bypass read-before-overwrite and compare-and-swap safety checks. Default: `false`. |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `path` | string | Yes | Normalized target file path. |
| `created` | boolean | Yes | `true` if a new file was created rather than overwritten. |
| `bytes_written` | integer | Yes | Total bytes written to disk. |
| `additions` | integer | Yes | Number of added lines. |
| `deletions` | integer | Yes | Number of deleted lines. |
| `diff` | string | No | Unified diff of the mutation. |
| `before_hash` | string | No | Previous SHA-256 hex digest when overwriting. |
| `after_hash` | string | Yes | New SHA-256 hex digest of the written file. |

```json
{
  "path": "config/settings.yaml",
  "content": "port: 8080\nenv: production\n"
}
```

## `edit`

Replaces text in an existing file and generates a unified diff. Supply exact text from `view`. If no exact match exists, `edit` can recover some minor differences in whole-line matches while preserving the file’s line endings. It rejects ambiguous matches unless `replace_all` is set.

**Safety annotations:** `isReadOnly: false`, `isIdempotent: false`, `isDangerous: false`, `isOpenWorld: false`.

**User hint:** `Edit file: %s`. **Default permission:** Workspace write; prompts.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `path` | string | Yes | Target file path within the workspace. |
| `old_text` | string | Yes | Exact literal substring to search for and replace. |
| `new_text` | string | Yes | Replacement text string. |
| `replace_all` | boolean | No | Replace all non-overlapping matches from the first successful matching pass (defaults to one unambiguous match). Default: `false`. |
| `force` | boolean | No | Bypass read-before-overwrite and compare-and-swap safety checks. Default: `false`. |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `path` | string | Yes | Normalized file path edited. |
| `replacements` | integer | Yes | Number of replacements applied. |
| `additions` | integer | Yes | Number of added lines. |
| `deletions` | integer | Yes | Number of deleted lines. |
| `diff` | string | No | Unified diff of the edit. |
| `before_hash` | string | Yes | SHA-256 hex digest before editing. |
| `after_hash` | string | Yes | SHA-256 hex digest after editing. |

```json
{
  "path": "server.go",
  "old_text": "port := 8080",
  "new_text": "port := 9090"
}
```

## `remove`

Safely removes a file from the workspace, recording deletion diffs and hash tracking.

**Safety annotations:** `isReadOnly: false`, `isIdempotent: false`, `isDangerous: true`, `isOpenWorld: false`.

**User hint:** `Remove file: %s`. **Default permission:** Delete; prompts.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `path` | string | Yes | Target file path within the workspace to remove. |
| `force` | boolean | No | Bypass read-before-overwrite and compare-and-swap checks. Default: `false`. |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `path` | string | Yes | Normalized file path removed. |
| `deletions` | integer | Yes | Number of deleted lines. |
| `diff` | string | No | Unified deletion diff. |
| `before_hash` | string | Yes | SHA-256 hex digest of the file prior to removal. |

```json
{
  "path": "tmp/obsolete.txt"
}
```
