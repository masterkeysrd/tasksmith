---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: view
  displayName: View File
  description: Read the contents of a text file from the workspace with line numbers.
spec:
  annotations:
    isReadOnly: true
    isIdempotent: true
    isDangerous: false
    isOpenWorld: false
    userHint: "Read file: %s"
  inputSchema:
    type: object
    properties:
      path:
        type: string
        description: File path relative to the workspace root or an absolute path inside the workspace.
      offset:
        type: integer
        description: 1-based line number from which to start reading.
      limit:
        type: integer
        description: Maximum number of lines to return.
    required:
      - path
  outputSchema:
    type: object
    properties:
      path:
        type: string
        description: Normalized workspace-relative file path.
      content:
        type: string
        description: Line-numbered text content of the selected line range.
      start_line:
        type: integer
        description: 1-based first line number returned.
      end_line:
        type: integer
        description: 1-based last line number returned.
      total_lines:
        type: integer
        description: Total number of lines in the file.
      truncated:
        type: boolean
        description: Whether additional lines remain after end_line.
      hash:
        type: string
        description: SHA-256 hex digest of the full file on disk.
    required:
      - path
      - content
      - start_line
      - end_line
      - total_lines
      - hash
---

Use the `view` tool to read the contents of a text file in the workspace.
Specify `path` (relative to the workspace root or an absolute path inside the workspace) and optional `offset` (1-based start line) and `limit` (maximum number of lines to read). Always read a file with `view` before modifying it with `edit` or overwriting it with `write`.
