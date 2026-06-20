---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: lsp_restart
  labels:
    category: lsp
spec:
  parameters:
    type: object
    properties:
      server:
        type: string
        description: Name of the LSP server to restart.
    required: ["server"]
  outputSchema:
    type: object
    properties:
      success:
        type: boolean
        description: Whether the LSP server restarted successfully.
      message:
        type: string
        description: Description of the restart outcome.
---
Restart LSP server.
