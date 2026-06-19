---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: bash
  labels:
    category: system
spec:
  command: ["bash", "-c"]
  parameters:
    type: object
    properties:
      command:
        type: string
        description: Bash command to execute.
    required: ["command"]
  outputSchema:
    type: object
    properties:
      stdout:
        type: string
        description: Standard output.
      stderr:
        type: string
        description: Standard error.
      exitCode:
        type: integer
        description: Exit code.
---
Execute a bash command.
