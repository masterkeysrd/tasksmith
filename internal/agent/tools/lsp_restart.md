---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: lsp_restart
  labels:
    category: lsp
spec:
  command: ["lsp-client", "restart"]
  description: Restart LSP server.
  parameters:
    type: object
    properties:
      server:
        type: string
        description: Name of the LSP server to restart.
    required: ["server"]
---
