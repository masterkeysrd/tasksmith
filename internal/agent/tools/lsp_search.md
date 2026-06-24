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
        description: List of LSP search results.
        items:
          type: object
          properties:
            name:
              type: string
              description: Symbol name.
            kind:
              type: string
              description: Symbol kind (e.g. Class, Method, Function).
            path:
              type: string
              description: File path containing the symbol.
            container_name:
              type: string
              description: Name of the parent container symbol.
            range:
              type: object
              description: The range in the file.
              properties:
                start:
                  type: object
                  description: Start position.
                  properties:
                    line:
                      type: integer
                      description: Zero-based line number.
                    character:
                      type: integer
                      description: Zero-based character offset.
                end:
                  type: object
                  description: End position.
                  properties:
                    line:
                      type: integer
                      description: Zero-based line number.
                    character:
                      type: integer
                      description: Zero-based character offset.
---
Search using LSP.
