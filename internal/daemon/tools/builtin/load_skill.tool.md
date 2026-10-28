---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: load_skill
  displayName: Load Skill Knowledge
  description: Loads domain instructions, coding conventions, or workflows for a specified skill on demand.
spec:
  annotations:
    isReadOnly: true
    isIdempotent: true
    isDangerous: false
    isOpenWorld: false
    userHint: "Load skill knowledge"
  inputSchema:
    type: object
    properties:
      skill:
        type: string
        description: Name of the skill to load (e.g. 'postgres-migrations', 'git-release').
      arguments:
        type: object
        description: Optional key-value parameters passed to configure the skill template.
        additionalProperties:
          type: string
    required:
      - skill
  outputSchema:
    type: object
    properties:
      skill:
        type: string
        description: Name of the skill that was loaded.
      instructions:
        type: string
        description: Rendered instructions and domain guidelines for the skill.
    required:
      - skill
      - instructions
---
Load and render instructions for a specialized skill from the workspace knowledge base.
