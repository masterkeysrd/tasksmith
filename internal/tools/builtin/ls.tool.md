---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: ls
  displayName: List Directory
  description: List files and subdirectories within a workspace directory.
spec:
  annotations:
    isReadOnly: true
    isIdempotent: true
    isDangerous: false
    isOpenWorld: false
    userHint: "List directory: %s"
  inputSchema:
    type: object
    properties:
      path:
        type: string
        description: Directory path relative to the workspace root. Defaults to current directory.
      recursive:
        type: boolean
        description: Traverse subdirectories recursively when true.
      max_depth:
        type: integer
        description: Maximum recursion depth when recursive is true.
      include_hidden:
        type: boolean
        description: Include dot-prefixed hidden entries when true.
      ignore:
        type: array
        description: Glob patterns to exclude from the directory listing.
        items:
          type: string
      limit:
        type: integer
        description: Maximum number of directory entries to return.
  outputSchema:
    type: object
    properties:
      path:
        type: string
        description: Normalized directory path listed.
      entries:
        type: array
        description: Directory entries found.
        items:
          type: object
          properties:
            path:
              type: string
              description: Entry path relative to the workspace root.
            name:
              type: string
              description: Base name of the entry.
            is_dir:
              type: boolean
              description: True when the entry is a directory.
            size:
              type: integer
              format: int64
              description: Size in bytes.
          required:
            - path
            - name
            - is_dir
            - size
      truncated:
        type: boolean
        description: Whether the listing was truncated at limit.
    required:
      - path
      - entries
---

Use the `ls` tool to inspect directory entries inside the workspace.
You can optionally set `path` (defaults to `.`), `recursive`, `max_depth`, `include_hidden`, `ignore` glob patterns, and `limit`.
