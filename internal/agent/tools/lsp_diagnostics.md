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
Retrieve LSP diagnostics (errors, warnings, hints) for a file or directory. Use this to verify correctness after edits or to investigate existing issues before making changes.

<guidelines>
- `path` can be a single file or a directory — directory mode aggregates diagnostics across all open files within it.
- Severity levels: `error` (must fix), `warning` (should review), `info`, `hint`.
- Diagnostics are also returned inline by `edit`, `multi_edit`, and `write` — call this tool explicitly when you need a broader view or want to check before editing.
- If the LSP server has just started, diagnostics may be incomplete; use `lsp_restart` if results appear stale or empty unexpectedly.
- `range` positions are zero-based — add 1 to `line` to get the line number for `view`.
</guidelines>
