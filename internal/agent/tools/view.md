---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: view
  labels:
    category: filesystem
spec:
  command: ["cat"]
  parameters:
    type: object
    properties:
      path:
        type: string
        description: Path to the file.
    required: ["path"]
  outputSchema:
    type: object
    properties:
      content:
        type: string
        description: Content of the file.
---
View the contents of a file.
