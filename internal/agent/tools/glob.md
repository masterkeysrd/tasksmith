---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: glob
  labels:
    category: filesystem
spec:
  command: ["find"]
  description: Find files matching a glob pattern.
  parameters:
    type: object
    properties:
      pattern:
        type: string
        description: Glob pattern to match.
    required: ["pattern"]
---
