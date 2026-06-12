---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: bash
  labels:
    category: system
spec:
  command: ["bash", "-c"]
  description: Execute a bash command.
  parameters:
    type: object
    properties:
      command:
        type: string
        description: Bash command to execute.
    required: ["command"]
---
