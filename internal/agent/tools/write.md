---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: write
  labels:
    category: filesystem
spec:
  inputSchema:
    type: object
    properties:
      path:
        type: string
        description: Path to the file.
      content:
        type: string
        description: Content to write.
    required: ["path", "content"]
  outputSchema:
    type: object
    properties:
      path:
        type: string
        description: Path to the written file.
      bytes_written:
        type: integer
        description: The number of bytes written to the file.
      success:
        type: boolean
        description: Whether the file was written successfully.
      diagnostics:
        type: string
        description: LSP diagnostics for this file.
---
Write content to a file, creating it if it does not exist or overwriting it entirely if it does.

<when-to-use>
- Creating a new file from scratch.
- Replacing the entire content of an existing file when most of it is changing.
</when-to-use>

<guidelines>
- You MUST `view` the file first when overwriting an existing one — externally modified files will be rejected.
- For partial changes to an existing file, prefer `edit` or `multi_edit` over a full rewrite.
- Review and resolve any LSP warnings/hints returned under `<lsp-diagnostics>` if possible to ensure code quality.
</guidelines>

