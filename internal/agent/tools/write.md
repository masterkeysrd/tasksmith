---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: write
  labels:
    category: filesystem
spec:
  parameters:
    type: object
    properties:
      path:
        type: string
        description: Path to the file.
      content:
        type: string
        description: Content to write.
    required: ["path", "content"]
  outputSchema:
    type: object
    properties:
      path:
        type: string
        description: Path to the written file.
---
Write content to a file.
