---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: lsp_search
  labels:
    category: lsp
spec:
  command: ["lsp-client", "search"]
  description: Search using LSP.
  parameters:
    type: object
    properties:
      query:
        type: string
        description: Search query.
    required: ["query"]
---
