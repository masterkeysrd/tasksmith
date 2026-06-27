---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: bash
  labels:
    category: system
    streaming: "true"
spec:
  parameters:
    type: object
    properties:
      command:
        type: string
        description: Bash command to execute.
      description:
        type: string
        description: A short description of what you are trying to accomplish by running this command.
      wait_ms:
        type: integer
        description: Time (in milliseconds) to wait for synchronous completion before shifting to background execution. Defaults to 10000.
      timeout:
        type: integer
        description: Maximum duration (in seconds) for the command execution before forced termination.
    required: ["command", "description"]
  outputSchema:
    type: object
    properties:
      stdout:
        type: string
        description: Standard output of the command (if finished synchronously).
      stderr:
        type: string
        description: Standard error of the command (if finished synchronously).
      exitCode:
        type: integer
        description: Exit code of the command (if finished synchronously).
      taskId:
        type: string
        description: The ID of the background task if execution transitioned to background.
      status:
        type: string
        description: The current status of the task ('running', 'completed', 'failed', 'killed').
      message:
        type: string
        description: A human-readable description of the execution status.
---
Execute a bash command. If the command takes longer than `wait_ms`, it transitions to a background task and returns a `taskId`.

<background_execution>
- Never append `&` or background commands yourself — the TaskManager handles it automatically.
- Self-backgrounded commands are immediately lost and cannot be managed or stopped.
</background_execution>

<scheduling>
- For long-running processes (dev servers, watchers, builds), set a low `wait_ms` (e.g. 1000–3000) to transition quickly.
- Use the `tasks` tool to monitor (`status`), list (`list`), or terminate (`kill`) background tasks.
</scheduling>

<cross_platform>
- Prefer POSIX syntax: `[ ]` over `[[ ]]`, `$(...)` over backticks.
- Avoid GNU-specific flags; use `uname` to detect the OS when behavior differs (e.g. `sed -i ''` on macOS vs `sed -i` on Linux).
- Use `command -v` instead of `which` to check for executables.
- Do not rely on shell aliases or user profiles; use full paths or explicit `env` invocations.
</cross_platform>
