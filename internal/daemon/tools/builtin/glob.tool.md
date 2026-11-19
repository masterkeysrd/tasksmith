---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: glob
  displayName: Glob Files
  description: Find files in the workspace matching a wildcard glob pattern.
spec:
  annotations:
    isReadOnly: true
    isIdempotent: true
    isDangerous: false
    isOpenWorld: false
    userHint: "Glob pattern: %s"
  inputSchema:
    type: object
    properties:
      pattern:
        type: string
        description: Wildcard glob expression to match file paths.
      path:
        type: string
        description: Base directory from which to walk. Defaults to current directory.
      include_hidden:
        type: boolean
        description: Include hidden dot-prefixed files and directories.
      ignore:
        type: array
        description: Additional glob patterns to skip.
        items:
          type: string
      limit:
        type: integer
        description: Maximum number of matched paths to return.
    required:
      - pattern
  outputSchema:
    type: object
    properties:
      pattern:
        type: string
        description: Glob pattern evaluated.
      path:
        type: string
        description: Base directory searched.
      matches:
        type: array
        description: Sorted list of matching file paths.
        items:
          type: string
      truncated:
        type: boolean
        description: Whether matches were truncated at limit.
    required:
      - pattern
      - path
      - matches
---

Locate workspace files by name or path glob `pattern` (such as `**/*.go` or `src/*.ts`); use `grep` when searching file contents.
