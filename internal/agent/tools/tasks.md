---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: tasks
  labels:
    category: system
spec:
  parameters:
    type: object
    properties:
      action:
        type: string
        description: "The action to perform. One of: 'list' (list all active and completed background tasks in the session), 'status' (retrieve the execution state and log tail of a specific task), 'kill' (terminate a running task)."
        enum: ["list", "status", "kill"]
      taskId:
        type: string
        description: "The ID of the background task (required for 'status' and 'kill')."
      limit:
        type: integer
        description: "The maximum number of lines from the end of the log to return for 'status' action (defaults to 100)."
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
- `list` — list all background tasks in the session with their status and exit codes.
- `status` — retrieve the current state and log tail of a specific task; use `limit` to control how many lines are returned.
- `kill` — terminate a running task.
</actions>

<when_to_use>
- After launching a long-running command with `bash`, use `status` to check progress or wait for completion.
- Use `list` to get an overview of all running and finished tasks before starting new ones.
- Use `kill` to stop a task that is stuck, no longer needed, or producing errors.
</when_to_use>

<guidelines>
- Do not poll `status` in a tight loop; wait for output or a reasonable interval before checking again.
- A task with status `completed` and `exitCode` != 0 means it finished with an error — check `stderrTail` for details.
- `taskId` is returned by `bash` when a command transitions to background; always save it if you need to track the task.
</guidelines>
