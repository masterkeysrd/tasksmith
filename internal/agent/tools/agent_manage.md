---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: agent_manage
  labels:
    category: workflow
spec:
  inputSchema:
    type: object
    properties:
      action:
        type: string
        enum: ["list", "kill", "kill_all"]
        description: The management action to perform.
      conversation_ids:
        type: array
        items:
          type: string
        description: Conversation IDs of subagents to terminate (required for 'kill').
    required: ["action"]
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
            type_name:
              type: string
            role:
              type: string
            status:
              type: string
      success:
        type: boolean
        description: Whether the management action succeeded.
      error:
        type: string
        description: Error message if the action failed.
---
Manage active subagent threads (list, kill, or kill all).
