---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: activate_skill
  labels:
    category: system
spec:
  annotations:
    isReadOnly: true
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
Load specialized instructions for a skill into the conversation context.

<when_to_use>
- Check the **Available Skills** in your system prompt — each has a name and trigger description.
- If your task matches a skill's trigger, you MUST call this before proceeding.
- Call early in your plan so the guidelines are active when you do the work.
</when_to_use>

<guidelines>
- The returned `instructions` are automatically injected into context — read them carefully before continuing.
- Use the returned `path` to access the skill's subdirectories with standard tools if needed.
</guidelines>
