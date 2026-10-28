# Built-in Tools Reference

TaskSmith provides 13 core built-in system tools backed by virtualized OS sandboxes, compare-and-swap filesystem mutators, background process managers, and interactive user prompts.

---

## 1. Core Tools Catalog Summary

| Tool Name | Capability | Default Permission Behavior | Description |
| :--- | :--- | :--- | :--- |
| **`view`** | `FS` | Safe & Read-Only / Auto-allowed | Reads whole files or specific line ranges with line numbering and SHA-256 digests. |
| **`ls`** | `FS` | Safe & Read-Only / Auto-allowed | Lists directory entries with file sizes, directories, and optional recursion. |
| **`glob`** | `FS` | Safe & Read-Only / Auto-allowed | Matches workspace paths against wildcard glob patterns. |
| **`grep`** | `FS` | Safe & Read-Only / Auto-allowed | Searches file contents using regular expressions with match line numbers and context. |
| **`write`** | `FS` | Mutating / Prompts in `default`, auto in `accept_edits` | Creates or overwrites files with atomic comparison and diff tracking. |
| **`edit`** | `FS` | Mutating / Prompts in `default`, auto in `accept_edits` | Replaces exact string matches within a file with unified diff generation. |
| **`remove`** | `FS` | Mutating / Prompts in `default`, auto in `accept_edits` | Deletes a file within the workspace with safety checks. |
| **`bash`** | `Proc` | Dangerous & Open-World / Prompts in `default` & `accept_edits` | Executes commands inside the sandbox with synchronous timeout or background task delegation. |
| **`web_fetch`** | `Net` | Prompts in `default` unless allow rule exists | Fetches web documents via HTTP/HTTPS with body extraction. |
| **`download`** | `Net`, `FS` | Prompts in `default` unless allow rule exists | Downloads a remote HTTP/HTTPS resource directly into a workspace destination. |
| **`tasks`** | `Proc`, `Session` | Safe / Auto-allowed | Lists, inspects, feeds input to, or terminates running background tasks. |
| **`todos`** | `Session` | Safe / Auto-allowed | Manages the session progress checklist, tracking pending and completed items. |
| **`ask_question`** | `Session` | Safe / Auto-allowed | Prompts the user with structured interactive questions (`single`, `multi`, `text`, `confirm`). |
| **`load_skill`** | `Session`, `Workspace` | Safe / Auto-allowed | Loads domain guidelines, coding conventions, or workflows for a specified skill on demand. |

---

## 2. Permission Modes & Safety Annotations

TaskSmith evaluates every tool invocation against the active session **Permission Mode** and the tool's declarative safety annotations:

### Safety Annotations

Every built-in tool defines safety properties in its manifest:

- **`isReadOnly`**: When `true`, tool execution does not modify filesystem or external system state.
- **`isIdempotent`**: When `true`, repeated executions with identical arguments yield identical outcomes.
- **`isDangerous`**: When `true`, execution could alter environment, process state, or system integrity.
- **`isOpenWorld`**: When `true`, tool interacts with unconstrained inputs, shell interpreters, or network hosts.
- **`userHint`**: Template string displayed in confirmation prompts (e.g., `"Run command: %s"`).

### Behavior Across Permission Modes

| Permission Mode | Safe Read-Only (`view`, `ls`, `glob`, `grep`, `todos`, `tasks`, `ask_question`, `load_skill`) | Filesystem Writes (`write`, `edit`, `remove`) | Execution & Network (`bash`, `web_fetch`, `download`) |
| :--- | :--- | :--- | :--- |
| **`default`** | Auto-allowed | Prompts user for approval | Prompts user for approval |
| **`accept_edits`** | Auto-allowed | Auto-allowed (unified diff recorded) | Prompts user for approval |
| **`plan`** | Auto-allowed | Denied (`AccessDeny`) | Denied (`AccessDeny`) |
| **`ask`** | Prompts user for approval | Prompts user for approval | Prompts user for approval |
| **`auto`** | Auto-allowed | Auto-allowed | Auto-allowed |

