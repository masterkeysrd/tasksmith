---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: remove
  labels:
    category: filesystem
spec:
  parameters:
    type: object
    properties:
      path:
        type: string
        description: Path to remove.
      recursive:
        type: boolean
        description: Must be set to true to remove directories recursively. Defaults to false.
    required: ["path"]
  outputSchema:
    type: object
    properties:
      path:
        type: string
        description: Path that was removed.
      success:
        type: boolean
        description: Whether removal succeeded.
---
Remove a file or directory.
