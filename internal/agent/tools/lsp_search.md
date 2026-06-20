---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: lsp_search
  labels:
    category: lsp
spec:
  parameters:
    type: object
    properties:
      query:
        type: string
        description: Search query.
    required: ["query"]
  outputSchema:
    type: object
    properties:
      results:
        type: array
        items:
          type: object
        description: List of LSP search results.
---
Search using LSP.
