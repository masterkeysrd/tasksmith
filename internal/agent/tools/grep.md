---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: grep
  labels:
    category: filesystem
spec:
  parameters:
    type: object
    properties:
      pattern:
        type: string
        description: Regex pattern to search for.
      path:
        type: string
        description: Directory or file to search in. Defaults to the workspace root.
      include:
        type: string
        description: Glob pattern to restrict which files are searched (e.g. "*.go", "**/*.ts").
      literal:
        type: boolean
        description: If true, treats the pattern as a fixed string instead of a regex. No escaping needed.
    required: ["pattern"]
  outputSchema:
    type: object
    properties:
      matches:
        type: array
        items:
          type: object
          properties:
            path:
              type: string
              description: File path containing the match.
            line:
              type: integer
              description: 1-indexed line number of the match.
            content:
              type: string
              description: Content of the matching line.
        description: List of search matches.
      total_count:
        type: integer
        description: Total number of matches found.
      truncated:
        type: boolean
        description: True when the result was capped by the limit.
---
Search for a regex pattern across files. Prefer this over `bash grep`/`bash rg` — it is faster, respects `.gitignore`, and returns structured results with file paths and line numbers.

<guidelines>
- `path` is optional — omit it to search the entire workspace.
- `pattern` is a regex; escape special characters as needed (e.g. `\.` for a literal dot) — or set `literal: true` to skip escaping entirely.
- Use `include` to restrict the search to specific file types (e.g. `*.go`, `**/*.ts`).
- If `truncated` is true, narrow the pattern or scope `path` to a subdirectory.
- Use `glob` first to find relevant files, then `grep` to search within them.
</guidelines>
