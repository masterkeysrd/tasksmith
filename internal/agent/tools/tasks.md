---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: tasks
  labels:
    category: system
spec:
  inputSchema:
    type: object
    properties:
      action:
        type: string
        description: "The action to perform. One of: 'list' (list background tasks), 'status' (retrieve the execution state and log tail), 'kill' (terminate a running task), 'send_input' (write data to the task's standard input)."
        enum: ["list", "status", "kill", "send_input"]
      taskId:
        type: string
        description: "The ID of the background task (required for 'status', 'kill', and 'send_input')."
      input:
        type: string
        description: "The input string to write to the task's stdin (required for 'send_input')."
      limit:
        type: integer
        description: "The maximum number of lines from the end of the log to return for 'status' action (defaults to 100)."
      include_completed:
        type: boolean
        description: "If true, 'list' will include all completed tasks in the session. By default, it only returns running tasks and the last few completed ones."
    required: ["action"]
  outputSchema:
    type: object
    properties:
      tasks:
        type: array
        description: "List of background tasks (only returned for 'list' action)."
        items:
          type: object
          properties:
            taskId:
              type: string
              description: "The ID of the task."
            name:
              type: string
              description: "The friendly name or command of the task."
            type:
              type: string
              description: "The type of task (e.g. bash)."
            status:
              type: string
              description: "The current status of the task."
            exitCode:
              type: integer
              description: "The exit code of the task (if finished)."
            startedAt:
              type: string
              description: "Timestamp when the task started."
            finishedAt:
              type: string
              description: "Timestamp when the task finished."
            error:
              type: string
              description: "Error message if the task failed."
            details:
              type: string
              description: "Extra generic task execution details."
      status:
        type: string
        description: "The status of the requested task."
      exitCode:
        type: integer
        description: "The exit code of the requested task (if finished)."
      stdoutTail:
        type: string
        description: "The tail of the standard output log (for 'status' action)."
      stderrTail:
        type: string
        description: "The tail of the standard error log (for 'status' action)."
      message:
        type: string
        description: "A human-readable result or error message."
---
Manage and monitor background tasks created by the `bash` tool.

<actions>
- `list` — list background tasks in the session. By default, only running tasks and the last 5 completed tasks are shown.
- `status` — retrieve the execution state and log tail of a specific task; use `limit` to control how many lines are returned.
- `kill` — terminate a running task.
- `send_input` — write data to the standard input (stdin) of a running task. Use this to respond to interactive prompts.
</actions>

<when_to_use>
- Use `status` to inspect intermediate logs or stdin of an actively running task, or check the final output of a completed/failed task.
- Use `list` to get an overview of all running and finished tasks before starting new ones.
- Use `kill` to stop a task that is stuck, no longer needed, or producing errors.
</when_to_use>

<guidelines>
- The system will automatically notify you and wake you up when a background task finishes. Do NOT poll or query `status` in a loop to wait for completion. Simply stop calling tools (or perform other work) and wait for the system notification.
- A task with status `completed` and `exitCode` != 0 means it finished with an error — check `stderrTail` for details.
- `taskId` is returned by `bash` when a command transitions to background; always save it if you need to track the task.
</guidelines>
