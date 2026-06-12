---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: view
  labels:
    category: filesystem
spec:
  command: ["cat"]
  description: View the contents of a file.
  parameters:
    type: object
    properties:
      path:
        type: string
        description: Path to the file.
    required: ["path"]
---
