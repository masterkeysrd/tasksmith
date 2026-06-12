---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: remove
  labels:
    category: filesystem
spec:
  command: ["rm", "-rf"]
  description: Remove a file or directory.
  parameters:
    type: object
    properties:
      path:
        type: string
        description: Path to remove.
    required: ["path"]
---
