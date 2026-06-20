---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: lsp_diagnostics
  labels:
    category: lsp
spec:
  parameters:
    type: object
    properties:
      path:
        type: string
        description: Path to the file or directory.
    required: ["path"]
  outputSchema:
    type: object
    properties:
      diagnostics:
        type: array
        items:
          type: object
        description: List of LSP diagnostics.
---
Get LSP diagnostics.
