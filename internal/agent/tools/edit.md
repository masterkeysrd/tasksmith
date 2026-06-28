---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: edit
  labels:
    category: filesystem
spec:
  parameters:
    type: object
    properties:
      path:
        type: string
        description: Path to the file.
      target:
        type: string
        description: A block of code copied verbatim from the viewed file. Must be a unique, contiguous sequence of lines.
      replacement:
        type: string
        description: The replacement content for the target block.
      replace_all:
        type: boolean
        description: If true, replaces all occurrences of the target block. If false (default), fails if the target block is not unique.
    required: ["path", "target", "replacement"]
  outputSchema:
    type: object
    properties:
      path:
        type: string
        description: Path to the edited file.
      success:
        type: boolean
        description: Whether the edit succeeded.
      diff:
        type: string
        description: The unified diff showing the changes made.
      additions:
        type: integer
        description: The number of lines added.
      deletions:
        type: integer
        description: The number of lines deleted.
      message:
        type: string
        description: Error or status message if the edit failed.
      diagnostics:
        type: string
        description: LSP diagnostics for this file.
---
Edit a file by replacing a target block of text with a replacement.

<guidelines>
- You MUST `view` the file first — unviewed or externally modified files will be rejected.
- The `target` must be copied character-for-character from the viewed file output — do not re-type or reformat it.
- Include enough surrounding context in `target` (3-5 lines) to ensure uniqueness — avoid single-line targets.
- If changes are large enough to touch most of the file, prefer `write` for a clean full rewrite instead.
</guidelines>