---

## 3. Tool Specifications & Schemas

### `view`

Reads text content from a file inside the workspace. Supports pagination via line limits and offsets.

- **Annotations**: `isReadOnly: true`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`
- **User Hint**: `"Read file: %s"`

#### Input Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `path` | string | **Yes** | File path relative to workspace root or absolute path inside the workspace. |
| `limit` | integer | No | Maximum number of lines to return. |
| `offset` | integer | No | 1-based line number from which to start reading (default: 1). |

#### Output Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `path` | string | Normalized workspace-relative file path. |
| `content` | string | Raw text content of the selected line range. |
| `numbered` | string | Line-numbered text content formatted for LLM inspection. |
| `start_line` | integer | 1-based first line number returned. |
| `end_line` | integer | 1-based last line number returned. |
| `total_lines` | integer | Total line count of the file on disk. |
| `hash` | string | SHA-256 hex digest of the full file on disk. |
| `truncated` | boolean | `true` if additional lines remain after `end_line`. |

```json
{
  "path": "src/index.ts",
  "offset": 1,
  "limit": 50
}
```

---

### `ls`

Lists files and directories under a specified directory path with file metadata.

- **Annotations**: `isReadOnly: true`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`
- **User Hint**: `"List directory: %s"`

#### Input Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `path` | string | No | Directory path relative to workspace root (defaults to `"."`). |
| `recursive` | boolean | No | Traverse subdirectories recursively when `true`. |
| `max_depth` | integer | No | Maximum recursion depth when `recursive` is `true`. |
| `limit` | integer | No | Maximum number of directory entries to return. |
| `include_hidden`| boolean | No | Include dot-prefixed hidden entries when `true`. |
| `ignore` | array of strings | No | Glob patterns to exclude from listing (e.g. `["node_modules/**"]`). |

#### Output Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `path` | string | Normalized directory path listed. |
| `entries` | array of objects | List of entries: `path` (string), `name` (string), `is_dir` (boolean), `size` (int64). |
| `truncated` | boolean | `true` if entries exceeded `limit`. |

```json
{
  "path": "internal/daemon",
  "recursive": false
}
```

---

### `glob`

Matches workspace file paths using standard wildcard glob expressions.

- **Annotations**: `isReadOnly: true`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`
- **User Hint**: `"Glob pattern: %s"`

#### Input Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `pattern` | string | **Yes** | Wildcard glob pattern (e.g. `**/*.go`, `src/**/*.tsx`). |
| `path` | string | No | Base directory from which to search (defaults to `"."`). |
| `include_hidden`| boolean | No | Include dot-prefixed hidden files and directories. |
| `limit` | integer | No | Maximum number of matched paths to return. |
| `ignore` | array of strings | No | Additional glob patterns to skip. |

#### Output Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `pattern` | string | Glob pattern evaluated. |
| `path` | string | Base directory searched. |
| `matches` | array of strings | Sorted list of matching workspace file paths. |
| `truncated` | boolean | `true` if matches were truncated at `limit`. |

```json
{
  "pattern": "**/*.tool.md",
  "path": "."
}
```

---

### `grep`

Searches file contents across the workspace using regular expressions.

- **Annotations**: `isReadOnly: true`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`
- **User Hint**: `"Search pattern: %s"`

#### Input Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `pattern` | string | **Yes** | Regular expression pattern to search for. |
| `path` | string | No | File or directory path under which to search. |
| `include` | string | No | Optional glob pattern filtering target file names (e.g. `*.go`). |
| `case_insensitive`| boolean | No | Enable case-insensitive regular expression matching. |
| `context_lines` | integer | No | Number of surrounding lines to capture before and after each match. |
| `include_hidden`| boolean | No | Search dot-prefixed hidden files and directories. |
| `limit` | integer | No | Maximum number of matches to return. |

