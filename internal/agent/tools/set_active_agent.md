---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: set_active_agent
  labels:
    category: system
spec:
  annotations:
    isReadOnly: false
  inputSchema:
    type: object
    properties:
      agent_name:
        type: string
        description: "The name of the agent to switch the session to. Omit or set to empty string to restore the session's default agent."
---

Switch the session's active agent to a different agent, or restore the session's default agent when a transient workflow completes.

<when-to-use>
- Call this tool at the end of a specialized workflow (e.g. `/init`, `/create-skill`) to switch the session back to the default developer agent.
</when-to-use>

<guidelines>
- Omit `agent_name` or set to `""` to restore the user's default active agent.
</guidelines>
