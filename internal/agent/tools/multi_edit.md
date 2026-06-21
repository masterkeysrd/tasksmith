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
              description: The exact block of code to edit.
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
---
Apply multiple, non-contiguous edits to a single file. This is highly useful for making multiple related changes across a file in a single turn.
