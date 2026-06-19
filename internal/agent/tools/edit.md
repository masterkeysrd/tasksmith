---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: edit
  labels:
    category: filesystem
spec:
  command: ["sed", "-i"]
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
  outputSchema:
    type: object
    properties:
      path:
        type: string
        description: Path to the edited file.
      success:
        type: boolean
        description: Whether the edit succeeded.
---
Edit a file using sed.
