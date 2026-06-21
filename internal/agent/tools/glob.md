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
      path:
        type: string
        description: Base directory to search from. Defaults to the workspace directory.
    required: ["pattern"]
  outputSchema:
    type: object
    properties:
      matches:
        type: array
        items:
          type: string
        description: List of matching file paths.
      total_count:
        type: integer
        description: Total number of matches after applying ignore filters.
      truncated:
        type: boolean
        description: True when the result was capped by the limit.
---
Find files matching a glob pattern.
