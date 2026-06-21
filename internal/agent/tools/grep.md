---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: grep
  labels:
    category: filesystem
spec:
  parameters:
    type: object
    properties:
      pattern:
        type: string
        description: Regex pattern to search for.
      path:
        type: string
        description: Directory or file to search in.
    required: ["pattern", "path"]
  outputSchema:
    type: object
    properties:
      matches:
        type: array
        items:
          type: object
          properties:
            path:
              type: string
              description: File path containing the match.
            line:
              type: integer
              description: 1-indexed line number of the match.
            content:
              type: string
              description: Content of the matching line.
        description: List of search matches.
      total_count:
        type: integer
        description: Total number of matches found.
      truncated:
        type: boolean
        description: True when the result was capped by the limit.
---
Search for a pattern in files.
