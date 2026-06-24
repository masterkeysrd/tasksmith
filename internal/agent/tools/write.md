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
      bytes_written:
        type: integer
        description: The number of bytes written to the file.
      success:
        type: boolean
        description: Whether the file was written successfully.
      diagnostics:
        type: string
        description: LSP diagnostics for this file.
---
Write content to a file.
