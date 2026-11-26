# Command Execution and Background Tasks

See [sandbox configuration](../../guides/sandbox.md) for process isolation and [permissions](../../guides/permissions.md) for command approvals. [Tool catalog and permission modes](../tools.md).

## `bash`

Executes a shell command inside the virtualized workspace sandbox. Supports synchronous execution with timeouts or transitioning long-running jobs to managed background tasks.

**Safety annotations:** `isReadOnly: false`, `isIdempotent: false`, `isDangerous: true`, `isOpenWorld: true`.

**User hint:** `Run command: %s`. **Default permission:** Depends on command classification.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `command` | string | Yes | Shell command string passed to `sh -c`. |
| `description` | string | No | Clear, concise description of the command (recommended 3-6 words for UI display). |
| `dir` | string | No | Optional working directory relative to sandbox root. |
| `run_in_background` | boolean | No | Set to `true` to immediately transition to background task execution and return `task_id`. Default: `false`. |
| `wait_ms` | integer | No | Milliseconds to wait before transitioning to background. Defaults to 60000ms (or 500ms when `run_in_background` is true). |
| `bypass_sandbox` | boolean | No | Retry a command once on the host after a confirmed OS sandbox failure, such as blocked GPG signing or a test that needs a host socket. The agent explains the blocker in `description`. Requires `allowUnsandboxed` (default true in trusted workspaces, always false in restricted ones) and user approval or a matching `bash_unsandboxed(...)` allow rule. Rejected in read-only sessions. Default: `false`. |

When a command matches an `excludedCommands` pattern in `sandbox.json` (strict AST matching: no redirects, substitutions, variable-expanded arguments, subshells, `cd`, `eval`, or background operators), the plan is flagged unsandboxed automatically while ordinary `bash(...)` permission rules and modes still decide approval.

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `output` | string | Yes | Combined standard output and standard error from the command. |
| `exit_code` | integer | Yes | Process exit status code (0 for success). |
| `duration_ms` | integer (int64) | Yes | Execution duration in milliseconds. |
| `task_id` | string | No | Background task ID if execution transitioned to the background. |
| `status` | string | No | Status: `completed`, `running`, `failed`, or `canceled`. |

```json
{
  "command": "go test ./...",
  "description": "Run package unit tests"
}
```

## `tasks`

Manages background tasks initiated by `bash` or asynchronous jobs.

**Safety annotations:** `isReadOnly: false`, `isIdempotent: false`, `isDangerous: false`, `isOpenWorld: false`.

**User hint:** `Task action: %s`. **Default permission:** List/status allowed; kill/send_input prompt.

### Input parameters

| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `action` | string | Yes | Action to perform: `list`, `status`, `kill`, or `send_input`. |
| `task_id` | string | Conditional | Background task ID (required for `status`, `kill`, and `send_input`). |
| `input` | string | Conditional | Input string to write to standard input (required for `send_input`; only tasks that read stdin interactively, such as background bash commands, accept it). |
| `limit` | integer | No | Maximum trailing log lines to return for `status` (default: 100). |

### Output fields

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `tasks` | array of objects | No | List of background tasks in the session (returned for 'list'). |
| `tasks[].id` | string | Yes | Unique task identifier. |
| `tasks[].kind` | string | Yes | Task kind (e.g., 'bash'). |
| `tasks[].title` | string | Yes | Concise description of the task. |
| `tasks[].command` | string | No | Command or target associated with the task. |
| `tasks[].status` | string | Yes | Current task status ('running', 'completed', 'failed', 'canceled'). |
| `tasks[].exit_code` | integer | No | Process exit status code (when finished). |
| `tasks[].error` | string | No | Error message if the task failed. |
| `tasks[].log_path` | string | No | Path to the full task output log file. |
| `tasks[].duration_ms` | integer (int64) | Yes | Execution duration in milliseconds. |
| `status` | string | No | Status of requested task (`running`, `completed`, `failed`, `canceled`). |
| `exit_code` | integer | No | Process exit code when finished. |
| `log_path` | string | No | Absolute path to full output log file on disk. |
| `output` | string | No | Trailing output lines from task log (for `status`). |
| `message` | string | No | Human-readable action outcome summary. |

```json
{
  "action": "status",
  "task_id": "task_01j7bx8...",
  "limit": 50
}
```
