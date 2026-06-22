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
Execute a bash command. If it takes longer than wait_ms, it automatically transitions to a background task.

IMPORTANT: Do NOT run commands in the background of the shell yourself (e.g. by appending '&' or using background commands). The TaskManager handles background execution automatically. If you background the command yourself, the TaskManager will immediately mark it as finished and lose track of it, preventing you from managing or stopping it.