#### Output Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `pattern` | string | Regular expression pattern evaluated. |
| `matches` | array of objects | Matches with `path`, `line` (1-based), `column` (1-based), `text`, `before` (lines), `after` (lines). |
| `truncated` | boolean | `true` if matches were truncated at `limit`. |

```json
{
  "pattern": "func NewServer",
  "include": "*.go",
  "context_lines": 2
}
```

---

### `write`

Creates a new file or completely replaces the contents of an existing file. Enforces compare-and-swap checking against file modifications.

- **Annotations**: `isReadOnly: false`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`
- **User Hint**: `"Write file: %s"`

#### Input Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `path` | string | **Yes** | Target file path within the workspace. |
| `content` | string | **Yes** | Full text content to write to the file. |
| `force` | boolean | No | Bypass read-before-overwrite and compare-and-swap safety checks. |

#### Output Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `path` | string | Normalized target file path. |
| `created` | boolean | `true` if a new file was created rather than overwritten. |
| `bytes_written` | integer | Total bytes written to disk. |
| `additions` | integer | Number of added lines. |
| `deletions` | integer | Number of deleted lines. |
| `before_hash` | string | Previous SHA-256 hex digest when overwriting. |
| `after_hash` | string | New SHA-256 hex digest of the written file. |
| `diff` | string | Unified diff of the mutation. |

```json
{
  "path": "config/settings.yaml",
  "content": "port: 8080\nenv: production\n"
}
```

---

### `edit`

Performs an exact literal text search and replacement on an existing file, generating a unified diff.

- **Annotations**: `isReadOnly: false`, `isIdempotent: false`, `isDangerous: false`, `isOpenWorld: false`
- **User Hint**: `"Edit file: %s"`

#### Input Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `path` | string | **Yes** | Target file path within the workspace. |
| `old_text` | string | **Yes** | Exact literal substring to search for and replace. |
| `new_text` | string | **Yes** | Replacement text string. |
| `replace_all` | boolean | No | Replace every occurrence of `old_text` when `true` (defaults to single match). |
| `force` | boolean | No | Bypass read-before-overwrite and compare-and-swap safety checks. |

#### Output Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `path` | string | Normalized file path edited. |
| `replacements` | integer | Number of replacements applied. |
| `additions` | integer | Number of added lines. |
| `deletions` | integer | Number of deleted lines. |
| `before_hash` | string | SHA-256 hex digest before editing. |
| `after_hash` | string | SHA-256 hex digest after editing. |
| `diff` | string | Unified diff of the edit. |

```json
{
  "path": "server.go",
  "old_text": "port := 8080",
  "new_text": "port := 9090"
}
```

---

### `remove`

Safely removes a file from the workspace, recording deletion diffs and hash tracking.

- **Annotations**: `isReadOnly: false`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`
- **User Hint**: `"Remove file: %s"`

#### Input Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `path` | string | **Yes** | Target file path within the workspace to remove. |
| `force` | boolean | No | Bypass read-before-overwrite and compare-and-swap checks. |

#### Output Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `path` | string | Normalized file path removed. |
| `deletions` | integer | Number of deleted lines. |
| `before_hash` | string | SHA-256 hex digest of the file prior to removal. |
| `diff` | string | Unified deletion diff. |

```json
{
  "path": "tmp/obsolete.txt"
}
```

---

### `bash`

Executes a shell command inside the virtualized workspace sandbox. Supports synchronous execution with timeouts or transitioning long-running jobs to managed background tasks.

- **Annotations**: `isReadOnly: false`, `isIdempotent: false`, `isDangerous: true`, `isOpenWorld: true`
- **User Hint**: `"Run command: %s"`

#### Input Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `command` | string | **Yes** | Shell command string passed to `sh -c`. |
| `description` | string | No | Clear, concise description of the command (recommended 3-6 words for UI display). |
| `dir` | string | No | Optional working directory relative to sandbox root. |
| `run_in_background` | boolean | No | Set to `true` to immediately transition to background task execution and return `task_id`. |
| `wait_ms` | integer | No | Milliseconds to wait before transitioning to background. Defaults to 60000ms (or 500ms when `run_in_background` is true). |

