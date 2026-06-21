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
      path:
        type: string
        description: Path to the file.
      start_line:
        type: integer
        description: The start line of the returned content.
      total_lines:
        type: integer
        description: Total number of lines in the file.
      truncated:
        type: boolean
        description: Whether the file content was truncated due to size limits.
---
Reads the contents of a file.

Important Usage Rules:
- Large files will be automatically truncated to fit your context window.
- Extremely long single lines (like base64 strings or minified code) will be automatically omitted to protect your context.
- If the output ends with a [SYSTEM NOTE] stating the file was truncated, do not assume you have the full file context. You must call `view` again using the `start_line` parameter provided in the system note to paginate and read the remainder of the file before making final decisions or edits.
