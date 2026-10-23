---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: remove
  displayName: Remove File
  description: Delete an existing file from the workspace filesystem.
spec:
  annotations:
    isReadOnly: false
    isIdempotent: false
    isDangerous: true
    isOpenWorld: false
    userHint: "Remove file: %s"
  inputSchema:
    type: object
    properties:
      path:
        type: string
        description: Target file path within the workspace to remove.
      force:
        type: boolean
        description: Bypass read-before-overwrite and Compare-And-Swap checks.
    required:
      - path
  outputSchema:
    type: object
    properties:
      path:
        type: string
        description: Normalized file path removed.
      deletions:
        type: integer
        description: Number of deleted lines.
      diff:
        type: string
        description: Unified deletion diff.
      before_hash:
        type: string
        description: SHA-256 hex digest of the file prior to removal.
    required:
      - path
      - deletions
      - before_hash
---

Use the `remove` tool to delete an existing file at `path` inside the workspace.
The target file must be read with `view` prior to removal unless `force` is set.
