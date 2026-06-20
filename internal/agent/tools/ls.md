---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: ls
  labels:
    category: filesystem
spec:
  parameters:
    type: object
    properties:
      path:
        type: string
        description: Path to the directory.
    required: ["path"]
  outputSchema:
    type: object
    properties:
      files:
        type: array
        items:
          type: object
        description: List of files and directories.
---
List files in a directory with type indicators.