#### Output Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `output` | string | Combined standard output and standard error from the command. |
| `exit_code` | integer | Process exit status code (0 for success). |
| `duration_ms` | integer (int64) | Execution duration in milliseconds. |
| `status` | string | Status: `completed`, `running`, `failed`, or `canceled`. |
| `task_id` | string | Background task ID if execution transitioned to the background. |

```json
{
  "command": "go test ./...",
  "description": "Run package unit tests"
}
```

---

### `web_fetch`

Fetches web documents over HTTP or HTTPS and returns decoded text content.

- **Annotations**: `isReadOnly: true`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: true`
- **User Hint**: `"Fetch URL: %s"`

#### Input Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `url` | string | **Yes** | Target HTTP or HTTPS URL to fetch. |
| `headers` | map of strings | No | Optional HTTP request headers (e.g. `{"Accept": "application/json"}`). |

#### Output Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `url` | string | Normalized request URL. |
| `status_code` | integer | HTTP response status code (e.g. 200). |
| `content_type` | string | Response `Content-Type` header. |
| `text` | string | Decoded text response body. |

```json
{
  "url": "https://raw.githubusercontent.com/masterkeysrd/tasksmith/main/README.md"
}
```

---

### `download`

Downloads a remote file over HTTP or HTTPS directly to a designated workspace path.

- **Annotations**: `isReadOnly: false`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: true`
- **User Hint**: `"Download %s to %s"`

#### Input Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `url` | string | **Yes** | Source HTTP or HTTPS URL to download. |
| `path` | string | **Yes** | Destination file path within the workspace. |
| `headers` | map of strings | No | Optional HTTP request headers. |
| `max_bytes` | integer (int64) | No | Maximum allowed download size in bytes. |
| `timeout_ms` | integer | No | Download timeout in milliseconds. |
| `force` | boolean | No | Bypass read-before-overwrite and compare-and-swap checks. |

#### Output Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `url` | string | Source URL downloaded. |
| `path` | string | Normalized destination file path in the workspace. |
| `status_code` | integer | HTTP status code returned by upstream server. |
| `bytes_written` | integer | Number of bytes written to disk. |
| `created` | boolean | `true` if a new file was created. |
| `after_hash` | string | New SHA-256 hex digest of the downloaded file. |
| `before_hash` | string | Previous SHA-256 hex digest when overwriting. |
| `content_type` | string | Upstream `Content-Type` header. |
| `is_binary` | boolean | `true` if content was detected as binary data. |

```json
{
  "url": "https://example.com/data.json",
  "path": "assets/data.json"
}
```

---

### `tasks`

Manages background tasks initiated by `bash` or asynchronous jobs.

- **Annotations**: `isReadOnly: false`, `isIdempotent: false`, `isDangerous: false`, `isOpenWorld: false`
- **User Hint**: `"Manage tasks: %s"`

#### Input Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `action` | string | **Yes** | Action to perform: `list`, `status`, `kill`, or `send_input`. |
| `task_id` | string | Conditional | Background task ID (required for `status`, `kill`, and `send_input`). |
| `input` | string | Conditional | Input string to write to standard input (required for `send_input`). |
| `limit` | integer | No | Maximum trailing log lines to return for `status` (default: 100). |

#### Output Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `tasks` | array of objects | Task list (for `list`), each with `id`, `kind`, `title`, `command`, `status`, `duration_ms`, `exit_code`, `log_path`. |
| `status` | string | Status of requested task (`running`, `completed`, `failed`, `canceled`). |
| `output` | string | Trailing output lines from task log (for `status`). |
| `exit_code` | integer | Process exit code when finished. |
| `log_path` | string | Absolute path to full output log file on disk. |
| `message` | string | Human-readable action outcome summary. |

