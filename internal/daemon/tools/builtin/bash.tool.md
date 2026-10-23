---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: bash
  displayName: Execute Shell Command
  description: Execute a shell command inside the workspace sandbox.
spec:
  annotations:
    isReadOnly: false
    isIdempotent: false
    isDangerous: true
    isOpenWorld: true
    userHint: "Run command: %s"
  inputSchema:
    type: object
    properties:
      command:
        type: string
        description: Shell command string passed to sh -c.
      dir:
        type: string
        description: Optional working directory relative to the sandbox root.
      env:
        type: object
        description: Optional per-command environment variable overrides.
        additionalProperties:
          type: string
      stdin:
        type: string
        description: Optional standard input passed to the command.
      timeout_ms:
        type: integer
        description: Optional execution timeout in milliseconds.
    required:
      - command
  outputSchema:
    type: object
    properties:
      stdout:
        type: string
        description: Captured standard output from the command.
      stderr:
        type: string
        description: Captured standard error from the command.
      exit_code:
        type: integer
        description: Process exit status code.
      duration_ms:
        type: integer
        format: int64
        description: Execution duration in milliseconds.
    required:
      - stdout
      - stderr
      - exit_code
      - duration_ms
---

Use the `bash` tool to run build, test, git, or utility commands via `sh -c` in the workspace sandbox.
Prefer dedicated file and search tools (`view`, `ls`, `glob`, `grep`, `write`, `edit`, `remove`) over shell equivalents.
