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
        description: The exact block of code to edit. This must match a unique sequence of lines in the existing file.
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
---
Edit a file by replacing a target block of text with a replacement block.
