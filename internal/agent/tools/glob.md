---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: glob
  labels:
    category: filesystem
spec:
  inputSchema:
    type: object
    properties:
      pattern:
        type: string
        description: Glob pattern to match.
      path:
        type: string
        description: Base directory to search from. Defaults to the workspace directory.
    required: ["pattern"]
  outputSchema:
    type: object
    properties:
      matches:
        type: array
        items:
          type: string
        description: List of matching file paths.
      total_count:
        type: integer
        description: Total number of matches after applying ignore filters.
      truncated:
        type: boolean
        description: True when the result was capped by the limit.
---
Find files matching a glob pattern. Prefer this over `bash` (e.g. `find`, `ls`) for file discovery — it is faster, safer, and automatically respects `.gitignore` rules.

<guidelines>
- A bare `*` is automatically expanded to `**/*` — it will match all files recursively.
- Use `**` for explicit recursive matching (e.g. `**/*.go`, `src/**/*.ts`).
- Use `path` to scope the search to a subdirectory instead of the entire workspace.
- If `truncated` is true, results were capped — narrow the pattern or scope with `path`.
- Combine with `grep` to first find files then search their contents.
</guidelines>
