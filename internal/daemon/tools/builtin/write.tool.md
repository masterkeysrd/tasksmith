---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: write
  displayName: Write File
  description: Create a new text file or overwrite an existing file in the workspace.
spec:
  annotations:
    isReadOnly: false
    isIdempotent: true
    isDangerous: false
    isOpenWorld: false
    userHint: "Write file: %s"
  inputSchema:
    type: object
    properties:
      path:
        type: string
        description: Target file path within the workspace.
      content:
        type: string
        description: Full text content to write to the file.
      force:
        type: boolean
        description: Bypass read-before-overwrite and Compare-And-Swap checks.
    required:
      - path
      - content
  outputSchema:
    type: object
    properties:
      path:
        type: string
        description: Normalized target file path.
      created:
        type: boolean
        description: True if a new file was created rather than overwritten.
      bytes_written:
        type: integer
        description: Number of bytes written to disk.
      additions:
        type: integer
        description: Number of added lines.
      deletions:
        type: integer
        description: Number of deleted lines.
      diff:
        type: string
        description: Unified diff of the file mutation.
      before_hash:
        type: string
        description: Previous SHA-256 hex digest when overwriting an existing file.
      after_hash:
        type: string
        description: New SHA-256 hex digest of the written file.
    required:
      - path
      - created
      - bytes_written
      - additions
      - deletions
      - after_hash
---

Use the `write` tool to create a new file or replace the entire contents of an existing file at `path` with `content`.
When overwriting an existing file, you must first read it using `view` so Compare-And-Swap conflict detection can verify the file has not changed on disk.
