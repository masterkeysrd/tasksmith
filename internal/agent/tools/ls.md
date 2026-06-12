---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: ls
  labels:
    category: filesystem
spec:
  command: ["ls", "-F"]
  description: List files in a directory with type indicators.
  parameters:
    type: object
    properties:
      path:
        type: string
        description: Path to the directory.
    required: ["path"]
---
