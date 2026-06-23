---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: todos
  labels:
    category: system
spec:
  parameters:
    type: object
    properties:
      todos:
        type: array
        description: "The complete, authoritative list of tasks. It replaces the current task list in its entirety on every update."
        items:
          type: object
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
# Manage Subtasks and Todos

Use this tool to plan, organize, and update your step-by-step progress checklist for the current session.

## Strategic Guidelines
- **Always initialize early:** As soon as you receive your main objective and have planned your technical approach, call `todos` to establish your initial subtask checklist.
- **Authoritative Replace-All Strategy:** This tool completely overwrites the checklist on every call. Whenever you want to add, complete, delete, or reorder tasks, you **MUST** provide the entire list of tasks in the order you want them displayed.
- **Maintain status in real time:**
  - Transition a task's status to `in_progress` when you start working on it.
  - Set `active_text` for `in_progress` tasks to give the user a clear hint of what you are doing (e.g. "writing unit tests" or "resolving LSP diagnostics").
  - Transition a task's status to `completed` once the code changes are written, compiled, and successfully tested.
  - Omit items from the array to delete them.

## Supported Statuses
- `pending`: The subtask is planned but execution has not started.
- `in_progress`: The subtask is currently being executed. Provide a helpful summary in `active_text`.
- `completed`: The subtask is completely done and validated.
