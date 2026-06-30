---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: remove
  labels:
    category: filesystem
spec:
  inputSchema:
    type: object
    properties:
      path:
        type: string
        description: Path to remove.
      recursive:
        type: boolean
        description: Must be set to true to remove directories recursively. Defaults to false.
    required: ["path"]
  outputSchema:
    type: object
    properties:
      path:
        type: string
        description: Path that was removed.
      success:
        type: boolean
        description: Whether removal succeeded.
---
Remove a file or directory.

<guidelines>
- **Text Files**: You MUST view the contents of a text file (using the `view` tool) before deleting it. This ensures you don't accidentally delete important code you haven't inspected.
- **Binary Files**: The view requirement is waived for binary files (e.g., images, compiled binaries). You can delete them directly.
- **Directories**: To remove a directory and all its contents, you MUST set `recursive: true`. Use this with extreme caution.
</guidelines>
