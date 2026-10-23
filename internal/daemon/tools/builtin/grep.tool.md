---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: grep
  displayName: Grep Search
  description: Search text files in the workspace using regular expressions.
spec:
  annotations:
    isReadOnly: true
    isIdempotent: true
    isDangerous: false
    isOpenWorld: false
    userHint: "Search pattern: %s"
  inputSchema:
    type: object
    properties:
      pattern:
        type: string
        description: Regular expression pattern to search for.
      path:
        type: string
        description: File or directory path under which to search.
      include:
        type: string
        description: Optional glob pattern filtering which file paths are searched.
      case_insensitive:
        type: boolean
        description: Enable case-insensitive regular expression matching.
      context_lines:
        type: integer
        description: Number of surrounding lines to capture before and after each match.
      include_hidden:
        type: boolean
        description: Search dot-prefixed hidden files and directories.
      limit:
        type: integer
        description: Maximum number of matches to return.
    required:
      - pattern
  outputSchema:
    type: object
    properties:
      pattern:
        type: string
        description: Regular expression pattern evaluated.
      matches:
        type: array
        description: Regular expression matches across searched files.
        items:
          type: object
          properties:
            path:
              type: string
              description: File path containing the match.
            line:
              type: integer
              description: 1-based line number of the match.
            column:
              type: integer
              description: 1-based column offset of the match.
            text:
              type: string
              description: Full text of the matching line.
            before:
              type: array
              description: Context lines preceding the match.
              items:
                type: string
            after:
              type: array
              description: Context lines following the match.
              items:
                type: string
          required:
            - path
            - line
            - column
            - text
      truncated:
        type: boolean
        description: Whether matches were truncated at limit.
    required:
      - pattern
      - matches
---

Use the `grep` tool to search across text files for lines matching a regular expression `pattern`.
You can scope the search with `path` and `include` file globs, enable `case_insensitive` matching, request surrounding `context_lines`, or cap results with `limit`.
