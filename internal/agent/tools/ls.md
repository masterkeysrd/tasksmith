---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: ls
  labels:
    category: filesystem
spec:
  parameters:
    type: object
    properties:
      path:
        type: string
        description: Path to the directory to list.
      pattern:
        type: string
        description: Optional glob pattern to filter entries by name (e.g. "*.go", "test_*").
      type:
        type: string
        description: Optional entry type filter. One of "file", "dir", or "symlink".
      limit:
        type: integer
        description: Maximum number of entries to return. Defaults to 200.
      detailed:
        type: boolean
        description: If true, includes detailed information such as permissions, owner, group, and modification time. Use this only when necessary as it is more verbose.
      depth:
        type: integer
        description: "Recursion depth. Defaults to 1 (current directory only). Maximum is 4. Use for shallow directory exploration. For deep searches, use glob."
    required: ["path"]
  outputSchema:
    type: object
    properties:
      files:
        type: array
        items:
          type: object
          properties:
            name:
              type: string
              description: File or directory name.
            permissions:
              type: string
              description: File mode string (e.g. -rw-r--r-- or drwxr-xr-x).
            links:
              type: integer
              description: Number of hard links.
            owner:
              type: string
              description: Owner username.
            group:
              type: string
              description: Group name.
            size:
              type: integer
              description: Size in bytes.
            modified:
              type: string
              description: Last modification time (RFC3339).
            is_dir:
              type: boolean
              description: Whether the entry is a directory.
            is_symlink:
              type: boolean
              description: Whether the entry is a symbolic link.
            link_target:
              type: string
              description: Symlink target path (only present when is_symlink is true).
            depth:
              type: integer
              description: Relative depth of the entry (0 for the direct content of the path).
            name_truncated:
              type: boolean
              description: Whether the name was truncated in the formatted field due to excessive length.
        description: List of matching directory entries, after filtering and ignore rules.
      total_count:
        type: integer
        description: Total number of entries after applying ignore and type/pattern filters.
      truncated:
        type: boolean
        description: True when the result was capped by the limit and more entries exist.
      detailed:
        type: boolean
        description: True if the result contains detailed information.
---
List the direct contents of a directory (optionally recursive up to depth 4). By default, it returns a simple tree-like list of names to save context space. Use the `detailed` flag when you need information such as permissions, sizes, and modification times. Use `view` for file contents and `glob`/`grep` for recursive or pattern-based searches across the workspace.

<ignore_rules>
Entries are filtered through two tiers automatically:

- **Predefined**: `.git`, `node_modules`, `vendor`, `dist`, `build`, `.next`, `.venv`, `__pycache__`, `.DS_Store`, and similar noise directories are always excluded.
- **Gitignore**: all `.gitignore` files from the repo root down to the target directory are applied (full git semantics).
</ignore_rules>

<guidelines>
- Use `pattern` to filter entries by filename glob (e.g. `*.go`, `test_*`).
- Use `type` to restrict results to `file`, `dir`, or `symlink`.
- Use `detailed: true` only when you specifically need metadata like permissions, owners, or sizes.
- If `truncated` is true, increase `limit` or narrow with `pattern`/`type` to see the rest.
</guidelines>
