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
        description: List of search matches.
---
Search for a pattern in files.
