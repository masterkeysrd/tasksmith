---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: lsp_restart
  labels:
    category: lsp
spec:
  inputSchema:
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
Restart an LSP server. Use when `lsp_diagnostics` returns stale, empty, or unexpected results. Pass the server name as registered in the workspace (e.g. `gopls`, `typescript-language-server`).
