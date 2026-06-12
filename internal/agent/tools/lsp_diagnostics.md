---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: lsp_diagnostics
  labels:
    category: lsp
spec:
  command: ["lsp-client", "diagnostics"]
  description: Get LSP diagnostics.
  parameters:
    type: object
    properties:
      path:
        type: string
        description: Path to the file or directory.
    required: ["path"]
---
