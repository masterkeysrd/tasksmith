---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: lsp_symbols
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
Search for symbol declarations across the workspace by name. Returns the declaration location, kind, and container for each match. Prefer this over `grep` for navigating to a known symbol — it is language-aware and does not require knowing the exact file.

<guidelines>
- Supports partial and fuzzy matching — `MultiEd` will find `MultiEdit`.
- Results are declarations only; use `lsp_inspect` to get the full picture including references and signature.
- If results are ambiguous (e.g. many symbols share a name), make the query more specific (e.g. include the package or container name).
- `range` positions are zero-based — add 1 to `line` to get the line number for `view`.
</guidelines>
