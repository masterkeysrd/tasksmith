---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: edit
  displayName: Edit File
  description: Replace an exact literal text block in an existing workspace file.
spec:
  annotations:
    isReadOnly: false
    isIdempotent: false
    isDangerous: false
    isOpenWorld: false
    userHint: "Edit file: %s"
  inputSchema:
    type: object
    properties:
      path:
        type: string
        description: Target file path within the workspace.
      old_text:
        type: string
        description: Exact literal substring to search for and replace.
      new_text:
        type: string
        description: Replacement text string.
      replace_all:
        type: boolean
        description: Replace every occurrence of old_text when true.
      force:
        type: boolean
        description: Bypass read-before-overwrite and Compare-And-Swap checks.
    required:
      - path
      - old_text
      - new_text
  outputSchema:
    type: object
    properties:
      path:
        type: string
        description: Normalized file path edited.
      replacements:
        type: integer
        description: Number of replacements applied.
      additions:
        type: integer
        description: Number of added lines.
      deletions:
        type: integer
        description: Number of deleted lines.
      diff:
        type: string
        description: Unified diff of the edit.
      before_hash:
        type: string
        description: SHA-256 hex digest before editing.
      after_hash:
        type: string
        description: SHA-256 hex digest after editing.
    required:
      - path
      - replacements
      - additions
      - deletions
      - before_hash
      - after_hash
---

Use the `edit` tool to perform targeted literal string replacements in an existing text file.
Always read the file with `view` first. Provide `path`, the exact `old_text` (including surrounding context and indentation), and `new_text`. Set `replace_all` to `true` only when replacing every occurrence in the file.
