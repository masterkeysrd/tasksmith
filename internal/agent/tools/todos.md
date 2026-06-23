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

Manages a structured task list for multi-step work. Call this tool to create, update, or complete tasks. Every call **replaces the entire task list** — always include all existing tasks when making an update.

## How to Use

Call the tool by providing an array of the **complete, updated list of tasks**. This list will overwrite the current task state entirely. Omitting a task from the array will permanently remove it. Only one task in_progress at the time to keep work focuses - Update inmediately after state changes for accurate tracking.

## When to Use

Use this tool whenever:
- A new task is identified or created
- An existing task changes status (e.g., starts, completes, gets blocked)
- A task is broken down into smaller subtasks
- A task's description or `active_text` needs to be updated
- After receiving new instructions to capture requirements
- User provide multiples tasks (numbered, commands-separated list)
- User request todos list management
- Non-trivial tasks that requires careful planning and multiple operations
- After completing a task (marking it `completed` and add a follow-up task if needed)

## When NOT to Use

- Trivial task with not organization benefit
- Purely conversational or informational interactions without actionable tasks

## Task Status Values

| Status       | Meaning                                              |
|--------------|------------------------------------------------------|
| `pending`    | The task has not been started yet.                   |
| `in_progress`| The task is actively being worked on.                |
| `completed`  | The task is finished and all requirements are met.   |

## State Management

The agent is the **single source of truth** for the task list. Every invocation of this tool replaces the entire list in application state. You must always pass every task — both unchanged ones and any newly added or modified ones.

## Completion Requirement

A task may only be marked `completed` when **all** of its subtasks and associated requirements have been thoroughly verified and resolved. Do not mark a task complete prematurely.

Never mark a task as `completed` if:
- Tests or verification steps are failed or incomplete
- Implementation is partial or missing critical components
- Encountered unresolved errors or blockers that prevent full completion
- Cannot find files or dependencies required to complete the task


## Handling Blockers or Failures

If a task cannot proceed or fails, do not use a blocked state. Instead, create new todos with the tasks required to unblock the work. If you cannot complete the task, leave the todos in their current state and output to the user explaining the situation.

## Breaking Down Tasks

Complex or large tasks must be decomposed into smaller, actionable subtasks. When a task is broken down, add the new subtasks to the array alongside the parent task (which may be left `in_progress` or removed, depending on context).

**IMPORTANT**: Never print or list the todos in your response text. The users already have the full list of tasks in the application state.
