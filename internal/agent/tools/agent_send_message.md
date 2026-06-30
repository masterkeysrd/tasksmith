---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: agent_send_message
  labels:
    category: workflow
spec:
  inputSchema:
    type: object
    properties:
      recipient_id:
        type: string
        description: The conversation ID of the target subagent.
      message:
        type: string
        description: The content/instruction message to send.
    required: ["recipient_id", "message"]
  outputSchema:
    type: object
    properties:
      success:
        type: boolean
        description: Whether sending the message succeeded.
      error:
        type: string
        description: Error message if sending failed.
---
Send an instruction or status query message to an active subagent.
