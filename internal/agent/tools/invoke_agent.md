---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: invoke_agent
  labels:
    category: workflow
    streaming: "true"
spec:
  inputSchema:
    type: object
    properties:
      agent_ref:
        type: string
        description: "The defined type name of the subagent to invoke (e.g. researcher)."
      task:
        type: string
        description: "The specific instruction or prompt for the subagent to start execution with."
      wait_ms:
        type: integer
        description: "How long (in milliseconds) to block and wait for the agent to finish before returning a background task reference."
      mode:
        type: string
        enum: ["transient", "interactive"]
        description: "The lifecycle mode of the subagent. 'transient' (default) runs the task to completion and exits. 'interactive' suspends execution upon completion of the task, allowing future instructions."
    required: ["agent_ref", "task"]
  outputSchema:
    type: object
    properties:
      task_id:
        type: string
        description: "The background task ID if the subagent execution runs in the background."
      status:
        type: string
        description: "The execution status of the subagent (e.g. completed, failed, running)."
      output:
        type: string
        description: "The final plain text message response produced by the subagent if completed synchronously."
      error:
        type: string
        description: "Error message if the subagent execution failed."
---
Invoke a defined subagent to run a task.
