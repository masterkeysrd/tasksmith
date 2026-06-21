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
---
List files in a directory in ls -l format.

Entries are filtered using a two-tier ignore system:
1. **Predefined ignores**: `.git`, `.env`, `node_modules`, `__pycache__`, `vendor`, `dist`, `build`, `target`, `.next`, `.nuxt`, `.DS_Store`, `.venv`, `venv`, `coverage`, and similar noise directories are always excluded.
2. **Gitignore rules**: if the target path is inside a git repository, all `.gitignore` files from the repo root down to the target directory are loaded and applied (full git semantics including nested `.gitignore` files).

Use `pattern` to narrow results by filename glob and `type` to restrict to files, directories, or symlinks. Use `limit` (default 200) and check `truncated` + `total_count` to paginate large directories.
