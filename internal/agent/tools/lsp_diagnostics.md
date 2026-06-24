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
        description: List of LSP diagnostics.
        items:
          type: object
          properties:
            path:
              type: string
              description: File path containing the diagnostic.
            message:
              type: string
              description: Diagnostic message.
            severity:
              type: string
              description: "Severity level: error, warning, info, hint."
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
      total_count:
        type: integer
        description: Total number of diagnostics found.
      truncated:
        type: boolean
        description: True if diagnostics were truncated due to length limits.
---
Get LSP diagnostics.
