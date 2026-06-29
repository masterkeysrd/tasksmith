---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: todos
  labels:
    category: system
spec:
  annotations:
    isReadOnly: true
  parameters:
    type: object
    properties:
      todos:
        type: array
        description: "The complete, authoritative list of tasks. It replaces the current task list in its entirety on every update."
        items:
          type: object
          additionalProperties: true
          properties:
            description:
              type: string
              description: "A brief description of the task."
            status:
              type: string
              description: "The status of the task. Must be one of: 'pending', 'in_progress', 'completed'."
              enum: ["pending", "in_progress", "completed"]
            active_text:
              type: string
              description: "Optional text describing the current activity for in_progress tasks, e.g. 'writing code' or 'waiting for response from database'."
          required: ["description", "status"]
    required: ["todos"]
  outputSchema:
    type: object
    properties:
      todos:
        type: array
        items:
          type: object
          properties:
            description:
              type: string
            status:
              type: string
            active_text:
              type: string
---

Manage a structured task list for multi-step work. Every call **replaces the entire list** — always include all tasks, not just the ones being changed.

<when_to_use>
- When the user gives multi-step instructions — create the full plan upfront before doing any work.
- When a task starts, completes, or needs to be broken into subtasks.
- Skip for trivial single-step requests.
</when_to_use>

<workflow>
1. Receive request → call `todos` with all tasks as `pending`.
2. Mark the first task `in_progress`, execute it, then update status.
3. Only mark `completed` when fully verified — never prematurely.
4. If blocked, add new tasks to unblock rather than using a blocked state.
</workflow>

<guidelines>
- Only one task should be `in_progress` at a time.
- Never print the todo list in your response — the user sees it in the UI.
- Complex tasks must be broken into smaller actionable subtasks.
</guidelines>
