---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: agent_invoke
  labels:
    category: workflow
spec:
  inputSchema:
    type: object
    properties:
      subagents:
        type: array
        items:
          type: object
          properties:
            type_name:
              type: string
              description: The defined type name of the subagent to invoke.
            role:
              type: string
              description: A brief role/job title for this invocation (e.g. Code Researcher).
            prompt:
              type: string
              description: The specific instruction task description for the subagent to start.
            workspace:
              type: string
              enum: ["inherit", "branch", "share"]
              description: Workspace isolation mode. Defaults to inherit.
          required: ["type_name", "role", "prompt"]
    required: ["subagents"]
  outputSchema:
    type: object
    properties:
      subagents:
        type: array
        items:
          type: object
          properties:
            conversation_id:
              type: string
              description: The unique ID assigned to this subagent conversation thread.
            type_name:
              type: string
              description: Subagent type.
            role:
              type: string
              description: Role description.
      success:
        type: boolean
        description: Whether invoking the subagents succeeded.
      error:
        type: string
        description: Error message if the invocation failed.
---
Invoke one or more subagents concurrently to perform background tasks.
