---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: glob
  labels:
    category: filesystem
spec:
  parameters:
    type: object
    properties:
      pattern:
        type: string
        description: Glob pattern to match.
    required: ["pattern"]
  outputSchema:
    type: object
    properties:
      matches:
        type: array
        items:
          type: string
        description: List of matching file paths.
---
Find files matching a glob pattern.
