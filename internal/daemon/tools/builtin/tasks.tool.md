---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: tasks
  displayName: Manage Background Tasks
  description: List, inspect status and logs, send input to, or terminate background tasks.
spec:
  annotations:
    isReadOnly: false
    isIdempotent: false
    isDangerous: false
    isOpenWorld: false
    userHint: "Task action: %s"
  inputSchema:
    type: object
    properties:
      action:
        type: string
        description: "The action to perform: 'list', 'status', 'kill', or 'send_input'."
        enum:
          - list
          - status
          - kill
          - send_input
      task_id:
        type: string
        description: The ID of the background task (required for 'status', 'kill', and 'send_input').
      input:
        type: string
        description: Input string to write to the task's standard input (required for 'send_input').
      limit:
        type: integer
        description: Maximum number of trailing log lines to return for 'status' (defaults to 100).
    required:
      - action
  outputSchema:
    type: object
    properties:
      tasks:
        type: array
        description: List of background tasks in the session (returned for 'list').
        items:
          type: object
          properties:
            id:
              type: string
              description: Unique task identifier.
            kind:
              type: string
              description: Task kind (e.g., 'bash').
            title:
              type: string
              description: Concise description of the task.
            command:
              type: string
              description: Command or target associated with the task.
            status:
              type: string
              description: Current task status ('running', 'completed', 'failed', 'canceled').
            exit_code:
              type: integer
              description: Process exit status code (when finished).
            error:
              type: string
              description: Error message if the task failed.
            log_path:
              type: string
              description: Path to the full task output log file.
            duration_ms:
              type: integer
              format: int64
              description: Execution duration in milliseconds.
          required:
            - id
            - kind
            - title
            - status
            - duration_ms
      status:
        type: string
        description: Current status of the requested task.
      exit_code:
        type: integer
        description: Exit code of the requested task (when finished).
      log_path:
        type: string
        description: Path to the full output log file for the task.
      output:
        type: string
        description: Trailing output lines from the task log (returned for 'status').
      message:
        type: string
        description: Human-readable status or action summary.
---

Manage and monitor background tasks in the current session (`list`, `status`, `kill`, `send_input`).

<guidelines>
- **No polling**: The system automatically notifies and wakes you up when a background task finishes — do **not** poll `status` in a loop waiting for completion.
- **Logs (`status`)**: Returns execution state, exit code, `log_path`, and trailing `limit` lines (default 100); use `view` (`offset`/`limit`) or `grep` directly on `log_path` to search large logs.
- **Interactive stdin & cleanup**: `send_input` only works on tasks reading stdin interactively (background `bash` commands; fixed-input custom tools reject stdin). Use `kill` to stop background servers or watchers once testing is complete.
</guidelines>
