---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: manage_agents
  displayName: Manage Subagents
  description: List subagents, check a subagent's status, send a message to a subagent, or kill a running subagent.
spec:
  annotations:
    isReadOnly: false
    isIdempotent: false
    isDangerous: false
    isOpenWorld: false
    userHint: "Manage subagents"
  inputSchema:
    type: object
    properties:
      action:
        type: string
        description: "The action to perform: 'list', 'status', 'send', or 'kill'."
      session_id:
        type: string
        description: "The session ID of the subagent (required for 'status', 'send', and 'kill')."
      message:
        type: string
        description: "Message to send to the subagent (required for 'send')."
    required:
      - action
  outputSchema:
    type: object
    properties:
      agents:
        type: array
        description: "List of child subagents (returned for 'list')."
        items:
          type: object
          title: ManageAgentsAgent
          properties:
            session_id:
              type: string
              description: Unique session identifier of the subagent.
            agent:
              type: string
              description: Agent persona name.
            title:
              type: string
              description: Task summary title.
            status:
              type: string
              description: "Current session status ('idle', 'thinking', 'generating', 'executing', 'waiting_permission', 'waiting_question', 'compacting')."
            work_dir:
              type: string
              description: Working directory of the subagent.
            error:
              type: string
              description: Error message if the subagent turn failed.
          required:
            - session_id
            - agent
            - title
            - status
            - work_dir
      session_id:
        type: string
        description: Session ID of the target subagent.
      agent:
        type: string
        description: Agent name of the target subagent.
      title:
        type: string
        description: Title of the target subagent.
      status:
        type: string
        description: Current status of the target subagent.
      work_dir:
        type: string
        description: Working directory of the target subagent.
      output:
        type: string
        description: Latest assistant response text from the subagent (returned for 'status').
      message:
        type: string
        description: Human-readable summary of the action result.
---
List subagents, check a subagent's status, send a message to a subagent, or kill a running subagent.
