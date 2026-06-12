---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: edit
  labels:
    category: filesystem
spec:
  command: ["sed", "-i"]
  description: Edit a file using sed.
  parameters:
    type: object
    properties:
      path:
        type: string
        description: Path to the file.
      expression:
        type: string
        description: Sed expression.
    required: ["path", "expression"]
---
