---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: view
  labels:
    category: filesystem
spec:
  parameters:
    type: object
    properties:
      path:
        type: string
        description: Path to the file.
      start_line:
        type: integer
        description: The 1-indexed line number to start reading from (optional, defaults to 1).
      end_line:
        type: integer
        description: The 1-indexed line number to stop reading at (optional).
    required: ["path"]
  outputSchema:
    type: object
    properties:
      content:
        type: string
        description: Content of the file.
      end_line:
        type: integer
        description: The end line of the returned content.
      source:
        type: string
        description: The source path or resource.
      start_line:
        type: integer
        description: The start line of the returned content.
      total_lines:
        type: integer
        description: Total number of lines in the file.
      truncated:
        type: boolean
        description: Whether the file content was truncated due to size limits.
      cached_path:
        type: string
        description: Cached path in workspace session storage.
      mime_type:
        type: string
        description: Detected MIME type of the file.
      is_binary:
        type: boolean
        description: Whether the file is binary.
      diagnostics:
        type: string
        description: LSP diagnostics for this file.
---
Read the contents of a file. Also registers the file as known in the session, which is required before using `edit`, `multi_edit`, or `write` on existing files. Renders images inline. For directories, use `ls` instead.

<guidelines>
- Use `start_line` and `end_line` to read a specific range; omit both to read from the beginning.
- If `truncated` is true, a `[SYSTEM NOTE]` at the end will provide the next `start_line` — paginate with another `view` call before making edits or decisions.
- Extremely long single lines (e.g. base64 strings, minified code) are automatically omitted to protect context.
</guidelines>
