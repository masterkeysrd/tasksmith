---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: invoke_agent
  displayName: Invoke Subagent
  description: Spawn a specialized subagent asynchronously in an isolated child session.
spec:
  annotations:
    isReadOnly: true
    isIdempotent: false
    isDangerous: false
    isOpenWorld: false
    userHint: "Delegate to subagent"
  inputSchema:
    type: object
    properties:
      agent:
        type: string
        description: Name of the subagent to invoke (must be listed in <available_subagents>).
      title:
        type: string
        description: Concise 3-6 word summary of the delegated task for UI display.
      prompt:
        type: string
        description: Detailed task instructions for the subagent.
      workspace:
        type: string
        description: "Workspace mode for the subagent: 'inherit' (default, shares current working directory) or 'worktree' (creates an isolated git worktree)."
    required:
      - agent
      - title
      - prompt
  outputSchema:
    type: object
    properties:
      session_id:
        type: string
        description: Session ID of the spawned subagent.
      agent:
        type: string
        description: Name of the subagent executing the task.
      title:
        type: string
        description: Summary title of the delegated task.
      work_dir:
        type: string
        description: Working directory where the subagent is executing.
      status:
        type: string
        description: Execution status of the subagent ('running').
      message:
        type: string
        description: Status message explaining that the subagent is running asynchronously.
    required:
      - session_id
      - agent
      - title
      - work_dir
      - status
      - message
---
Spawn a specialized subagent asynchronously in an isolated child session.
