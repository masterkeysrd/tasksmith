---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: remove
  labels:
    category: filesystem
spec:
  command: ["rm", "-rf"]
  parameters:
    type: object
    properties:
      path:
        type: string
        description: Path to remove.
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