```json
{
  "action": "status",
  "task_id": "task_01j7bx8...",
  "limit": 50
}
```

---

### `todos`

Maintains the agent's task checklist and milestone tracking throughout a multi-step session.

- **Annotations**: `isReadOnly: false`, `isIdempotent: true`, `isDangerous: false`, `isOpenWorld: false`
- **User Hint**: `"Update session checklist"`

#### Input Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `todos` | array of objects | **Yes** | Complete checklist of tasks. Each item contains `content` (string), `active_text` (string present-tense action), and `status` (`pending`, `in_progress`, `completed`). |

#### Output Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `todos` | array of objects | Updated checklist items. |
| `total` | integer | Total number of todos in checklist. |
| `completed` | integer | Number of completed todos. |
| `in_progress` | integer | Number of in-progress todos. |
| `pending` | integer | Number of pending todos. |
| `message` | string | Summary message describing checklist progress. |

```json
{
  "todos": [
    {
      "content": "Implement auth middleware",
      "active_text": "Implementing auth middleware",
      "status": "completed"
    },
    {
      "content": "Add unit tests for token parsing",
      "active_text": "Writing token parsing unit tests",
      "status": "in_progress"
    }
  ]
}
```

---

### `ask_question`

Prompts the user with structured interactive questions to resolve ambiguities or confirm architectural choices.

- **Annotations**: `isReadOnly: true`, `isIdempotent: false`, `isDangerous: false`, `isOpenWorld: false`
- **User Hint**: `"Ask user: %s"`

#### Input Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `questions` | array of objects | **Yes** | List of 1 to 5 structured questions to present to the user. |
| `title` | string | No | Header / confirmation title when asking multiple questions. |
| `description` | string | No | Context summary explaining what will happen based on answers. |

Each item in `questions` includes:
- `type`: Question kind (`single`, `multi`, `text`, or `confirm`).
- `question`: Short, direct one-line question string.
- `description`: Required context below the question explaining tradeoffs (max 300 chars).
- `label`: Optional short tab header (max 3 words).
- `options`: List of 2 to 5 choices for `single` and `multi` questions (each option with `id`, `label`, and optional `description`).

#### Output Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `answers` | array of objects | User responses: `question` (string), `selected` (array of string IDs), `text` (string), and `confirmed` (boolean for confirmation questions only). |
| `canceled` | boolean | `true` if the user dismissed or canceled the prompt without submitting. |

```json
{
  "title": "Select Database Backend",
  "description": "Choose the persistent storage engine for the service.",
  "questions": [
    {
      "type": "single",
      "question": "Which database engine should be used?",
      "description": "PostgreSQL provides full relational features; SQLite enables single-binary embedded operation.",
      "options": [
        {
          "id": "sqlite",
          "label": "(Recommended) SQLite",
          "description": "Zero external dependencies, single file on disk."
        },
        {
          "id": "postgres",
          "label": "PostgreSQL",
          "description": "Requires external Postgres instance."
        }
      ]
    }
  ]
}
```

---

### `load_skill`

Loads and renders instructions, coding conventions, or runbooks for a specialized workspace skill into the agent's context on demand.

#### Safety Profile
- **`isReadOnly`**: `true`
- **`isIdempotent`**: `true`
- **`isDangerous`**: `false`
- **`isOpenWorld`**: `false`
- **User Hint**: `"Load skill knowledge"`

#### Input Parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `skill` | string | **Yes** | Identifier of the skill to load (e.g. `'postgres-migrations'`, `'docker-deploy'`). |
| `arguments` | object | No | Optional key-value parameters passed to render the skill template. |

#### Output Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `skill` | string | Name of the skill that was loaded. |
| `instructions` | string | Rendered instructions and guidelines populated from the skill package. |

```json
{
  "skill": "docker-deploy",
  "arguments": {
    "image": "my-service",
    "tag": "v1.2.0"
  }
}
```
