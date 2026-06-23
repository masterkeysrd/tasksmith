---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: activate_skill
  labels:
    category: system
spec:
  parameters:
    type: object
    properties:
      skill:
        type: string
        description: "The name of the skill to activate (e.g. 'git-expert')."
    required: ["skill"]
  outputSchema:
    type: object
    properties:
      success:
        type: boolean
        description: "Whether the skill was successfully activated."
      instructions:
        type: string
        description: "The rendered instructions and guidelines of the activated skill."
      path:
        type: string
        description: "The absolute filesystem directory path where the skill's resources, scripts, and files are stored."
      message:
        type: string
        description: "A human-readable result or error message."
---
# Activate Skill

Use this tool to dynamically load specialized domain-specific instructions, conventions, style guides, and scripts into your conversation context.

## When to Call
- Look at the **Available Skills** section in your system prompt. Every skill lists a name and a description that acts as a trigger.
- If your current objective or any subtask involves the concepts mentioned in a skill's trigger description, you **MUST** call `activate_skill` to load its instructions before proceeding with the task.
- Call this tool early in your plan so that the guidelines are active when you write code.

## Interpreting Output
- This tool returns the full rendered text of the skill's instructions, which will automatically be injected into your conversation history. Read these instructions carefully.
- The tool also returns the absolute directory `path` where the skill is located. You can run scripts from its `scripts/` directory or read files from its `references/` or `assets/` subdirectories by using standard shell/view tools relative to this absolute path.
