---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: multi_edit
  labels:
    category: filesystem
spec:
  parameters:
    type: object
    properties:
      path:
        type: string
        description: Path to the file.
      edits:
        type: array
        items:
          type: object
          properties:
            target:
              type: string
              description: A block of code copied verbatim from the viewed file. Must be a unique, contiguous sequence of lines.
            replacement:
              type: string
              description: The replacement content for the target block.
            replace_all:
              type: boolean
              description: If true, replaces all occurrences of the target block. If false (default), fails if the target block is not unique.
          required: ["target", "replacement"]
        description: A list of edits to apply to the file.
    required: ["path", "edits"]
  outputSchema:
    type: object
    properties:
      path:
        type: string
        description: Path to the file.
      success:
        type: boolean
        description: Whether any edits were successfully applied.
      diff:
        type: string
        description: The unified diff showing all successfully applied changes.
      additions:
        type: integer
        description: The total number of lines added.
      deletions:
        type: integer
        description: The total number of lines deleted.
      results:
        type: array
        items:
          type: object
          properties:
            success:
              type: boolean
              description: Whether this specific edit was applied.
            message:
              type: string
              description: Failure reason if this specific edit failed.
          required: ["success"]
        description: The status of each edit in the edits list.
      diagnostics:
        type: string
        description: LSP diagnostics for this file.
---
Apply multiple, non-contiguous edits to a single file in a single turn.

<guidelines>
- You MUST `view` the file first — unviewed or externally modified files will be rejected.
- Each `target` must be copied character-for-character from the viewed file output — do not re-type or reformat it.
- Edits are applied top-to-bottom — each `target` must match the file as it exists *after* prior edits. Avoid overlapping targets.
- Keep each `target` to the smallest unique block (3-20 lines) — never target an entire function.
- If changes are large enough to touch most of the file, prefer `write` for a clean full rewrite instead.
</guidelines>
