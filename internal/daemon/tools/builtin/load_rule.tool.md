---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: load_rule
  displayName: Load Rule
  description: Load the full instructions of a workspace rule by name.
spec:
  annotations:
    isReadOnly: true
    isIdempotent: true
    isDangerous: false
    isOpenWorld: false
    userHint: "Load rule"
  inputSchema:
    type: object
    properties:
      rule:
        type: string
        description: Name of the rule to load (e.g. 'go-error-handling', 'security-baseline').
    required:
      - rule
  outputSchema:
    type: object
    properties:
      rule:
        type: string
        description: Name of the rule that was loaded.
      instructions:
        type: string
        description: Rendered instructions and constraints for the rule.
      path:
        type: string
        description: Path to the rule manifest file on disk, relative to the workspace root.
      priority:
        type: integer
        description: Priority of the rule.
      truncated:
        type: boolean
        description: Whether the rule instructions exceeded limits and were truncated.
    required:
      - rule
      - instructions
---
Load and render the full instructions of a workspace rule by name.
